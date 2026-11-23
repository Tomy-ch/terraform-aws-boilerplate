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

// tree は files（`/` 区切りの相対パスから中身）を一時ディレクトリに置き、そのパスを返す。macOS の /var の symlink を
// 解いておく（作業ディレクトリの $PWD と比べるため）。
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	for rel, body := range files {
		p := filepath.Join(base, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	}
	return base
}

func readText(t *testing.T, p string) string {
	t.Helper()
	raw, err := os.ReadFile(p)
	require.NoError(t, err)
	return string(raw)
}

// fakeTool は、起動ごとに「作業ディレクトリ 段 TF_DATA_DIR」を calls.log へ書く偽の道具を作る。作業ディレクトリに
// `fail-<段>` があればその段は 1 で終わり、`<段>.out` があればその中身を stdout に出す。
func fakeTool(t *testing.T) (toolFunc, func() []string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	script := filepath.Join(dir, "tool")
	body := `#!/bin/sh
step="$1"
echo "$PWD $step ${TF_DATA_DIR:-}" >> '` + log + `'
if [ -e "$step.out" ]; then cat "$step.out"; fi
if [ -e "fail-$step" ]; then echo "$step が落ちた" >&2; exit 1; fi
exit 0
`
	require.NoError(t, os.WriteFile(script, []byte(body), 0o700))
	tool := func(d string, env []string, step string) *exec.Cmd {
		c := exec.CommandContext(context.Background(), script, step)
		c.Dir, c.Env = d, env
		return c
	}
	calls := func() []string {
		raw, err := os.ReadFile(log)
		if os.IsNotExist(err) {
			return nil
		}
		require.NoError(t, err)
		return strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	}
	return tool, calls
}
