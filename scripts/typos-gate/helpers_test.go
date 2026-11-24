package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, p, body string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	return p
}

// realCwd は、テストのプロセスの作業ディレクトリを symlink を解いて返します（偽の道具が控える `pwd -P` と比べるため）。
func realCwd(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	cwd, err = filepath.EvalSymlinks(cwd)
	require.NoError(t, err)
	return cwd
}
