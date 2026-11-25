package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/misetoml"
)

func Test_runCount(t *testing.T) {
	t.Parallel()

	f := newFixture(t)
	from := f.head()
	f.write("a", "a\n")
	f.commit("text")
	f.commit("empty", "--allow-empty")
	ns := f.numstat(from)

	for name, c := range map[string]struct {
		out     string
		numstat []byte
		minimum int
		want    error
	}{
		"範囲: 空の commit を数えない走査を通す": {"INF 1 commits scanned.\n", ns, 0, nil},
		"全履歴: 下限ちょうど":              {"INF 1 commits scanned.\n", nil, 1, nil},
		"範囲: 走査が0件":                {"INF 0 commits scanned.\n", ns, 0, errMismatch},
		"範囲: 空の commit まで数えた走査":    {"INF 2 commits scanned.\n", ns, 0, errMismatch},
		"範囲: 空の範囲を0件の走査で通す":        {"INF 0 commits scanned.\n", []byte{}, 0, nil},
		"全履歴: 下限の1つ手前":             {"INF 0 commits scanned.\n", nil, 1, errMismatch},
		"数を読めない出力":                 {"ERR stderr is not empty\n", ns, 0, errNoCount},
		"全履歴: 数を読めない出力":            {"", nil, 1, errNoCount},
		"numstat を解釈できない":          {"INF 1 commits scanned.\n", []byte("x\x00"), 0, errNumstat},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := runCount(c.out, c.numstat, c.minimum)
			if c.want == nil {
				require.NoError(t, err)

				return
			}
			require.ErrorIs(t, err, c.want)
		})
	}

	t.Run("範囲: 上限は数えるかを決められない commit を含む（must と must+maybe を通し、+1 を落とす）", func(t *testing.T) {
		t.Parallel()
		g := newFixture(t)
		from := g.head()
		g.write("a", "a\n")
		g.commit("text")
		g.git("mv", "base", "moved")
		g.commit("rename")
		ns := g.numstat(from)
		must, maybe, err := expected(ns)
		require.NoError(t, err)
		require.Equal(t, [2]int{1, 1}, [2]int{must, maybe}, "前提: テキストの commit 1つと、行の変わらない rename だけの commit 1つ")
		require.NoError(t, runCount("INF 1 commits scanned.\n", ns, 0))
		require.NoError(t, runCount("INF 2 commits scanned.\n", ns, 0))
		require.ErrorIs(t, runCount("INF 3 commits scanned.\n", ns, 0), errMismatch)
		require.ErrorIs(t, runCount("INF 0 commits scanned.\n", ns, 0), errMismatch)
	})
}

func Test_scanned(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		out  string
		want int
	}{
		"gitleaks の行": {"9:41AM INF 4 commits scanned.\n9:41AM INF no leaks found\n", 4},
		"0件":          {"INF 0 commits scanned.\n", 0},
		"前の文字が数字でない":  {"x12 commits scanned.", 12},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			n, err := scanned(c.out)
			require.NoError(t, err)
			assert.Equal(t, c.want, n)
		})
	}

	for name, out := range map[string]string{
		"数の行が無い": "ERR error=\"stderr is not empty\"\n",
		"空の出力":   "",
		"数の行が2つ": "INF 1 commits scanned.\nINF 2 commits scanned.\n",
		"桁あふれ":   "INF 99999999999999999999 commits scanned.\n",
		"数の無い行":  "INF commits scanned.\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := scanned(out)
			require.ErrorIs(t, err, errNoCount)
		})
	}
}

func Test_expected(t *testing.T) {
	t.Parallel()

	t.Run("git の出力", func(t *testing.T) {
		t.Parallel()

		for name, c := range map[string]struct {
			build       func(f *fixture)
			must, maybe int
		}{
			"空の範囲":           {func(*fixture) {}, 0, 0},
			"空の commit だけ":   {func(f *fixture) { f.commit("empty", "--allow-empty") }, 0, 0},
			"空の commit を含む":  {func(f *fixture) { f.write("a", "a\n"); f.commit("a"); f.commit("empty", "--allow-empty") }, 1, 0},
			"行を足す":           {func(f *fixture) { f.write("a", "a\n"); f.commit("a") }, 1, 0},
			"行を消すだけの変更":      {func(f *fixture) { f.write("base", ""); f.commit("shrink") }, 1, 0},
			"ファイルの削除だけ":      {func(f *fixture) { f.git("rm", "-q", "base"); f.commit("rm") }, 0, 0},
			"空のファイルの追加":      {func(f *fixture) { f.write("e", ""); f.commit("e") }, 0, 0},
			"mode の変更だけ":     {func(f *fixture) { f.git("update-index", "--chmod=+x", "base"); f.commit("x") }, 0, 0},
			"行の変わらない rename": {func(f *fixture) { f.git("mv", "base", "moved"); f.commit("mv") }, 0, 1},
			"行の変わる rename": {func(f *fixture) {
				f.git("mv", "base", "moved")
				f.write("moved", "base\nmore\n")
				f.commit("mv")
			}, 1, 0},
			"バイナリだけ":    {func(f *fixture) { f.write("b.bin", "\x00\x01\x02"); f.commit("bin") }, 0, 1},
			"バイナリとテキスト": {func(f *fixture) { f.write("b.bin", "\x00\x01"); f.write("a", "a\n"); f.commit("both") }, 1, 0},
			"commit ごとに数える": {func(f *fixture) {
				f.write("a", "a\n")
				f.commit("a")
				f.write("b", "b\n")
				f.write("c", "c\n")
				f.commit("bc")
			}, 2, 0},
			"空白や改行を含む path": {func(f *fixture) { f.write("a b\nc", "x\n"); f.commit("odd") }, 1, 0},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				f := newFixture(t)
				from := f.head()
				c.build(f)

				must, maybe, err := expected(f.numstat(from))
				require.NoError(t, err)
				assert.Equal(t, c.must, must, "必ず数える commit")
				assert.Equal(t, c.maybe, maybe, "数えるかを決められない commit")
			})
		}
	})

	t.Run("解釈できない出力", func(t *testing.T) {
		t.Parallel()

		hash := strings.Repeat("a", 40)
		for name, raw := range map[string]string{
			"commit の行より前の numstat": "\n1\t0\ta\x00",
			"object 名でない commit の行": "commit xyz\x00",
			"大文字の object 名":         "commit " + strings.Repeat("A", 40) + "\x00",
			"列が足りない":                "commit " + hash + "\x00\n1\t0\x00",
			"数でない行数":                "commit " + hash + "\x00\nx\t0\ta\x00",
			"片側だけの -":               "commit " + hash + "\x00\n-\t0\ta\x00",
			"rename の path が欠けている":  "commit " + hash + "\x00\n0\t0\t\x00old\x00",
			"途中の空の区切り":              "commit " + hash + "\x00\x00\n1\t0\ta\x00",
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				_, _, err := expected([]byte(raw))
				require.ErrorIs(t, err, errNumstat)
			})
		}
	})

	t.Run("SHA-256 の object 名", func(t *testing.T) {
		t.Parallel()

		must, maybe, err := expected([]byte("commit " + strings.Repeat("b", 64) + "\x00\n1\t0\ta\x00"))
		require.NoError(t, err)
		assert.Equal(t, [2]int{1, 0}, [2]int{must, maybe})
	})

	t.Run("写しと正本のずれ", func(t *testing.T) {
		t.Parallel()

		entries, err := misetoml.ParseFile(filepath.Join("..", "..", "mise.toml"))
		require.NoError(t, err)

		got, ok := misetoml.Lookup(entries, "aqua:gitleaks/gitleaks")
		require.True(t, ok, "mise.toml に gitleaks の宣言がありません")
		assert.Equal(t, gitleaksVersion, got, "gitleaks を上げたら、数え方を新しい版のソースで確かめ直す")
	})
}

func Test_lineCounts(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		added, deleted  string
		changed, binary bool
	}{
		"行が変わる":   {"1", "0", true, false},
		"行を消すだけ":  {"0", "3", true, false},
		"行が変わらない": {"0", "0", false, false},
		"バイナリ":    {"-", "-", false, true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			changed, binary, err := lineCounts(c.added, c.deleted)
			require.NoError(t, err)
			assert.Equal(t, c.changed, changed)
			assert.Equal(t, c.binary, binary)
		})
	}

	for name, c := range map[string][2]string{
		"負の数":     {"-1", "0"},
		"数でない":    {"a", "0"},
		"空":       {"", ""},
		"片側だけの -": {"0", "-"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, _, err := lineCounts(c[0], c[1])
			require.ErrorIs(t, err, errNumstat)
		})
	}
}
