package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTools は、段ごとのシェルの台本で外部コマンドを差し替える toolFunc を作り、起動した段を記録します。
type fakeTools struct {
	mu      sync.Mutex
	scripts map[string]string
	steps   []string
}

func (f *fakeTools) tool(dir, step string) *exec.Cmd {
	f.mu.Lock()
	f.steps = append(f.steps, step)
	f.mu.Unlock()
	cmd := exec.CommandContext(context.Background(), "sh", "-c", f.scripts[step])
	cmd.Dir = dir

	return cmd
}

func (f *fakeTools) called() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.steps...)
}

// testDeps の execTool・self・wd・exit は呼ばれると panic する。必要なテストだけが差し替える。
func testDeps(f *fakeTools, stderr io.Writer) deps {
	return deps{
		tool: f.tool,
		execTool: func(string, []string, []string) error {
			panic("execTool は呼ばれない想定")
		},
		self:   func() (string, error) { panic("self は呼ばれない想定") },
		wd:     func() (string, error) { panic("wd は呼ばれない想定") },
		sleep:  func(time.Duration) {},
		exit:   func(int) { panic("exit は呼ばれない想定") },
		stderr: stderr,
		stdout: io.Discard,
	}
}

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("サブコマンドが無ければ usage で落ちる", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, run(nil, testDeps(&fakeTools{}, io.Discard)), errUsage)
	})
	t.Run("知らないサブコマンドは usage で落ちる", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, run([]string{"canary"}, testDeps(&fakeTools{}, io.Discard)), errUsage)
	})
	t.Run("exec へ渡る", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, run([]string{"exec"}, testDeps(&fakeTools{}, io.Discard)), errExecUsage)
	})
	t.Run("lock-check へ渡る", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, run([]string{"lock-check"}, testDeps(&fakeTools{}, io.Discard)), errLockCheckUsage)
	})
	t.Run("mise-npm-disabled へ渡る", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, run([]string{"mise-npm-disabled", "x"}, testDeps(&fakeTools{}, io.Discard)), errMiseUsage)
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
			c := command("/x/d", step)
			assert.Equal(t, want, c.Args)
			assert.Equal(t, "/x/d", c.Dir)
		})
	}
}

func Test_execProcess(t *testing.T) {
	t.Parallel()
	t.Skip("テストの process そのものを置き換えるので、process の中から呼んで結果を確かめられない")
}

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o700)) //nolint:gosec // 実行できる偽の道具
}
