package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var testToday = time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)

// 違反の無い最小の構成。
var baseMk = map[string]string{
	".makefiles/go.mk": ".PHONY: go-test ## t\ngo-test:\n\t@$(GO_TOOL) go -C scripts test ./...\n",
	".makefiles/tf.mk": ".PHONY: tf-test ## t\ntf-test: tf-init\n\t@$(TF) -chdir=\"$$d\" test || x\n\ntf-init:\n\techo\n",
}

// dbFor は、`.mk` のレシピを持つ規則を make -p の出力の形に並べる（make を起動せずに済ませる）。
func dbFor(t *testing.T, files map[string]string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString(dbHead + "# Files\n\n")
	for _, path := range keys(files) {
		mk, err := parseMk([]byte(files[path]))
		require.NoError(t, err)
		for _, name := range keys(mk.withRecipe) {
			b.WriteString(name + ":\n#  commands to execute (from `" + path + "', line 1):\n\techo\n\n")
		}
	}
	b.WriteString(dbTail)
	return b.String()
}

func with(extra map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range baseMk {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func makefileFor(files map[string]string) string {
	var b strings.Builder
	for _, p := range keys(files) {
		b.WriteString("include " + p + "\n")
	}
	return b.String()
}

func inputFor(t *testing.T, files map[string]string, bypassText string) input {
	t.Helper()
	mk := map[string][]byte{}
	for k, v := range files {
		mk[k] = []byte(v)
	}
	return input{
		makefilePath: "Makefile", makefile: []byte(makefileFor(files)), mk: mk,
		db: []byte(dbFor(t, files)), dbExit: 1, bypassPath: "bypass.toml", bypass: []byte(bypassText),
	}
}

func entry(name, expires string) string {
	return `"` + name + `" = { expires = ` + expires + `, issue = 439, reason = "r" }` + "\n"
}

// writeTree は fixture を一時ディレクトリへ置き、Makefile・make の出力・除外の宣言のパスをこの順に返す。
func writeTree(t *testing.T, files map[string]string, makefile, db, bypassText string) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	for p, body := range files {
		full := filepath.Join(dir, p)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o750))
		require.NoError(t, os.WriteFile(full, []byte(body), 0o600))
	}
	mf := filepath.Join(dir, "Makefile")
	require.NoError(t, os.WriteFile(mf, []byte(makefile), 0o600))
	dbPath := filepath.Join(dir, "db.txt")
	require.NoError(t, os.WriteFile(dbPath, []byte(db), 0o600))
	bp := filepath.Join(dir, "bypass.toml")
	require.NoError(t, os.WriteFile(bp, []byte(bypassText), 0o600))
	return mf, dbPath, bp
}

func Test_run(t *testing.T) {
	t.Parallel()

	args := func(mf, db, bp string, extra ...string) []string {
		return append([]string{"-makefile", mf, "-dir", filepath.Join(filepath.Dir(mf), ".makefiles"), "-db", db, "-db-exit", "1", "-bypass", bp, "-today", "2026-11-20"}, extra...)
	}

	t.Run("違反が無ければ、見たファイル・ターゲット・移行中の件数を出して通る", func(t *testing.T) {
		t.Parallel()
		files := with(map[string]string{".makefiles/x.mk": "x-test:\n\techo\n"})
		mf, db, bp := writeTree(t, files, makefileFor(files), dbFor(t, files), entry("x-test", "2027-01-31"))
		var out bytes.Buffer
		require.NoError(t, run(args(mf, db, bp), &out, testToday))
		assert.Equal(t, "✅ mk-test-lint: 3 ファイル / 4 ターゲット / 移行中 1 件\n", out.String())
	})
	t.Run("テストのターゲットを ファイル:行 で全件報告して落とす", func(t *testing.T) {
		t.Parallel()
		files := with(map[string]string{".makefiles/x.mk": "a:\n\techo\n\nx-test:\n\techo\n\ny-canary:\n\techo\n"})
		mf, db, bp := writeTree(t, files, makefileFor(files), dbFor(t, files), "")
		var out bytes.Buffer
		err := run(args(mf, db, bp), &out, testToday)
		require.ErrorIs(t, err, errViolations)
		assert.Contains(t, out.String(), "❌ mk-test-lint: 2 件の違反")
		assert.Contains(t, out.String(), ":4  x-test はテストのターゲットです")
		assert.Contains(t, out.String(), ":7  y-canary はテストのターゲットです")
	})
	t.Run("-today を省けば渡された現在の日付で期限を判定する", func(t *testing.T) {
		t.Parallel()
		files := with(map[string]string{".makefiles/x.mk": "x-test:\n\techo\n"})
		mf, db, bp := writeTree(t, files, makefileFor(files), dbFor(t, files), entry("x-test", "2026-11-25"))
		a := args(mf, db, bp)
		a = a[:len(a)-2]
		err := run(a, &bytes.Buffer{}, time.Date(2026, 11, 26, 15, 0, 0, 0, time.UTC))
		require.ErrorIs(t, err, errViolations)
	})
	t.Run("-today が日付でなければ読めない入力とする", func(t *testing.T) {
		t.Parallel()
		files := with(nil)
		mf, db, bp := writeTree(t, files, makefileFor(files), dbFor(t, files), "")
		err := run(append(args(mf, db, bp), "-today", "x"), &bytes.Buffer{}, testToday)
		require.ErrorIs(t, err, errUnreadable)
	})
	t.Run("知らない引数はエラーにする", func(t *testing.T) {
		t.Parallel()
		require.Error(t, run([]string{"-x"}, &bytes.Buffer{}, testToday))
	})
}

func Test_exitCode(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name string
		err  error
		want int
	}{
		{"成功は0", nil, 0},
		{"違反は1", xerrors.Wrap(errViolations, "x"), 1},
		{"読めない入力は2", xerrors.Wrap(errUnreadable, "x"), 2},
		{"0件は2", xerrors.Wrap(errNoTargets, "x"), 2},
		{"件数の食い違いは2", xerrors.Wrap(errMismatch, "x"), 2},
		{"構文の誤りは2", xerrors.Wrap(errSyntax, "x"), 2},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, exitCode(c.err))
		})
	}
}

func Test_load(t *testing.T) {
	t.Parallel()

	t.Run("`.mk` を Makefile の置き場からの相対パスで集め、`.mk` でないファイルを除く", func(t *testing.T) {
		t.Parallel()
		files := with(map[string]string{".makefiles/README.md": "x"})
		mf, db, bp := writeTree(t, files, "m", "d", "b")
		in, err := load(mf, filepath.Join(filepath.Dir(mf), ".makefiles"), db, 1, bp)
		require.NoError(t, err)
		assert.Equal(t, []string{".makefiles/go.mk", ".makefiles/tf.mk"}, keys(in.mk))
		assert.Equal(t, "m", string(in.makefile))
		assert.Equal(t, "d", string(in.db))
		assert.Equal(t, "b", string(in.bypass))
	})
	for _, missing := range []string{"Makefile", "db.txt", "bypass.toml", ".makefiles"} {
		t.Run(missing+" を読めなければ読めない入力とする", func(t *testing.T) {
			t.Parallel()
			mf, db, bp := writeTree(t, baseMk, "m", "d", "b")
			dir := filepath.Dir(mf)
			require.NoError(t, os.RemoveAll(filepath.Join(dir, missing)))
			_, err := load(mf, filepath.Join(dir, ".makefiles"), db, 1, bp)
			require.ErrorIs(t, err, errUnreadable)
		})
	}
}

func Test_check(t *testing.T) {
	t.Parallel()

	t.Run("違反の無い構成は、ファイル・ターゲット・移行中の件数を返す", func(t *testing.T) {
		t.Parallel()
		res, err := check(inputFor(t, baseMk, ""), testToday)
		require.NoError(t, err)
		assert.Empty(t, res.findings)
		assert.Equal(t, result{files: 2, targets: 3}, res)
	})
	t.Run("名前に変数を含む規則は make の展開した名前で判定する", func(t *testing.T) {
		t.Parallel()
		files := with(map[string]string{".makefiles/x.mk": "$(X):\n\techo\n"})
		in := inputFor(t, files, "")
		in.db = []byte(strings.Replace(string(in.db), dbHead+"# Files\n\n", dbHead+"# Files\n\nv-canary:\n#  commands to execute (from `.makefiles/x.mk', line 1):\n\n", 1))
		res, err := check(in, testToday)
		require.NoError(t, err)
		require.Len(t, res.findings, 1)
		assert.Contains(t, res.findings[0].Message, "v-canary はテストのターゲットです")
	})
	t.Run("レシピを持たない規則のテストの名前も判定する", func(t *testing.T) {
		t.Parallel()
		res, err := check(inputFor(t, with(map[string]string{".makefiles/x.mk": "x-test: go-test\n"}), ""), testToday)
		require.NoError(t, err)
		require.Len(t, res.findings, 1)
		assert.Equal(t, ".makefiles/x.mk", res.findings[0].File)
	})

	findingsOf := func(t *testing.T, files map[string]string, bypassText string) []string {
		t.Helper()
		res, err := check(inputFor(t, files, bypassText), testToday)
		require.NoError(t, err)
		var msgs []string
		for _, f := range res.findings {
			msgs = append(msgs, f.Message)
		}
		return msgs
	}
	x := map[string]string{".makefiles/x.mk": "x-test:\n\techo\n"}
	for _, c := range []struct {
		name   string
		files  map[string]string
		bypass string
		want   string
	}{
		{"期限の切れたエントリは除外せず、エントリも報告する", with(x), entry("x-test", "2026-11-19"), "期限 2026-11-19 が切れています"},
		{"上限を越えたエントリは除外しない", with(x), entry("x-test", "2027-02-21"), "上限（2026-11-20 から 3 ヶ月 = 2027-02-20）を越えています"},
		{"定義されていないターゲットのエントリを落とす", baseMk, entry("gone-test", "2027-01-31"), "gone-test は定義されていません"},
		{"テストの名前に当たらないエントリを落とす", with(map[string]string{".makefiles/x.mk": "lint:\n\techo\n"}), entry("lint", "2027-01-31"), "lint はテストの名前に当たりません"},
		{"runner のエントリは不要として落とす", baseMk, entry("go-test", "2027-01-31"), "go-test は runner として除外済みです"},
		{"runner が定義されていなければ除外の前提が崩れたとして落とす", map[string]string{".makefiles/go.mk": baseMk[".makefiles/go.mk"]}, "", "除外する runner tf-test が定義されていません"},
		{"runner のレシピが道具を起動しなければ落とす", map[string]string{".makefiles/go.mk": "go-test:\n\techo fake\n", ".makefiles/tf.mk": baseMk[".makefiles/tf.mk"]}, "", "go-test は"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			msgs := findingsOf(t, c.files, c.bypass)
			assert.NotEmpty(t, msgs)
			assert.Contains(t, strings.Join(msgs, "\n"), c.want)
		})
	}
	t.Run("期限の当日のエントリは除外する", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, findingsOf(t, with(x), entry("x-test", "2026-11-20")))
	})

	for _, c := range []struct {
		name   string
		mutate func(in *input)
		want   error
	}{
		{"`.mk` が0個なら落とす", func(in *input) { in.mk = map[string][]byte{} }, errNoTargets},
		{"include されない `.mk` があれば落とす", func(in *input) { in.mk[".makefiles/y.mk"] = []byte("y:\n\techo\n") }, errMismatch},
		{"実在しない include があれば落とす", func(in *input) { in.makefile = append(in.makefile, []byte("include .makefiles/z.mk\n")...) }, errMismatch},
		{"Makefile を解釈できなければ落とす", func(in *input) { in.makefile = []byte("-include x.mk\n") }, errSyntax},
		{"make の出力を読めなければ落とす", func(in *input) { in.dbExit = 2 }, errUnreadable},
		{"宣言を読めなければ落とす", func(in *input) { in.bypass = []byte("x") }, errSyntax},
		{"`.mk` を解釈できなければ落とす", func(in *input) { in.mk[".makefiles/go.mk"] = []byte("define F\n") }, errSyntax},
		{"make にだけある規則があれば落とす", func(in *input) {
			in.db = []byte(strings.Replace(string(in.db), dbHead+"# Files\n\n", dbHead+"# Files\n\nhidden:\n#  commands to execute (from `.makefiles/go.mk', line 9):\n\n", 1))
		}, errMismatch},
		{"ターゲットが1つも無ければ落とす", func(in *input) {
			in.mk = map[string][]byte{".makefiles/v.mk": []byte("X := 1\n")}
			in.makefile = []byte("include .makefiles/v.mk\n")
			in.db = []byte(dbHead + "# Files\n\nother:\n#  commands to execute (from `Makefile', line 1):\n\n" + dbTail)
		}, errNoTargets},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			in := inputFor(t, baseMk, "")
			c.mutate(&in)
			_, err := check(in, testToday)
			require.ErrorIs(t, err, c.want)
		})
	}
}

func Test_reconcile(t *testing.T) {
	t.Parallel()

	mk, err := parseMk([]byte("a:\n\techo\n\nb: a\n\n$(X):\n\techo\n"))
	require.NoError(t, err)

	t.Run("リテラルの名前と、make にだけある変数の規則の名前を合わせて返す", func(t *testing.T) {
		t.Parallel()
		got, err := reconcile("x.mk", mk, map[string]struct{}{"a": {}, "v": {}})
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b", "v"}, got)
	})
	t.Run("レシピを持つ規則が make に無ければ落とす", func(t *testing.T) {
		t.Parallel()
		_, err := reconcile("x.mk", mk, map[string]struct{}{"v": {}, "w": {}})
		require.ErrorIs(t, err, errMismatch)
	})
	t.Run("make にだけある規則が変数の規則の数と合わなければ落とす", func(t *testing.T) {
		t.Parallel()
		_, err := reconcile("x.mk", mk, map[string]struct{}{"a": {}})
		require.ErrorIs(t, err, errMismatch)
	})
}

func Test_sameSet(t *testing.T) {
	t.Parallel()

	t.Run("同じ集合なら通る", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, sameSet("include", []string{"a", "b"}, []string{"b", "a"}))
	})
	t.Run("片方にだけある要素を両側とも名指して落とす", func(t *testing.T) {
		t.Parallel()
		err := sameSet("include", []string{"a", "c"}, []string{"a", "b"})
		require.ErrorIs(t, err, errMismatch)
		assert.Contains(t, err.Error(), "include されない `.mk` [b]、実在しない include [c]")
	})
}

func Test_keys(t *testing.T) {
	t.Parallel()

	t.Run("キーを昇順で返す", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"a", "b"}, keys(map[string]int{"b": 1, "a": 2}))
		assert.Empty(t, keys(map[string]int{}))
	})
}

func Test_isTestName(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]bool{
		"tf-gates-test":         true,
		"kics-canary":           true,
		"kics-canary-threshold": true,
		"go-test-cover":         false,
		"test-mapping":          false,
		"contest":               false,
		"latest":                false,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, want, isTestName(name))
		})
	}
}
