package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/testenv"
)

func Test_prepareWork(t *testing.T) {
	t.Parallel()

	t.Run("tmp/secret-scan/<name> を作り直す", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		stale := filepath.Join(root, "tmp", "secret-scan", "x", "stale")
		require.NoError(t, os.MkdirAll(filepath.Dir(stale), 0o750))
		require.NoError(t, os.WriteFile(stale, nil, 0o600))
		dir, _, err := prepareWork(root, "x", io.Discard)
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(root, "tmp", "secret-scan", "x"), dir)
		assert.DirExists(t, dir)
		assert.NoFileExists(t, stale)
	})

	t.Run("後片付けは tmp/secret-scan/<name> だけを消し、消せなければ log へ警告する", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		var log strings.Builder
		dir, cleanup, err := prepareWork(root, "x", &log)
		require.NoError(t, err)
		keep := filepath.Join(root, "tmp", "secret-scan", "y")
		require.NoError(t, os.MkdirAll(keep, 0o750))
		cleanup()
		assert.NoDirExists(t, dir)
		assert.DirExists(t, keep)
		assert.Empty(t, log.String())

		outside := t.TempDir()
		require.NoError(t, os.Remove(filepath.Join(root, "tmp", "secret-scan", "y")))
		require.NoError(t, os.Symlink(outside, filepath.Join(root, "tmp", "secret-scan", "x")))
		cleanup()
		assert.Contains(t, log.String(), "⚠️ 作業ディレクトリ")
		assert.DirExists(t, outside)
	})

	t.Run("tmp/ の外を指せば errUsage で、外を消さない", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		outside := t.TempDir()
		keep := filepath.Join(outside, "keep")
		require.NoError(t, os.WriteFile(keep, nil, 0o600))
		require.NoError(t, os.Mkdir(filepath.Join(root, "tmp"), 0o750))
		require.NoError(t, os.Symlink(outside, filepath.Join(root, "tmp", "secret-scan")))
		_, _, err := prepareWork(root, "x", io.Discard)
		require.ErrorIs(t, err, errUsage)
		assert.FileExists(t, keep)
		_, _, err = prepareWork(root, "../../x", io.Discard)
		require.ErrorIs(t, err, errUsage)
	})

	t.Run("作れなければ errInput", func(t *testing.T) {
		t.Parallel()
		testenv.RequireNonRoot(t, "読み取り専用のディレクトリへ書けないことを作る")
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "tmp"), 0o750))
		require.NoError(t, os.Chmod(filepath.Join(root, "tmp"), 0o500))
		t.Cleanup(func() { _ = os.Chmod(filepath.Join(root, "tmp"), 0o700) })
		_, _, err := prepareWork(root, "x", io.Discard)
		require.ErrorIs(t, err, errInput)
	})
}

func Test_readFile(t *testing.T) {
	t.Parallel()

	p := filepath.Join(t.TempDir(), "f")
	require.NoError(t, os.WriteFile(p, []byte("body"), 0o600))
	got, err := readFile(p)
	require.NoError(t, err)
	assert.Equal(t, "body", got)

	_, err = readFile(filepath.Join(t.TempDir(), "absent"))
	require.ErrorIs(t, err, errInput)
}

func Test_writeFile(t *testing.T) {
	t.Parallel()

	p := filepath.Join(t.TempDir(), "a", "b", "f")
	require.NoError(t, writeFile(p, "body"))
	got, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, "body", string(got))

	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	require.ErrorIs(t, writeFile(filepath.Join(file, "f"), ""), errInput)
}

func Test_copyInto(t *testing.T) {
	t.Parallel()

	root, dir := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, ".gitleaks.toml"), []byte("cfg"), 0o600))
	require.NoError(t, copyInto(root, dir, ".gitleaks.toml"))
	got, err := os.ReadFile(filepath.Join(dir, ".gitleaks.toml"))
	require.NoError(t, err)
	assert.Equal(t, "cfg", string(got))

	require.ErrorIs(t, copyInto(root, dir, ".gitleaksignore"), errInput, "複写元が無い")
}
