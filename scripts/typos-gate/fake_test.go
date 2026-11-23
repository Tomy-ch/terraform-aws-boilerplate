package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// fake は偽の道具の制御です。段ごとに stdout・stderr・終了コードを決め、起動ごとに「作業ディレクトリ 段 GIT_DIR」を
// 控えます。
type fake struct {
	t   *testing.T
	dir string
}

// newFake は、制御用のディレクトリ（作業ディレクトリとは別）に偽の道具を置きます。段の `<段>.out` を stdout へ、
// `<段>.err` を stderr へ出し、`<段>.rc` の終了コードで終わり、`<段>.kill` があれば自身をシグナルで止めます。
func newFake(t *testing.T) *fake {
	t.Helper()
	dir := t.TempDir()
	body := `#!/bin/sh
ctl='` + dir + `'
step="$1"
echo "$PWD $step ${GIT_DIR:-}" >> "$ctl/calls.log"
if [ -e "$ctl/$step.kill" ]; then kill -9 $$; fi
if [ -e "$ctl/$step.out" ]; then cat "$ctl/$step.out"; fi
if [ -e "$ctl/$step.err" ]; then cat "$ctl/$step.err" >&2; fi
if [ -e "$ctl/$step.rc" ]; then exit "$(cat "$ctl/$step.rc")"; fi
exit 0
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tool"), []byte(body), 0o700))
	return &fake{t: t, dir: dir}
}

func (f *fake) set(step, suffix, body string) {
	f.t.Helper()
	require.NoError(f.t, os.WriteFile(filepath.Join(f.dir, step+"."+suffix), []byte(body), 0o600))
}

func (f *fake) tool() toolFunc {
	return func(d string, env []string, step string) *exec.Cmd {
		c := exec.CommandContext(context.Background(), filepath.Join(f.dir, "tool"), step)
		c.Dir, c.Env = d, env
		return c
	}
}

func (f *fake) calls() []string {
	f.t.Helper()
	raw, err := os.ReadFile(filepath.Join(f.dir, "calls.log"))
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(f.t, err)
	return strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
}

// missingTool は、PATH に無い道具を起動するコマンドを作ります。
func missingTool(d string, env []string, _ string) *exec.Cmd {
	c := exec.CommandContext(context.Background(), "/nonexistent/typos-gate-tool")
	c.Dir, c.Env = d, env
	return c
}

func writeFile(t *testing.T, p, body string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	return p
}

// realCwd は、テストのプロセスの作業ディレクトリを symlink を解いて返します（偽の道具が控える $PWD と比べるため）。
func realCwd(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	cwd, err = filepath.EvalSymlinks(cwd)
	require.NoError(t, err)
	return cwd
}
