package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const installScript = "mkdir -p node_modules/.bin && printf '#!/bin/sh\\n' > node_modules/.bin/markdownlint-cli2 && echo installed"

type execCall struct {
	bin        string
	argv       []string
	lockExists bool
}

func Test_runExec(t *testing.T) {
	t.Parallel()

	setup := func(t *testing.T, script string) (deps, *fakeTools, *bytes.Buffer, string, string, *execCall) {
		t.Helper()
		dir := t.TempDir()
		lock := filepath.Join(t.TempDir(), "locks", "l")
		f := &fakeTools{scripts: map[string]string{stepInstall: script}}
		var stderr bytes.Buffer
		d := testDeps(f, &stderr)
		call := &execCall{}
		d.execTool = func(bin string, argv, _ []string) error {
			call.bin, call.argv = bin, argv
			_, err := os.Stat(lock)
			call.lockExists = err == nil

			return nil
		}

		return d, f, &stderr, dir, lock, call
	}

	t.Run("install を通してから node_modules/.bin の道具を引数ごと起動し、lock は起動の前に返す", func(t *testing.T) {
		t.Parallel()
		d, _, stderr, dir, lock, call := setup(t, installScript)
		require.NoError(t, runExec([]string{"-dir", dir, "-lock", lock, "markdownlint-cli2", "--fix", "a.md"}, d))
		bin := filepath.Join(dir, "node_modules", ".bin", "markdownlint-cli2")
		assert.Equal(t, bin, call.bin)
		assert.Equal(t, []string{bin, "--fix", "a.md"}, call.argv)
		assert.False(t, call.lockExists)
		assert.Contains(t, stderr.String(), "installed")
	})
	t.Run("install が落ちたら道具を起動せず、lock を返す", func(t *testing.T) {
		t.Parallel()
		d, _, _, dir, lock, _ := setup(t, "echo ERR_PNPM_OUTDATED_LOCKFILE >&2; exit 1")
		d.execTool = func(string, []string, []string) error { t.Fatal("起動された"); return nil }
		require.ErrorIs(t, runExec([]string{"-dir", dir, "-lock", lock, "markdownlint-cli2"}, d), errInstall)
		assert.NoDirExists(t, lock)
	})
	t.Run("install の後に道具が無ければ PATH を引かずに落ちる", func(t *testing.T) {
		t.Parallel()
		d, _, _, dir, lock, _ := setup(t, "true")
		d.execTool = func(string, []string, []string) error { t.Fatal("起動された"); return nil }
		require.ErrorIs(t, runExec([]string{"-dir", dir, "-lock", lock, "markdownlint-cli2"}, d), errNoTool)
	})
	t.Run("道具の名前がディレクトリなら落ちる", func(t *testing.T) {
		t.Parallel()
		d, _, _, dir, lock, _ := setup(t, "mkdir -p node_modules/.bin/markdownlint-cli2")
		require.ErrorIs(t, runExec([]string{"-dir", dir, "-lock", lock, "markdownlint-cli2"}, d), errNoTool)
	})
	for name, args := range map[string][]string{
		"-dir が無い":    {"-lock", "/l", "t"},
		"-dir が相対パス":  {"-dir", "d", "-lock", "/l", "t"},
		"-lock が相対パス": {"-dir", "/d", "-lock", "l", "t"},
		"道具の名前が無い":    {"-dir", "/d", "-lock", "/l"},
		"知らないフラグ":     {"-x", "-dir", "/d", "-lock", "/l", "t"},
	} {
		t.Run(name+"なら usage で落ちる", func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, runExec(args, testDeps(&fakeTools{}, io.Discard)), errExecUsage)
		})
	}
	for _, tool := range []string{"../x", "a/b", ".", ".."} {
		t.Run("道具の名前 "+tool+" はパスなので落ちる", func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, runExec([]string{"-dir", "/d", "-lock", "/l", tool}, testDeps(&fakeTools{}, io.Discard)), errToolName)
		})
	}
}

func Test_installLocked(t *testing.T) {
	t.Parallel()

	t.Run("install の出力は stderr へ流し、stdin を渡さない", func(t *testing.T) {
		t.Parallel()
		f := &fakeTools{scripts: map[string]string{stepInstall: "echo out; echo err >&2; if read x; then echo got-stdin; fi"}}
		var stderr bytes.Buffer
		d := testDeps(f, &stderr)
		lock := filepath.Join(t.TempDir(), "l")
		require.NoError(t, installLocked(t.TempDir(), lock, d))
		assert.Contains(t, stderr.String(), "out")
		assert.Contains(t, stderr.String(), "err")
		assert.NotContains(t, stderr.String(), "got-stdin")
		assert.NoDirExists(t, lock)
	})
	t.Run("lock を取れなければ install を起動しない", func(t *testing.T) {
		t.Parallel()
		f := &fakeTools{scripts: map[string]string{stepInstall: "true"}}
		lock := filepath.Join(t.TempDir(), "l")
		require.NoError(t, os.Mkdir(lock, 0o750))
		require.ErrorIs(t, installLocked(t.TempDir(), lock, testDeps(f, io.Discard)), errLockBusy)
		assert.Empty(t, f.called())
	})
}

func Test_acquireLock(t *testing.T) {
	t.Parallel()

	t.Run("lock を作り、release で消す。release は2回呼んでよい", func(t *testing.T) {
		t.Parallel()
		lock := filepath.Join(t.TempDir(), "a", "b", "l")
		release, err := acquireLock(lock, testDeps(&fakeTools{}, io.Discard))
		require.NoError(t, err)
		assert.DirExists(t, lock)
		release()
		release()
		assert.NoDirExists(t, lock)
	})
	t.Run("他が持つ lock は 599 回待ってから落ち、他の lock を消さない", func(t *testing.T) {
		t.Parallel()
		lock := filepath.Join(t.TempDir(), "l")
		require.NoError(t, os.Mkdir(lock, 0o750))
		d := testDeps(&fakeTools{}, io.Discard)
		var waits int
		d.sleep = func(p time.Duration) {
			assert.Equal(t, lockPoll, p)
			waits++
		}
		_, err := acquireLock(lock, d)
		require.ErrorIs(t, err, errLockBusy)
		assert.Equal(t, lockTries-1, waits)
		assert.DirExists(t, lock)
	})
	t.Run("待ちの間に他が lock を返せば取れる", func(t *testing.T) {
		t.Parallel()
		lock := filepath.Join(t.TempDir(), "l")
		require.NoError(t, os.Mkdir(lock, 0o750))
		d := testDeps(&fakeTools{}, io.Discard)
		var waits int
		d.sleep = func(time.Duration) {
			waits++
			if waits == 3 {
				require.NoError(t, os.Remove(lock))
			}
		}
		release, err := acquireLock(lock, d)
		require.NoError(t, err)
		assert.Equal(t, 3, waits)
		release()
		assert.NoDirExists(t, lock)
	})
	t.Run("lock の親を作れなければ落ちる", func(t *testing.T) {
		t.Parallel()
		file := filepath.Join(t.TempDir(), "f")
		require.NoError(t, os.WriteFile(file, nil, 0o600))
		_, err := acquireLock(filepath.Join(file, "l"), testDeps(&fakeTools{}, io.Discard))
		require.Error(t, err)
	})
	t.Run("lock の場所に作れない理由が在れば待たずに落ちる", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, os.Chmod(dir, 0o500))       //nolint:gosec // 書けないディレクトリを作る
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) //nolint:gosec // 片付けのため戻す
		if os.Geteuid() == 0 {
			t.Skip("特権実行では書けないディレクトリにも書けるため検証できない")
		}
		d := testDeps(&fakeTools{}, io.Discard)
		d.sleep = func(time.Duration) { t.Fatal("待った") }
		_, err := acquireLock(filepath.Join(dir, "l"), d)
		require.Error(t, err)
		require.NotErrorIs(t, err, errLockBusy)
	})
}

func Test_watchSignals(t *testing.T) {
	t.Parallel()

	t.Run("シグナルを受けたら取った lock を消して 128+signal で終わる", func(t *testing.T) {
		t.Parallel()
		lock := filepath.Join(t.TempDir(), "l")
		require.NoError(t, os.Mkdir(lock, 0o750))
		sigs := make(chan os.Signal, 1)
		sigs <- syscall.SIGINT
		var code atomic.Int64
		watchSignals(sigs, make(chan struct{}), func() bool { return true }, lock, func(c int) { code.Store(int64(c)) })
		assert.Equal(t, int64(130), code.Load())
		assert.NoDirExists(t, lock)
	})
	t.Run("lock を取る前のシグナルでは他の lock を消さない", func(t *testing.T) {
		t.Parallel()
		lock := filepath.Join(t.TempDir(), "l")
		require.NoError(t, os.Mkdir(lock, 0o750))
		sigs := make(chan os.Signal, 1)
		sigs <- syscall.SIGTERM
		var code atomic.Int64
		watchSignals(sigs, make(chan struct{}), func() bool { return false }, lock, func(c int) { code.Store(int64(c)) })
		assert.Equal(t, int64(143), code.Load())
		assert.DirExists(t, lock)
	})
	t.Run("done が閉じれば何もしない", func(t *testing.T) {
		t.Parallel()
		done := make(chan struct{})
		close(done)
		watchSignals(make(chan os.Signal), done, func() bool { return true }, "/nonexistent", func(int) { t.Fatal("終わった") })
	})
}
