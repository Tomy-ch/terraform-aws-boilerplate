package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validIgnore = "warnings:\n" +
	"  - summary: Argument is deprecated\n" +
	"    path: modules/a/main.tf\n" +
	"    block: resource \"aws_iam_role\" \"this\"\n" +
	"    detail: managed_policy_arns is deprecated.\n" +
	"    occurrences: 1\n" +
	"    reason: 理由\n" +
	"    withdrawal: 撤回の条件\n"

func Test_loadIgnore(t *testing.T) {
	t.Parallel()
	t.Run("宣言の各欄を読む", func(t *testing.T) {
		t.Parallel()
		got, err := loadIgnore(writeFile(t, t.TempDir(), "ignore.yaml", validIgnore))
		require.NoError(t, err)
		assert.Equal(t, []ignoreEntry{{
			Summary: "Argument is deprecated", Path: "modules/a/main.tf", Block: `resource "aws_iam_role" "this"`,
			Detail: "managed_policy_arns is deprecated.", Occurrences: 1, Reason: "理由", Withdrawal: "撤回の条件",
		}}, got)
	})
	t.Run("0件の宣言は抑止なしとして読む", func(t *testing.T) {
		t.Parallel()
		got, err := loadIgnore(writeFile(t, t.TempDir(), "ignore.yaml", "# 説明\nwarnings: []\n"))
		require.NoError(t, err)
		assert.Empty(t, got)
	})
	t.Run("無いファイルはエラー", func(t *testing.T) {
		t.Parallel()
		_, err := loadIgnore(t.TempDir() + "/missing.yaml")
		require.ErrorIs(t, err, errIgnore)
	})

	field := func(old, repl string) string {
		return replaceOnce(t, validIgnore, old, repl)
	}
	const notNormal = "は走査の基点からの正規化したパスではありません"
	broken := []struct{ name, body, msg string }{
		{"空のファイル", "", "が空です"},
		{"コメントだけのファイル", "# 説明\n", "が空です"},
		{"warnings が無い", "{}\n", "warnings がありません"},
		{"warnings が null", "warnings:\n", "warnings がありません"},
		{"知らないキー", validIgnore + "    line: 3\n", "field line not found"},
		{"知らない最上位のキー", validIgnore + "extra: 1\n", "field extra not found"},
		{"YAML の文書が2つ", validIgnore + "---\nwarnings: []\n", "YAML の文書が2つ以上あります"},
		{"YAML として読めない", "warnings: [\n", "yaml:"},
		{"summary が空", field("summary: Argument is deprecated", "summary: ''"), "warnings[0] の summary が空です"},
		{"path が空", field("path: modules/a/main.tf", "path: ''"), "warnings[0] の path が空です"},
		{"block が空", field(`block: resource "aws_iam_role" "this"`, "block: ''"), "warnings[0] の block が空です"},
		{"detail が空", field("detail: managed_policy_arns is deprecated.", "detail: ''"), "warnings[0] の detail が空です"},
		{"reason が空白だけ", field("reason: 理由", "reason: '  '"), "warnings[0] の reason が空です"},
		{"withdrawal が無い", field("    withdrawal: 撤回の条件\n", ""), "warnings[0] の withdrawal が空です"},
		{"occurrences が無い", field("    occurrences: 1\n", ""), "occurrences は1以上です"},
		{"occurrences が0", field("occurrences: 1", "occurrences: 0"), "occurrences は1以上です"},
		{"occurrences が負", field("occurrences: 1", "occurrences: -1"), "occurrences は1以上です"},
		{"path が絶対パス", field("path: modules/a/main.tf", "path: /modules/a/main.tf"), notNormal},
		{"path が正規化されていない", field("path: modules/a/main.tf", "path: modules/b/../a/main.tf"), notNormal},
		{"path が基点の外", field("path: modules/a/main.tf", "path: ../x/main.tf"), notNormal},
		{"path が基点の親そのもの", field("path: modules/a/main.tf", "path: '..'"), notNormal},
		{"path が ./ で始まる", field("path: modules/a/main.tf", "path: ./modules/a/main.tf"), notNormal},
		{"同じ警告を名指す宣言が2つ", validIgnore + validIgnore[len("warnings:\n"):], "前の宣言と同じ警告を名指します"},
	}
	for _, c := range broken {
		t.Run(c.name+"はエラー", func(t *testing.T) {
			t.Parallel()
			_, err := loadIgnore(writeFile(t, t.TempDir(), "ignore.yaml", c.body))
			require.ErrorIs(t, err, errIgnore)
			assert.ErrorContains(t, err, c.msg)
		})
	}
}

func Test_applyIgnore(t *testing.T) {
	t.Parallel()
	entry := ignoreEntry{Summary: "s", Path: "modules/a/main.tf", Block: "b", Detail: "d", Occurrences: 1}
	w := warning{path: "modules/a/main.tf", line: 1, column: 1, summary: "s", block: "b", detail: "d"}

	t.Run("4つの欄が一致する警告を外し、件数が合えば宣言は合う", func(t *testing.T) {
		t.Parallel()
		remaining, suppressed, mismatches := applyIgnore([]ignoreEntry{entry}, []warning{w})
		assert.Empty(t, remaining)
		assert.Equal(t, 1, suppressed)
		assert.Empty(t, mismatches)
	})
	t.Run("宣言が無ければすべての警告が残る", func(t *testing.T) {
		t.Parallel()
		remaining, suppressed, mismatches := applyIgnore(nil, []warning{w})
		assert.Equal(t, []warning{w}, remaining)
		assert.Zero(t, suppressed)
		assert.Empty(t, mismatches)
	})
	t.Run("どの警告にも当たらない宣言は件数0で合わない", func(t *testing.T) {
		t.Parallel()
		remaining, _, mismatches := applyIgnore([]ignoreEntry{entry}, nil)
		assert.Empty(t, remaining)
		assert.Equal(t, []ignoreMismatch{{entry: entry, matched: 0}}, mismatches)
	})
	t.Run("同じ block と detail の2件目が増えると件数が合わない", func(t *testing.T) {
		t.Parallel()
		w2 := w
		w2.line = 5
		remaining, suppressed, mismatches := applyIgnore([]ignoreEntry{entry}, []warning{w, w2})
		assert.Empty(t, remaining)
		assert.Equal(t, 2, suppressed)
		assert.Equal(t, []ignoreMismatch{{entry: entry, matched: 2}}, mismatches)
	})
	t.Run("occurrences が当たる件数より多いと合わない", func(t *testing.T) {
		t.Parallel()
		e := entry
		e.Occurrences = 2
		_, _, mismatches := applyIgnore([]ignoreEntry{e}, []warning{w})
		assert.Equal(t, []ignoreMismatch{{entry: e, matched: 1}}, mismatches)
	})

	differs := []struct {
		name   string
		modify func(*warning)
	}{
		{"summary", func(x *warning) { x.summary = "other" }},
		{"path", func(x *warning) { x.path = "modules/b/main.tf" }},
		{"block", func(x *warning) { x.block = "other" }},
		{"detail", func(x *warning) { x.detail = "d2" }},
	}
	for _, c := range differs {
		t.Run(c.name+"が違う警告には当たらない", func(t *testing.T) {
			t.Parallel()
			other := w
			c.modify(&other)
			remaining, suppressed, mismatches := applyIgnore([]ignoreEntry{entry}, []warning{other})
			assert.Equal(t, []warning{other}, remaining)
			assert.Zero(t, suppressed)
			assert.Equal(t, []ignoreMismatch{{entry: entry, matched: 0}}, mismatches)
		})
	}
	t.Run("位置を持たない警告には当たらない", func(t *testing.T) {
		t.Parallel()
		e := ignoreEntry{Summary: "s", Path: "", Block: "", Detail: "d", Occurrences: 1}
		noPath := warning{summary: "s", detail: "d"}
		remaining, suppressed, mismatches := applyIgnore([]ignoreEntry{e}, []warning{noPath})
		assert.Equal(t, []warning{noPath}, remaining)
		assert.Zero(t, suppressed)
		assert.Equal(t, []ignoreMismatch{{entry: e, matched: 0}}, mismatches)
	})
}

func replaceOnce(t *testing.T, s, old, repl string) string {
	t.Helper()
	i := strings.Index(s, old)
	require.GreaterOrEqual(t, i, 0, "置き換え元 %q が見つかりません", old)
	return s[:i] + repl + s[i+len(old):]
}
