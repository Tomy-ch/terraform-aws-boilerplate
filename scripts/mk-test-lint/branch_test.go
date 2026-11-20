package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_constructs(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name string
		line string
		want []string
	}{
		{"道具を起動するだけの行は当たらない", "\t@$(call RUN_SCRIPT,egress,check)", nil},
		{"make の変数の中の語を、コマンドの位置と読まない", "\t@$(TF) test", nil},
		{"go の subcommand の test は当たらない", "\t@$(GO_TOOL) go -C scripts test ./...", nil},
		{"パイプとリダイレクトと set は当たらない", "\t@set -eu; a | b > c 2>&1", nil},
		{"if（条件の [ ] は if の一部として拾う）", "\t@if [ -n x ]; then a; fi", []string{"if"}},
		{"case", "\tcase $$x in a) b ;; esac", []string{"case"}},
		{"for", "\tfor d in a b; do c; done", []string{"for"}},
		{"while と until", "\twhile x; do y; done; until z; do w; done", []string{"while", "until"}},
		{"&& と ||", "\ta && b || c", []string{"&&", "||"}},
		{"単独の & は背景実行", "\ta & wait", []string{"&（背景実行）", "wait"}},
		{"[[ ]] と test", "\t[[ -n x ]]; test -f y", []string{"[[ ]]", "test"}},
		{"条件のパラメータ展開", "\techo $${X:-y}", []string{"条件のパラメータ展開"}},
		{"コマンド置換", "\tx=$$(git rev-parse HEAD)", []string{"コマンド置換"}},
		{"バッククォート", "\tx=`date`", []string{"コマンド置換"}},
		{"関数定義", "\tf() { a; }; f", []string{"関数定義"}},
		{"trap と exit と return", "\ttrap 'rm x' EXIT; exit 1; return 0", []string{"trap", "exit", "return"}},
		{"eval と source と .", "\teval x; source y; . z", []string{"eval", "source"}},
		{"make の分岐", "\t@$(if $(X),a,b)", []string{"$(if)"}},
		{"make の繰り返し", "\t@$(foreach d,$(DIRS),echo $(d);)", []string{"$(foreach)"}},
		{"make の $(shell)", "X := $(shell date)", []string{"$(shell)"}},
		{"条件ディレクティブ", "ifeq ($(A),1)", []string{"条件ディレクティブ"}},
		{"値が . だけの代入は source ではない", ".", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			assert.ElementsMatch(t, c.want, constructs(c.line))
		})
	}
}

func Test_varRefs(t *testing.T) {
	t.Parallel()

	t.Run("変数と call の名前を返し、make の関数を除く", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"A", "B", "C"}, varRefs("$(A) ${B} $(call C,x) $(if x,y) $(wildcard *)"))
	})
	t.Run("参照が無ければ空", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, varRefs("echo $$HOME"))
	})
}

func Test_recipeFindings(t *testing.T) {
	t.Parallel()

	parse := func(t *testing.T, src string) mkFile {
		t.Helper()
		mk, err := parseMk([]byte(src))
		require.NoError(t, err)
		return mk
	}
	vars := func(file string, mks ...mkFile) map[string][]varDef {
		out := map[string][]varDef{}
		for _, mk := range mks {
			for name, body := range mk.vars {
				out[name] = append(out[name], varDef{file: file, body: body})
			}
		}
		return out
	}

	t.Run("レシピの行の違反を ファイル:行 とターゲットで返す", func(t *testing.T) {
		t.Parallel()
		mk := parse(t, "a:\n\t@echo ok\n\t@x && y\n")
		got := recipeFindings("a.mk", mk, vars("a.mk", mk))
		assert.Equal(t, []finding{{file: "a.mk", line: 3, what: "&&", where: "ターゲット a のレシピ"}}, got)
	})
	t.Run("レシピから参照される変数と define の本体を、別のファイルまで推移的に辿る", func(t *testing.T) {
		t.Parallel()
		a := parse(t, "a:\n\t@$(call OUTER,x)\n")
		lib := parse(t, "define OUTER\n$(INNER) $(1)\nendef\nINNER = for d in x; do y; done\n")
		got := recipeFindings("a.mk", a, vars("lib.mk", lib))
		assert.Equal(t, []finding{{file: "lib.mk", line: 4, what: "for", where: "変数 INNER の本体"}}, got)
	})
	t.Run("起動の前置きの本体は辿らない", func(t *testing.T) {
		t.Parallel()
		a := parse(t, "a:\n\t@$(call RUN_SCRIPT,x,y)\n")
		runner := parse(t, "RUN_SCRIPT = $(if $(A),b) $$(id -u)\nCOMPOSE_RUN = x || true\n")
		assert.Empty(t, recipeFindings("a.mk", a, vars("runner.mk", runner)))
	})
	t.Run("参照されない変数の本体は見ない", func(t *testing.T) {
		t.Parallel()
		a := parse(t, "a:\n\t@echo ok\nUNUSED = a && b\n")
		assert.Empty(t, recipeFindings("a.mk", a, vars("a.mk", a)))
	})
	t.Run("変数の再帰を落とす", func(t *testing.T) {
		t.Parallel()
		a := parse(t, "a:\n\t@$(X)\nX = $(Y)\nY = $(X)\n")
		got := recipeFindings("a.mk", a, vars("a.mk", a))
		require.Len(t, got, 1)
		assert.Equal(t, "変数の再帰", got[0].what)
	})
	t.Run("レシピの中の条件ディレクティブを落とす", func(t *testing.T) {
		t.Parallel()
		a := parse(t, "a:\n\t@one\nifdef X\n\t@two\nendif\n")
		got := recipeFindings("a.mk", a, vars("a.mk", a))
		assert.Equal(t, []finding{
			{file: "a.mk", line: 3, what: "条件ディレクティブ", where: "ターゲット a のレシピ"},
			{file: "a.mk", line: 5, what: "条件ディレクティブ", where: "ターゲット a のレシピ"},
		}, got)
	})
}

func Test_parseScope(t *testing.T) {
	t.Parallel()

	t.Run("files の一覧を順に返し、コメントと空行を飛ばす", func(t *testing.T) {
		t.Parallel()
		got, err := parseScope([]byte("# c\n\nfiles = [\n  \"Makefile\",\n  \".makefiles/a.mk\",\n]\n"))
		require.NoError(t, err)
		assert.Equal(t, []string{"Makefile", ".makefiles/a.mk"}, got)
	})
	t.Run("空の一覧は0件として落とす", func(t *testing.T) {
		t.Parallel()
		_, err := parseScope([]byte("files = [\n]\n"))
		require.ErrorIs(t, err, errNoTargets)
	})
	for name, src := range map[string]string{
		"閉じない一覧":   "files = [\n  \"a.mk\",\n",
		"一覧の無い宣言":  "# c\n",
		"引用符の無いパス": "files = [\n  a.mk,\n]\n",
		"重複したパス":   "files = [\n  \"a.mk\",\n  \"a.mk\",\n]\n",
		"一覧の外の行":   "x = 1\nfiles = [\n  \"a.mk\",\n]\n",
		"2つ目の一覧":   "files = [\n  \"a.mk\",\n]\nfiles = [\n]\n",
	} {
		t.Run(name+"は読めない入力とする", func(t *testing.T) {
			t.Parallel()
			_, err := parseScope([]byte(src))
			require.ErrorIs(t, err, errSyntax)
		})
	}
}
