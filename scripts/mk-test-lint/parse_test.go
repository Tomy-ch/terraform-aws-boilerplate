package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

func Test_parseIncludes(t *testing.T) {
	t.Parallel()

	t.Run("include 行のパスを順に返し、行末の注記を許す", func(t *testing.T) {
		t.Parallel()
		got, err := parseIncludes([]byte("# c\ninclude .makefiles/a.mk\n\ninclude .makefiles/b.mk # sample:line\nX := 1\n"))
		require.NoError(t, err)
		assert.Equal(t, []string{".makefiles/a.mk", ".makefiles/b.mk"}, got)
	})
	t.Run("include が1つも無ければ0件として落とす", func(t *testing.T) {
		t.Parallel()
		_, err := parseIncludes([]byte(".DEFAULT_GOAL := help\n"))
		require.ErrorIs(t, err, errNoTargets)
	})
	for _, line := range []string{"-include .makefiles/a.mk", "sinclude .makefiles/a.mk", "include $(X)", "include a.mk b.mk"} {
		t.Run("展開か複数を要する include は読めない入力とする: "+line, func(t *testing.T) {
			t.Parallel()
			_, err := parseIncludes([]byte(line + "\n"))
			require.ErrorIs(t, err, errSyntax)
		})
	}
}

func Test_mkFile_line(t *testing.T) {
	t.Parallel()

	t.Run("ターゲットが最初に現れた行を返し、無ければ0を返す", func(t *testing.T) {
		t.Parallel()
		mk, err := parseMk([]byte("a:\n\techo\n\na: b\n"))
		require.NoError(t, err)
		assert.Equal(t, 1, mk.line("a"))
		assert.Equal(t, 0, mk.line("z"))
	})
}

func Test_parseMk(t *testing.T) {
	t.Parallel()

	t.Run("規則のターゲットとレシピを取り、代入・特別なターゲット・条件分岐を規則としない", func(t *testing.T) {
		t.Parallel()
		src := "X := a:b\nY = c\n.PHONY: a-test ## d\na-test: dep\n\t@go -C scripts test ./...\n\t@echo x\n\nifeq ($(A),1)\nb: ; echo b\nendif\nc: V := 1\nexport Z\n"
		mk, err := parseMk([]byte(src))
		require.NoError(t, err)
		assert.Equal(t, map[string]int{"a-test": 4, "b": 9, "c": 11}, mk.literal)
		assert.Equal(t, map[string]struct{}{"a-test": {}, "b": {}}, mk.withRecipe)
		assert.Equal(t, "\n\t@go -C scripts test ./...\n\t@echo x", mk.recipes["a-test"])
		assert.Equal(t, " echo b", mk.recipes["b"])
		assert.Equal(t, 0, mk.unresolved)
	})
	t.Run("define の中と、続きの行の中の `:` を規則としない", func(t *testing.T) {
		t.Parallel()
		src := "define F\nx-test:\n\tdefine G\n\tendef\nendef\na:\n\techo 1 \\\nz-test: \\\n\tdone\n"
		mk, err := parseMk([]byte(src))
		require.NoError(t, err)
		assert.Equal(t, map[string]int{"a": 6}, mk.literal)
		assert.Contains(t, mk.recipes["a"], "z-test:")
	})
	t.Run("名前に変数かパターンを含みレシピを持つ規則を数える", func(t *testing.T) {
		t.Parallel()
		mk, err := parseMk([]byte("$(X):\n\techo\n%.o: %.c\n\tcc\n$(Y): z\n"))
		require.NoError(t, err)
		assert.Equal(t, 2, mk.unresolved)
		assert.Empty(t, mk.literal)
	})
	t.Run("1行に複数のターゲットを持つ規則は、それぞれにレシピを持たせる", func(t *testing.T) {
		t.Parallel()
		mk, err := parseMk([]byte("a b:\n\techo\n"))
		require.NoError(t, err)
		assert.Equal(t, map[string]struct{}{"a": {}, "b": {}}, mk.withRecipe)
	})
	for name, src := range map[string]string{
		"閉じない define":       "define F\nx:\n",
		"対応の無い endef":       "endef\n",
		"続きの行の途中で終わるファイル":   "a:\n\techo \\",
		"`.mk` の中の include": "include x.mk\n",
		"ターゲットの無い規則":        ": a\n",
	} {
		t.Run(name+"は読めない入力とする", func(t *testing.T) {
			t.Parallel()
			_, err := parseMk([]byte(src))
			require.ErrorIs(t, err, errSyntax)
		})
	}
}

func Test_ruleColon(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name string
		in   string
		want int
	}{
		{"規則の区切り", "a: b", 1},
		{"即時の代入", "a := b:c", -1},
		{"POSIX の代入", "a ::= b", -1},
		{"再帰の代入", "a = b:c", -1},
		{"追記の代入", "a += b", -1},
		{"変数の中の `:` と `=` を飛ばす", "$(subst a:b,=,x): y", 16},
		{"波括弧の変数", "${X:a=b}: y", 8},
		{"`:` も `=` も無い行", "abc", -1},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, ruleColon(c.in))
		})
	}
}

const dbHead = "# GNU Make 3.81\n\n# Make data base, printed on x\n\n"
const dbTail = "\n# files hash-table stats:\n\n# Finished Make data base on x\n"

func Test_parseDB(t *testing.T) {
	t.Parallel()

	t.Run("レシピを定義したファイルごとにターゲットを返し、ターゲットでないものと特別なものを除く", func(t *testing.T) {
		t.Parallel()
		db := dbHead + "# Variables\nx: y\n#  commands to execute (from `v.mk', line 1):\n\n# Files\n\n" +
			"a-test:\n#  Phony target.\n#  commands to execute (from `.makefiles/a.mk', line 3):\n\techo\n\n" +
			"b: c\n#  recipe to execute (from '.makefiles/b.mk', line 1):\n\techo\n\n" +
			"# Not a target:\n.makefiles/a.mk:\n#  commands to execute (from `Makefile', line 1):\n\n" +
			".PHONY: a-test\n\nd:\n#  Phony target.\n" + dbTail
		got, err := parseDB([]byte(db), 1)
		require.NoError(t, err)
		assert.Equal(t, map[string]map[string]struct{}{
			".makefiles/a.mk": {"a-test": {}},
			".makefiles/b.mk": {"b": {}},
		}, got)
	})
	t.Run("make の終了コードが 0 と 1 以外なら読めない入力とする", func(t *testing.T) {
		t.Parallel()
		_, err := parseDB([]byte(dbHead+dbTail), 2)
		require.ErrorIs(t, err, errUnreadable)
	})
	t.Run("始まりか終わりの無い出力は読めない入力とする", func(t *testing.T) {
		t.Parallel()
		_, err := parseDB([]byte(dbHead+"# Files\n"), 0)
		require.ErrorIs(t, err, errUnreadable)
	})
	t.Run("レシピを持つ規則が0件なら落とす", func(t *testing.T) {
		t.Parallel()
		_, err := parseDB([]byte(dbHead+"# Files\n\nd:\n#  Phony target.\n"+dbTail), 0)
		require.ErrorIs(t, err, errNoTargets)
	})
}

func Test_bypass_valid(t *testing.T) {
	t.Parallel()

	today := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		name    string
		expires time.Time
		want    bool
	}{
		{"期限の当日は有効", today, true},
		{"期限の翌日は無効", today.AddDate(0, 0, -1), false},
		{"上限ちょうどは有効", today.AddDate(0, 3, 0), true},
		{"上限の翌日は無効", today.AddDate(0, 3, 1), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, bypass{expires: c.expires}.valid(today))
		})
	}
}

func Test_parseBypass(t *testing.T) {
	t.Parallel()

	t.Run("エントリを名前ごとに行番号つきで返し、空行とコメントを飛ばす", func(t *testing.T) {
		t.Parallel()
		got, err := parseBypass([]byte("# c\n\n\"a-test\" = { expires = 2027-01-31, issue = 439, reason = \"r\" }\n"))
		require.NoError(t, err)
		assert.Equal(t, map[string]bypass{"a-test": {expires: time.Date(2027, 1, 31, 0, 0, 0, 0, time.UTC), issue: 439, line: 3}}, got)
	})
	t.Run("空の宣言は空の一覧を返す", func(t *testing.T) {
		t.Parallel()
		got, err := parseBypass(nil)
		require.NoError(t, err)
		assert.Empty(t, got)
	})
	for name, src := range map[string]string{
		"キーの欠けた行":  `"a-test" = { expires = 2027-01-31, issue = 439 }`,
		"空の理由":     `"a-test" = { expires = 2027-01-31, issue = 439, reason = "" }`,
		"存在しない日付":  `"a-test" = { expires = 2027-02-30, issue = 439, reason = "r" }`,
		"順序の違うキー":  `"a-test" = { issue = 439, expires = 2027-01-31, reason = "r" }`,
		"重複したエントリ": "\"a-test\" = { expires = 2027-01-31, issue = 1, reason = \"r\" }\n\"a-test\" = { expires = 2027-01-31, issue = 1, reason = \"r\" }",
	} {
		t.Run(name+"は読めない入力とする", func(t *testing.T) {
			t.Parallel()
			_, err := parseBypass([]byte(src))
			require.ErrorIs(t, err, errSyntax)
			assert.False(t, xerrors.Is(err, errViolations))
		})
	}
}
