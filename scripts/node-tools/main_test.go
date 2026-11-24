package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// asNodeTools が立っていると、テストのバイナリは node-tools として振る舞う。pnpm だけを fakePnpm の台本に差し替え、
// 排他・install の判定・syscall.Exec は本物を通す（canary が起動する self と、execProcess の確認に使う）。
const asNodeTools = "NODE_TOOLS_TEST_AS_MAIN"

// fakePnpm は、写しの宣言の中身を見て pnpm の振る舞いを真似る台本です。
//   - package.json に x-test-launch があれば、壊れていても install して道具を置く
//   - package.json の版が 0.0.0 なら ERR_PNPM_OUTDATED_LOCKFILE で落ちる
//   - lockfile の integrity が AAAA か BBBB で始まれば ERR_PNPM_TARBALL_INTEGRITY で落ちる
//     （package.json に x-test-integrity-reason があれば別の理由で落ちる）
//   - それ以外は install して、引数と X_TEST を出す道具を置く
const fakePnpm = `if grep -q x-test-launch package.json; then :
elif grep -q '"0.0.0"' package.json; then echo ERR_PNPM_OUTDATED_LOCKFILE >&2; exit 1
elif grep -qE 'sha512-(AAAA|BBBB)' pnpm-lock.yaml; then
	if grep -q x-test-integrity-reason package.json; then echo ERR_PNPM_FETCH_404 >&2; exit 1; fi
	echo ERR_PNPM_TARBALL_INTEGRITY >&2; exit 1
fi
mkdir -p node_modules/.bin
printf '#!/bin/sh\nread -r in || in=\necho launched "$@" "x=$X_TEST" "in=$in" "pid=$$"\nexit "${X_EXIT:-0}"\n' > node_modules/.bin/markdownlint-cli2
chmod +x node_modules/.bin/markdownlint-cli2
`

func TestMain(m *testing.M) {
	if os.Getenv(asNodeTools) != "" {
		d := deps{
			tool: func(ctx context.Context, dir, _ string) *exec.Cmd {
				cmd := exec.CommandContext(ctx, "sh", "-c", fakePnpm)
				cmd.Dir = dir

				return cmd
			},
			execTool: execProcess,
			self:     os.Executable,
			wd:       os.Getwd,
			sleep:    time.Sleep,
			stderr:   os.Stderr,
		}
		if err := run(context.Background(), os.Args[1:], d); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	// このプロセスが起動する子（canary の self を含む）は node-tools として振る舞う。
	if err := os.Setenv(asNodeTools, "1"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func selfAsNodeTools(t *testing.T) string {
	t.Helper()
	self, err := os.Executable()
	require.NoError(t, err)

	return self
}

type toolCall struct {
	dir  string
	step string
}

// fakeTools は、段ごとのシェルの台本で外部コマンドを差し替え、起動した段とディレクトリを記録します。
// 台本の無い段は exit 99 で落ちる（台本の付け忘れを成功として通さない）。
type fakeTools struct {
	mu      sync.Mutex
	scripts map[string]string
	calls   []toolCall
}

func (f *fakeTools) tool(ctx context.Context, dir, step string) *exec.Cmd {
	f.mu.Lock()
	f.calls = append(f.calls, toolCall{dir: dir, step: step})
	script, ok := f.scripts[step]
	f.mu.Unlock()
	if !ok {
		script = "exit 99"
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", script)
	cmd.Dir = dir
	// 呼び出し側が stdin を外さなければ、台本はこれを読める。
	cmd.Stdin = strings.NewReader("payload\n")

	return cmd
}

func (f *fakeTools) called() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	steps := make([]string, 0, len(f.calls))
	for _, c := range f.calls {
		steps = append(steps, c.step)
	}

	return steps
}

func (f *fakeTools) dirs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	dirs := make([]string, 0, len(f.calls))
	for _, c := range f.calls {
		dirs = append(dirs, c.dir)
	}

	return dirs
}

// testDeps の execTool・self・wd は呼ばれると panic する。必要なテストだけが差し替える。
func testDeps(f *fakeTools, stderr io.Writer) deps {
	return deps{
		tool: f.tool,
		execTool: func(string, []string, []string) error {
			panic("execTool は呼ばれない想定")
		},
		self:   func() (string, error) { panic("self は呼ばれない想定") },
		wd:     func() (string, error) { panic("wd は呼ばれない想定") },
		sleep:  func(time.Duration) {},
		stderr: stderr,
	}
}

func Test_run(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	t.Run("サブコマンドが無ければ usage で落ちる", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, run(ctx, nil, testDeps(&fakeTools{}, io.Discard)), errUsage)
	})
	t.Run("知らないサブコマンドは usage で落ちる", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, run(ctx, []string{"canary"}, testDeps(&fakeTools{}, io.Discard)), errUsage)
	})
	t.Run("exec へ渡り、install の後に道具を起動する", func(t *testing.T) {
		t.Parallel()
		f := &fakeTools{scripts: map[string]string{stepInstall: installScript}}
		d := testDeps(f, io.Discard)
		var got string
		d.execTool = func(bin string, _, _ []string) error {
			got = bin

			return nil
		}
		dir := t.TempDir()
		require.NoError(t, run(ctx, []string{"exec", "-dir", dir, "-lock", filepath.Join(t.TempDir(), "l"), "markdownlint-cli2"}, d))
		assert.Equal(t, filepath.Join(dir, "node_modules", ".bin", "markdownlint-cli2"), got)
		assert.Equal(t, []string{stepInstall}, f.called())
	})
	t.Run("lock-check へ渡る", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, run(ctx, []string{"lock-check"}, testDeps(&fakeTools{}, io.Discard)), errLockCheckUsage)
	})
	t.Run("mise-npm-disabled へ渡る", func(t *testing.T) {
		t.Parallel()
		f := &fakeTools{scripts: map[string]string{stepMiseLs: `echo '{"go": []}'`}}
		require.NoError(t, run(ctx, []string{"mise-npm-disabled"}, testDeps(f, io.Discard)))
		assert.Equal(t, []string{stepMiseLs}, f.called())
	})
}

func Test_command(t *testing.T) {
	t.Parallel()

	for step, want := range map[string][]string{
		stepInstall:   {"pnpm", "install", "--frozen-lockfile", "--prod", "--reporter=silent"},
		stepLockCheck: {"pnpm", "install", "--frozen-lockfile", "--lockfile-only"},
		stepMiseLs:    {"mise", "ls", "--current", "--local", "--json"},
	} {
		t.Run(step+" の引数は固定でディレクトリを作業ディレクトリで渡す", func(t *testing.T) {
			t.Parallel()
			c := command(context.Background(), "/x/d", step)
			assert.Equal(t, want, c.Args)
			assert.Equal(t, "/x/d", c.Dir)
		})
	}
	t.Run("知らない段は別の道具へ落とさず panic する", func(t *testing.T) {
		t.Parallel()
		assert.Panics(t, func() { command(context.Background(), "/x/d", "unknown-step") })
	})
}

func Test_execProcess(t *testing.T) {
	t.Parallel()

	t.Run("bin が無ければ置き換わらずにエラーを返す", func(t *testing.T) {
		t.Parallel()
		err := execProcess(filepath.Join(t.TempDir(), "none"), []string{"none"}, os.Environ())
		require.ErrorIs(t, err, syscall.ENOENT)
	})
	t.Run("道具に置き換わり、引数と環境がそのまま届く", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "pnpm-lock.yaml"), nil, 0o600))
		cmd := exec.CommandContext(t.Context(), selfAsNodeTools(t), "exec", "-dir", dir, "-lock", filepath.Join(t.TempDir(), "l"), "markdownlint-cli2", "--fix", "a b.md")
		cmd.Env = append(os.Environ(), "X_TEST=v", "X_EXIT=7")
		cmd.Stdin = strings.NewReader("payload\n")
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		err := cmd.Run()
		// 道具の終了コードがそのまま返り、stdin が道具へ届き、道具の pid が起動した process の pid と同じ（置き換わった）。
		var exitErr *exec.ExitError
		require.ErrorAs(t, err, &exitErr, out.String())
		assert.Equal(t, 7, exitErr.ExitCode())
		assert.Equal(t, fmt.Sprintf("launched --fix a b.md x=v in=payload pid=%d\n", cmd.Process.Pid), out.String())
	})
}

func Test_exitCode(t *testing.T) {
	t.Parallel()

	t.Run("中断は 130 で終わる", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, 130, exitCode(fmt.Errorf("x: %w", errInterrupted)))
	})
	t.Run("ほかの失敗は 1 で終わる", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, 1, exitCode(errInstall))
	})
}

func Test_stopGently(t *testing.T) {
	t.Parallel()

	t.Run("中断で子へ SIGTERM を送り、子が片付けてから終わるのを待ち、待ちには上限を置く", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		out := filepath.Join(t.TempDir(), "out")
		cmd := exec.CommandContext(ctx, "sh", "-c", `trap 'echo term > "$0"; exit 0' TERM; echo ready; while :; do sleep 0.1; done`, out)
		stopGently(cmd)
		assert.Equal(t, stopGrace, cmd.WaitDelay)
		stdout, err := cmd.StdoutPipe()
		require.NoError(t, err)
		require.NoError(t, cmd.Start())
		buf := make([]byte, 6)
		_, err = io.ReadFull(stdout, buf)
		require.NoError(t, err)
		cancel()
		_ = cmd.Wait()
		got, err := os.ReadFile(out) //nolint:gosec // t.TempDir 配下
		require.NoError(t, err)
		assert.Equal(t, "term\n", string(got))
	})
}
