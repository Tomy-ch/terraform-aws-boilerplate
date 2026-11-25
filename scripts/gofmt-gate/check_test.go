package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/testenv"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun/tooltest"
)

func writeFile(t *testing.T, p, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	writeFile(t, filepath.Join(root, "src", "a.go"), "package a\n")
	writeFile(t, filepath.Join(root, "src", "lib", "b.go"), "package lib\n")
	return root
}

func passingFake(t *testing.T) *tooltest.Fake {
	t.Helper()
	f := tooltest.New(t)
	f.Set(stepCanary, "out", canaryOut)
	return f
}

var checkArgs = []string{"-work", "tmp/gofmt-gate", "-dir", "src"}

func steps(calls []string) []string {
	var out []string
	for _, c := range calls {
		out = append(out, strings.Fields(c)[1])
	}
	return out
}

func Test_runCheck(t *testing.T) {
	t.Parallel()

	t.Run("canary・本走査の順に走り、本走査が何も挙げなければ件数を出して通る", func(t *testing.T) {
		t.Parallel()
		root := repoRoot(t)
		f := passingFake(t)
		var out bytes.Buffer
		require.NoError(t, runCheck(checkArgs, &out, f.Tool(), root))
		assert.Equal(t, []string{stepCanary, stepMain}, steps(f.Calls()))
		assert.Equal(t, filepath.Join(root, "src"), strings.Fields(f.Calls()[1])[0], "本走査は -dir で走る")
		assert.Contains(t, out.String(), "✅ gofmt canary")
		assert.Contains(t, out.String(), "✅ gofmt: src の .go 2 件は整形済みです")
	})

	t.Run("canary が成り立たなければ、本走査を起動せずに errCanary", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t)
		require.ErrorIs(t, runCheck(checkArgs, new(bytes.Buffer), f.Tool(), repoRoot(t)), errCanary)
		assert.Equal(t, []string{stepCanary}, steps(f.Calls()))
	})

	t.Run(".go が0件なら、道具を起動せずに errNoInput", func(t *testing.T) {
		t.Parallel()
		root, err := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)
		writeFile(t, filepath.Join(root, "src", "README.md"), "x\n")
		f := passingFake(t)
		require.ErrorIs(t, runCheck(checkArgs, new(bytes.Buffer), f.Tool(), root), errNoInput)
		assert.Nil(t, f.Calls())
	})

	t.Run("-dir が無ければ、道具を起動せずに errRead", func(t *testing.T) {
		t.Parallel()
		f := passingFake(t)
		args := []string{"-work", "tmp/gofmt-gate", "-dir", "none"}
		require.ErrorIs(t, runCheck(args, new(bytes.Buffer), f.Tool(), repoRoot(t)), errRead)
		assert.Nil(t, f.Calls())
	})

	t.Run("本走査が rc≠0 なら errExit で、canary の後に起動している", func(t *testing.T) {
		t.Parallel()
		f := passingFake(t)
		f.Set(stepMain, "rc", "2")
		require.ErrorIs(t, runCheck(checkArgs, new(bytes.Buffer), f.Tool(), repoRoot(t)), errExit)
		assert.Equal(t, []string{stepCanary, stepMain}, steps(f.Calls()))
	})

	for name, args := range map[string][]string{
		"-work が無い":       {"-dir", "src"},
		"-dir が無い":        {"-work", "tmp/gofmt-gate"},
		"余分な引数":           {"-work", "tmp/gofmt-gate", "-dir", "src", "x"},
		"-work が tmp/ の外": {"-work", "elsewhere", "-dir", "src"},
		"-dir が外へ出る":      {"-work", "tmp/gofmt-gate", "-dir", "../src"},
		"-dir が絶対パス":      {"-work", "tmp/gofmt-gate", "-dir", "/src"},
		"知らない flag":       {"-no-such-flag"},
	} {
		t.Run(name+"なら、道具を起動せずに errUsage", func(t *testing.T) {
			t.Parallel()
			f := passingFake(t)
			require.ErrorIs(t, runCheck(args, new(bytes.Buffer), f.Tool(), repoRoot(t)), errUsage)
			assert.Nil(t, f.Calls())
		})
	}
}

func Test_countGoFiles(t *testing.T) {
	t.Parallel()

	t.Run("入れ子と隠しディレクトリの .go を数え、. で始まる名前と .go でないものは数えない", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		for _, p := range []string{"a.go", "lib/b.go", ".hidden/c.go", ".d.go", "e.go.txt", "README.md"} {
			writeFile(t, filepath.Join(dir, filepath.FromSlash(p)), "x\n")
		}
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "dir.go"), 0o750))
		n, err := countGoFiles(dir)
		require.NoError(t, err)
		assert.Equal(t, 3, n)
	})

	t.Run("空のディレクトリは errNoInput", func(t *testing.T) {
		t.Parallel()
		_, err := countGoFiles(t.TempDir())
		require.ErrorIs(t, err, errNoInput)
	})

	t.Run("無いディレクトリは errRead", func(t *testing.T) {
		t.Parallel()
		_, err := countGoFiles(filepath.Join(t.TempDir(), "none"))
		require.ErrorIs(t, err, errRead)
	})

	t.Run("ファイルを渡せば errRead", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(t.TempDir(), "a.go")
		writeFile(t, p, "package a\n")
		_, err := countGoFiles(p)
		require.ErrorIs(t, err, errRead)
	})

	t.Run("読めないディレクトリがあれば errRead", func(t *testing.T) {
		t.Parallel()
		testenv.RequireNonRoot(t, "root は権限を外したディレクトリも読めるので、この分岐に届かない")
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "a.go"), "x\n")
		locked := filepath.Join(dir, "locked")
		require.NoError(t, os.MkdirAll(locked, 0o750))
		require.NoError(t, os.Chmod(locked, 0))
		t.Cleanup(func() { _ = os.Chmod(locked, 0o750) })
		_, err := countGoFiles(dir)
		require.ErrorIs(t, err, errRead)
	})
}

func Test_runScan(t *testing.T) {
	t.Parallel()

	t.Run("何も挙げず rc=0 なら件数を出して通る", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t)
		var out bytes.Buffer
		require.NoError(t, runScan(t.TempDir(), "scripts", 7, &out, f.Tool()))
		assert.Equal(t, "✅ gofmt: scripts の .go 7 件は整形済みです\n", out.String())
	})

	t.Run("挙がったファイルを label からのパスで1行ずつ出し、件数とともに errFailed", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t)
		f.Set(stepMain, "out", "a.go\n./lib/b.go\n")
		var out bytes.Buffer
		err := runScan(t.TempDir(), "scripts", 7, &out, f.Tool())
		require.ErrorIs(t, err, errFailed)
		assert.Contains(t, err.Error(), "2 件")
		assert.Equal(t, "scripts/a.go\nscripts/lib/b.go\n", out.String())
	})

	t.Run("rc≠0 なら、一覧が空でも出力を写して errExit（構文の誤りを整形済みにしない）", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t)
		f.Set(stepMain, "err", "a.go:1:1: expected 'package'\n")
		f.Set(stepMain, "rc", "2")
		var out bytes.Buffer
		err := runScan(t.TempDir(), "scripts", 7, &out, f.Tool())
		require.ErrorIs(t, err, errExit)
		assert.Contains(t, err.Error(), "rc=2")
		assert.Contains(t, out.String(), "expected 'package'")
		assert.NotContains(t, out.String(), "✅")
	})

	t.Run("rc≠0 なら、一覧が空でなくても errFailed ではなく errExit", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t)
		f.Set(stepMain, "out", "a.go\n")
		f.Set(stepMain, "rc", "2")
		err := runScan(t.TempDir(), "scripts", 7, new(bytes.Buffer), f.Tool())
		require.ErrorIs(t, err, errExit)
		require.NotErrorIs(t, err, errFailed)
	})

	t.Run("道具を起動できなければ errTool", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, runScan(t.TempDir(), "scripts", 7, new(bytes.Buffer), tooltest.Missing), toolrun.ErrTool)
	})
}
