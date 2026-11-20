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
		{". file は source", "\t. ./env.sh", []string{"source"}},
		{"行頭の [ ] を拾う", "\t[ -f x ]", []string{"[ ]"}},
		{"@ の後ろの空白を飛ばしてコマンドを読む", "\t@ if x; then y; fi", []string{"if"}},
		{"! の後ろのコマンドを読む", "\t@! test -f a", []string{"test"}},
		{"環境変数の代入の後ろのコマンドを読む", "\t@X=1 test -f a", []string{"test"}},
		{"time の後ろのコマンドを読む", "\t@time if x; then y; fi", []string{"if"}},
		{"sh -c の単引用符の中の if", "\t@sh -c 'if x; then y; fi'", []string{"if"}},
		{"bash -c の二重引用符の中の while", "\t@bash -c \"while x; do y; done\"", []string{"while"}},
		{"sh -c の中の test と [ ]", "\t@sh -c 'test -f a'; sh -c '[ -f b ]'", []string{"test", "[ ]"}},
		{"閉じない sh -c の引数も読む", "\t@$(GO_TOOL) sh -c 'set -eu; for d in a; do \\", []string{"for"}},
		{"引用符の中の jq の式をコマンドと読まない", "\t@jq '.a | if type == \"array\" then .[] else . end' x", nil},
		{"数字の直後の & も背景実行", "\tsleep 5&", []string{"&（背景実行）"}},
		{"リダイレクトの & は背景実行ではない", "\ta 2>&1 >&2 &>x", nil},
		{"function キーワードの関数定義", "\tfunction f { a; }", []string{"関数定義"}},
		{"go-do のような語を区切りと読まない", "\t@go-do test-x", nil},
		{"計算された変数名", "\t@$($(N))", []string{"計算された変数名"}},
		{"結合したフラグの sh -euc", "\t@sh -euc 'if x; then y; fi'", []string{"if"}},
		{"-c の前に別のオプション", "\t@bash -o pipefail -c 'while x; do y; done'", []string{"while"}},
		{"-c の後ろの --", "\t@sh -c -- 'test -f a'", []string{"test"}},
		{"$(SHELL) -c", "\t@$(SHELL) -c 'for d in a; do b; done'", []string{"for"}},
		{"エスケープした引用符は解釈できない", "\techo \\' ; if x; then y; fi ; echo '", []string{"解釈できない引用かシェルへの流し込み"}},
		{"'\\'' を含む sh -c は解釈できない", "\t@sh -c 'echo '\\''x'\\''; exit 1'", []string{"解釈できない引用かシェルへの流し込み"}},
		{"シェルへの流し込みは解釈できない", "\techo 'if x; then y; fi' | sh", []string{"解釈できない引用かシェルへの流し込み"}},
		{"ANSI-C の引用は解釈できない", "\t@sh -c $$'exit 1'", []string{"解釈できない引用かシェルへの流し込み"}},
		{"引用したコマンドの名前", "\t@\"test\" -f x", []string{"引用したコマンドの名前"}},
		{"env のオプションの後ろのコマンドを読む", "\t@env -i test -f x", []string{"test"}},
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
		assert.Equal(t, []string{"A", "B", "C", "my-var", "my.var", "D", "E"}, varRefs("$(A) ${B} $(call C,x) $(if x,y) $(wildcard *) $(my-var) $(my.var) $(value D) $(E:.a=.b)"))
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
	t.Run("define の本体の続きの行も辿る", func(t *testing.T) {
		t.Parallel()
		a := parse(t, "a:\n\t@$(D)\ndefine D\necho a \\\n && echo b\nendef\n")
		got := recipeFindings("a.mk", a, vars("a.mk", a))
		assert.Equal(t, []finding{{file: "a.mk", line: 5, what: "&&", where: "変数 D の本体"}}, got)
	})
	t.Run("export と override の代入と define の本体も辿る", func(t *testing.T) {
		t.Parallel()
		a := parse(t, "a:\n\t@$(E) $(O) $(F)\nexport E := a && b\noverride O = c || d\nexport define F\nfor x in y; do z; done\nendef\n")
		got := recipeFindings("a.mk", a, vars("a.mk", a))
		var whats []string
		for _, f := range got {
			whats = append(whats, f.what)
		}
		assert.ElementsMatch(t, []string{"&&", "||", "for"}, whats)
	})
	t.Run("ターゲットだけに効く変数の本体も辿る", func(t *testing.T) {
		t.Parallel()
		a := parse(t, "a: V = $(if $(X),y)\na:\n\t@$(V)\n")
		got := recipeFindings("a.mk", a, vars("a.mk", a))
		assert.Equal(t, []finding{{file: "a.mk", line: 1, what: "$(if)", where: "変数 V の本体"}}, got)
	})
	t.Run("前提条件の make の分岐も落とす", func(t *testing.T) {
		t.Parallel()
		a := parse(t, "a: $(or $(A),b)\n\t@echo\n")
		got := recipeFindings("a.mk", a, vars("a.mk", a))
		assert.Equal(t, []finding{{file: "a.mk", line: 1, what: "$(or)", where: "ターゲット a のレシピ"}}, got)
	})
	t.Run("規則の直後の条件ディレクティブの先のレシピも検査する", func(t *testing.T) {
		t.Parallel()
		a := parse(t, "a:\nifndef X\n\t@if x; then y; fi\nendif\n")
		got := recipeFindings("a.mk", a, vars("a.mk", a))
		var whats []string
		for _, f := range got {
			whats = append(whats, f.what)
		}
		assert.ElementsMatch(t, []string{"条件ディレクティブ", "if"}, whats)
	})
	t.Run("名前に変数を含む規則のレシピも検査する", func(t *testing.T) {
		t.Parallel()
		a := parse(t, "$(X)-gen:\n\t@a && b\n")
		got := recipeFindings("a.mk", a, vars("a.mk", a))
		assert.Equal(t, []finding{{file: "a.mk", line: 2, what: "&&", where: "ターゲット $(X)-gen のレシピ"}}, got)
	})
	t.Run("; の後ろのレシピの続きの行も検査する", func(t *testing.T) {
		t.Parallel()
		a := parse(t, "b: ; foo \\\n  && bar\n")
		got := recipeFindings("a.mk", a, vars("a.mk", a))
		assert.Equal(t, []finding{{file: "a.mk", line: 2, what: "&&", where: "ターゲット b のレシピ"}}, got)
	})
	t.Run("名前にハイフンとドットを含む変数も辿る", func(t *testing.T) {
		t.Parallel()
		a := parse(t, "a:\n\t@$(my-var) $(my.var)\nmy-var = a && b\nmy.var = c || d\n")
		assert.Len(t, recipeFindings("a.mk", a, vars("a.mk", a)), 2)
	})
	t.Run("RUN_SCRIPT_HOST の本体も辿らない", func(t *testing.T) {
		t.Parallel()
		a := parse(t, "a:\n\t@$(call RUN_SCRIPT_HOST,x,y)\nRUN_SCRIPT_HOST = sh -c 'a && b'\n")
		assert.Empty(t, recipeFindings("a.mk", a, vars("a.mk", a)))
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
	t.Run("レシピの中の条件ディレクティブを落とし、レシピの外のものは数えない", func(t *testing.T) {
		t.Parallel()
		a := parse(t, "a:\n\t@one\nifdef X\n\t@two\nendif\nifeq (a,b)\nZ := 1\nendif\n")
		got := recipeFindings("a.mk", a, vars("a.mk", a))
		assert.Equal(t, []finding{{file: "a.mk", line: 3, what: "条件ディレクティブ", where: "ターゲット a のレシピ"}}, got)
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
