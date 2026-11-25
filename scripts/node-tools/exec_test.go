package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/testenv"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const installScript = "mkdir -p node_modules/.bin && printf '#!/bin/sh\\n' > node_modules/.bin/markdownlint-cli2 && echo installed"

type execCall struct {
	bin        string
	argv       []string
	env        []string
	lockExists bool
}

func Test_runExec(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	setup := func(t *testing.T, script string) (deps, *bytes.Buffer, string, string, *execCall) {
		t.Helper()
		dir := t.TempDir()
		lock := filepath.Join(t.TempDir(), "locks", "l")
		f := &fakeTools{scripts: map[string]string{stepInstall: script}}
		var stderr bytes.Buffer
		d := testDeps(f, &stderr)
		call := &execCall{}
		d.execTool = func(bin string, argv, env []string) error {
			call.bin, call.argv, call.env = bin, argv, env
			_, err := os.Stat(lock)
			call.lockExists = err == nil

			return nil
		}

		return d, &stderr, dir, lock, call
	}

	t.Run("install を通してから node_modules/.bin の道具を引数と環境ごと起動し、lock は起動の前に返す", func(t *testing.T) {
		t.Parallel()
		d, stderr, dir, lock, call := setup(t, installScript)
		require.NoError(t, runExec(ctx, []string{"-dir", dir, "-lock", lock, "markdownlint-cli2", "--fix", "a.md"}, d))
		bin := filepath.Join(dir, "node_modules", ".bin", "markdownlint-cli2")
		assert.Equal(t, bin, call.bin)
		assert.Equal(t, []string{bin, "--fix", "a.md"}, call.argv)
		assert.Equal(t, os.Environ(), call.env)
		assert.False(t, call.lockExists)
		assert.Contains(t, stderr.String(), "installed")
	})
	t.Run("install が落ちたら道具を起動せず、pnpm の出力を stderr へ流し、lock を返す", func(t *testing.T) {
		t.Parallel()
		d, stderr, dir, lock, _ := setup(t, "echo ERR_PNPM_OUTDATED_LOCKFILE >&2; exit 1")
		d.execTool = func(string, []string, []string) error { t.Fatal("起動された"); return nil }
		require.ErrorIs(t, runExec(ctx, []string{"-dir", dir, "-lock", lock, "markdownlint-cli2"}, d), errInstall)
		assert.Contains(t, stderr.String(), "ERR_PNPM_OUTDATED_LOCKFILE")
		assert.NoDirExists(t, lock)
	})
	t.Run("-dir のディレクトリが無ければ install を起動できず ErrTool で不成立（2）", func(t *testing.T) {
		t.Parallel()
		d, _, _, lock, _ := setup(t, installScript)
		err := runExec(ctx, []string{"-dir", filepath.Join(t.TempDir(), "none"), "-lock", lock, "markdownlint-cli2"}, d)
		require.ErrorIs(t, err, toolrun.ErrTool)
		assert.Equal(t, 2, exitCode(err))
	})
	t.Run("install が非0で終われば errInstall で不成立（2）、道具を起動しない", func(t *testing.T) {
		t.Parallel()
		d, _, dir, lock, _ := setup(t, "exit 4")
		d.execTool = func(string, []string, []string) error { t.Fatal("起動された"); return nil }
		err := runExec(ctx, []string{"-dir", dir, "-lock", lock, "markdownlint-cli2"}, d)
		require.ErrorIs(t, err, errInstall)
		assert.Contains(t, err.Error(), "exit status 4")
		assert.Equal(t, 2, exitCode(err))
	})
	t.Run("install の後に道具が無ければ PATH を引かずに落ちる", func(t *testing.T) {
		t.Parallel()
		d, _, dir, lock, _ := setup(t, "true")
		d.execTool = func(string, []string, []string) error { t.Fatal("起動された"); return nil }
		require.ErrorIs(t, runExec(ctx, []string{"-dir", dir, "-lock", lock, "markdownlint-cli2"}, d), errNoTool)
	})
	t.Run("道具の名前がディレクトリなら落ちる", func(t *testing.T) {
		t.Parallel()
		d, _, dir, lock, _ := setup(t, "mkdir -p node_modules/.bin/markdownlint-cli2")
		require.ErrorIs(t, runExec(ctx, []string{"-dir", dir, "-lock", lock, "markdownlint-cli2"}, d), errNoTool)
	})
	t.Run("install の後に中断されていれば道具を起動しない", func(t *testing.T) {
		t.Parallel()
		cctx, cancel := context.WithCancel(ctx)
		d, _, dir, lock, _ := setup(t, installScript)
		d.tool = func(c context.Context, dir string, env []string, step string, _ ...string) *exec.Cmd {
			cmd := (&fakeTools{scripts: map[string]string{stepInstall: installScript}}).tool(c, dir, env, step)
			cancel()

			return cmd
		}
		d.execTool = func(string, []string, []string) error { t.Fatal("起動された"); return nil }
		require.ErrorIs(t, runExec(cctx, []string{"-dir", dir, "-lock", lock, "markdownlint-cli2"}, d), errInterrupted)
	})
	t.Run("起動に失敗したらそのエラーを返す", func(t *testing.T) {
		t.Parallel()
		d, _, dir, lock, _ := setup(t, installScript)
		sentinel := errors.New("exec failed")
		d.execTool = func(string, []string, []string) error { return sentinel }
		require.ErrorIs(t, runExec(ctx, []string{"-dir", dir, "-lock", lock, "markdownlint-cli2"}, d), sentinel)
	})
	for name, args := range map[string][]string{
		"-dir が無い":    {"-lock", "/l", "t"},
		"-dir が相対パス":  {"-dir", "d", "-lock", "/l", "t"},
		"-lock が無い":   {"-dir", "/d", "t"},
		"-lock が相対パス": {"-dir", "/d", "-lock", "l", "t"},
		"道具の名前が無い":    {"-dir", "/d", "-lock", "/l"},
		"知らないフラグ":     {"-x", "-dir", "/d", "-lock", "/l", "t"},
	} {
		t.Run(name+"なら usage で落ちる", func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, runExec(ctx, args, testDeps(&fakeTools{}, io.Discard)), errExecUsage)
		})
	}
	for _, tool := range []string{"../x", "a/b", ".", "..", ""} {
		t.Run("道具の名前 "+tool+" は install の前に落ちる", func(t *testing.T) {
			t.Parallel()
			f := &fakeTools{}
			require.ErrorIs(t, runExec(ctx, []string{"-dir", "/d", "-lock", "/l", "--", tool}, testDeps(f, io.Discard)), errToolName)
			assert.Empty(t, f.called())
		})
	}
}

func Test_installLocked(t *testing.T) {
	t.Parallel()

	t.Run("install の出力は stderr へ流し、stdin を渡さない", func(t *testing.T) {
		t.Parallel()
		f := &fakeTools{scripts: map[string]string{stepInstall: "echo out; echo err >&2; if read x; then echo got-$x; fi"}}
		var stderr bytes.Buffer
		lock := filepath.Join(t.TempDir(), "l")
		require.NoError(t, installLocked(context.Background(), t.TempDir(), lock, testDeps(f, &stderr)))
		assert.Contains(t, stderr.String(), "out")
		assert.Contains(t, stderr.String(), "err")
		assert.NotContains(t, stderr.String(), "got-payload")
		assert.NoDirExists(t, lock)
	})
	t.Run("lock を取れなければ install を起動しない", func(t *testing.T) {
		t.Parallel()
		f := &fakeTools{scripts: map[string]string{stepInstall: "true"}}
		lock := filepath.Join(t.TempDir(), "l")
		require.NoError(t, os.Mkdir(lock, 0o750))
		require.ErrorIs(t, installLocked(context.Background(), t.TempDir(), lock, testDeps(f, io.Discard)), errLockBusy)
		assert.Empty(t, f.called())
	})
	t.Run("install の途中で中断されたら install を止め、lock を返して中断として落ちる", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		dir := t.TempDir()
		f := &fakeTools{scripts: map[string]string{stepInstall: "touch started; exec sleep 30"}}
		lock := filepath.Join(t.TempDir(), "l")
		go func() {
			// install が起動してから中断する（起動の前の中断は acquireLock の側の経路になる）。
			for {
				if _, err := os.Stat(filepath.Join(dir, "started")); err == nil {
					cancel()

					return
				}
				time.Sleep(10 * time.Millisecond)
			}
		}()
		start := time.Now()
		require.ErrorIs(t, installLocked(ctx, dir, lock, testDeps(f, io.Discard)), errInterrupted)
		assert.FileExists(t, filepath.Join(dir, "started"))
		assert.Less(t, time.Since(start), 10*time.Second)
		assert.NoDirExists(t, lock)
	})
}

func Test_acquireLock(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	t.Run("lock を作り、release で消す。release は2回呼んでよい", func(t *testing.T) {
		t.Parallel()
		lock := filepath.Join(t.TempDir(), "a", "b", "l")
		release, err := acquireLock(ctx, lock, testDeps(&fakeTools{}, io.Discard))
		require.NoError(t, err)
		assert.DirExists(t, lock)
		release()
		release()
		assert.NoDirExists(t, lock)
	})
	t.Run("errLockBusy の文言の2分と待ちの合計が合う", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, 2*time.Minute, lockPoll*lockTries)
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
		_, err := acquireLock(ctx, lock, d)
		require.ErrorIs(t, err, errLockBusy)
		assert.Equal(t, lockTries-1, waits)
		assert.DirExists(t, lock)
	})
	t.Run("最後の試行の前に他が lock を返せば取れる", func(t *testing.T) {
		t.Parallel()
		lock := filepath.Join(t.TempDir(), "l")
		require.NoError(t, os.Mkdir(lock, 0o750))
		d := testDeps(&fakeTools{}, io.Discard)
		var waits int
		d.sleep = func(time.Duration) {
			waits++
			if waits == lockTries-1 {
				require.NoError(t, os.Remove(lock))
			}
		}
		release, err := acquireLock(ctx, lock, d)
		require.NoError(t, err)
		assert.Equal(t, lockTries-1, waits)
		release()
		assert.NoDirExists(t, lock)
	})
	t.Run("待ちの間に中断されたら、他の lock を消さずに中断として落ちる", func(t *testing.T) {
		t.Parallel()
		lock := filepath.Join(t.TempDir(), "l")
		require.NoError(t, os.Mkdir(lock, 0o750))
		cctx, cancel := context.WithCancel(ctx)
		d := testDeps(&fakeTools{}, io.Discard)
		var waits int
		d.sleep = func(time.Duration) {
			waits++
			if waits == 3 {
				cancel()
			}
		}
		_, err := acquireLock(cctx, lock, d)
		require.ErrorIs(t, err, errInterrupted)
		assert.Equal(t, 3, waits)
		assert.DirExists(t, lock)
	})
	t.Run("lock の親を作れなければ落ちる", func(t *testing.T) {
		t.Parallel()
		file := filepath.Join(t.TempDir(), "f")
		require.NoError(t, os.WriteFile(file, nil, 0o600))
		_, err := acquireLock(ctx, filepath.Join(file, "l"), testDeps(&fakeTools{}, io.Discard))
		require.ErrorIs(t, err, syscall.ENOTDIR)
	})
	t.Run("lock の場所に作れない理由が在れば待たずに落ちる", func(t *testing.T) {
		t.Parallel()
		testenv.RequireNonRoot(t, "特権実行では書けないディレクトリにも書けるため検証できない")
		dir := t.TempDir()
		require.NoError(t, os.Chmod(dir, 0o500))       //nolint:gosec // 書けないディレクトリを作る
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) //nolint:gosec // 片付けのため戻す
		d := testDeps(&fakeTools{}, io.Discard)
		d.sleep = func(time.Duration) { t.Fatal("待った") }
		_, err := acquireLock(ctx, filepath.Join(dir, "l"), d)
		require.ErrorIs(t, err, fs.ErrPermission)
	})
}
