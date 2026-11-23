package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// writeTree は、files（`/` 区切りの相対パスから中身）を一時ディレクトリに置き、そのパスを返す。
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	}

	return dir
}

func Test_exitCode(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		err  error
		want int
	}{
		"成功は 0":             {err: nil, want: 0},
		"構成ファイルが0件なら 2":     {err: xerrors.Wrap(errNoInput, "x"), want: 2},
		"root が無いのは違反と同じ 1": {err: xerrors.Wrap(errMissingRoot, "x"), want: 1},
		"件数の食い違いは 1":        {err: errFixtures, want: 1},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, exitCode(tt.err))
		})
	}
}

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("サブコマンドが無ければ errUsage", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, run(nil, &bytes.Buffer{}), errUsage)
	})

	t.Run("知らないサブコマンドは errUsage", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, run([]string{"count"}, &bytes.Buffer{}), errUsage)
	})

	t.Run("inputs へ渡す", func(t *testing.T) {
		t.Parallel()
		base := writeTree(t, map[string]string{"m/main.tf": ""})
		out := t.TempDir()
		require.NoError(t, run([]string{"inputs", "-base", base, "-out", out, "m"}, &bytes.Buffer{}))
		assert.FileExists(t, filepath.Join(out, "found.txt"))
	})

	t.Run("fixtures へ渡す", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, run([]string{"fixtures"}, &bytes.Buffer{}), errUsage)
	})
}
