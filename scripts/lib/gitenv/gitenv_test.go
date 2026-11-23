package gitenv

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnv(t *testing.T) {
	t.Parallel()

	t.Run("実物の git の一覧で GIT_DIR と GIT_INDEX_FILE を外し、ほかは残す", func(t *testing.T) {
		t.Parallel()
		got, err := Env([]string{"GIT_DIR=/x", "GIT_INDEX_FILE=/y", "GIT_WORK_TREE=/w", "GIT_OBJECT_DIRECTORY=/o", "GIT_AUTHOR_NAME=a", "PATH=" + os.Getenv("PATH")})
		require.NoError(t, err)
		for _, kv := range []string{"GIT_DIR=/x", "GIT_INDEX_FILE=/y", "GIT_WORK_TREE=/w", "GIT_OBJECT_DIRECTORY=/o"} {
			assert.NotContains(t, got, kv)
		}
		assert.Contains(t, got, "GIT_AUTHOR_NAME=a")
	})

	t.Run("外側のリポジトリを指す GIT_DIR を外し、外側を書き換えない", func(t *testing.T) {
		t.Parallel()
		outer := t.TempDir()
		run := func(dir string, env []string, args ...string) {
			t.Helper()
			cmd := exec.CommandContext(t.Context(), "git", args...)
			cmd.Dir, cmd.Env = dir, env
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, string(out))
		}
		base := withoutGitPrefix(os.Environ())
		run(outer, base, "init", "-q")
		leaked := append(append([]string{}, base...), "GIT_DIR="+filepath.Join(outer, ".git"), "GIT_INDEX_FILE="+filepath.Join(outer, ".git", "index"))
		env, err := Env(leaked)
		require.NoError(t, err)
		inner := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(inner, "f"), []byte("x\n"), 0o600))
		run(inner, env, "init", "-q")
		run(inner, env, "add", "-A")
		_, err = os.Stat(filepath.Join(outer, ".git", "index"))
		assert.ErrorIs(t, err, os.ErrNotExist, "外側の index が作られていない")
		_, err = os.Stat(filepath.Join(inner, ".git", "index"))
		assert.NoError(t, err)
	})
}

func Test_isolate(t *testing.T) {
	t.Parallel()

	t.Run("一覧の変数を外し、問い合わせには GIT_ を外した env を渡す", func(t *testing.T) {
		t.Parallel()
		var asked []string
		got, err := isolate([]string{"GIT_DIR=/x", "GIT_TRACE=1", "HOME=/h"}, func(env []string) ([]byte, error) {
			asked = env
			return []byte("GIT_DIR\nGIT_WORK_TREE\n"), nil
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"GIT_TRACE=1", "HOME=/h"}, got)
		assert.Equal(t, []string{"HOME=/h"}, asked)
	})

	t.Run("問い合わせが失敗すれば ErrNames で、写しを返さない", func(t *testing.T) {
		t.Parallel()
		got, err := isolate([]string{"GIT_DIR=/x"}, func([]string) ([]byte, error) { return nil, errors.New("boom") })
		require.ErrorIs(t, err, ErrNames)
		assert.Contains(t, err.Error(), "boom")
		assert.Nil(t, got)
	})

	t.Run("一覧が空なら ErrNames", func(t *testing.T) {
		t.Parallel()
		got, err := isolate([]string{"GIT_DIR=/x"}, func([]string) ([]byte, error) { return []byte(" \n"), nil })
		require.ErrorIs(t, err, ErrNames)
		assert.Nil(t, got)
	})
}

func Test_queryLocalEnvVars(t *testing.T) {
	t.Parallel()

	t.Run("壊れた GIT_DIR が立っていても、実物の git が一覧を返す", func(t *testing.T) {
		t.Parallel()
		out, err := queryLocalEnvVars(append(withoutGitPrefix(os.Environ()), "GIT_DIR=/dev/null/broken", "GIT_INDEX_FILE=/dev/null/broken"))
		require.NoError(t, err)
		assert.Contains(t, strings.Fields(string(out)), "GIT_INDEX_FILE")
	})

	t.Run("HOME の .gitconfig が壊れていれば、実物の git が失敗してエラーを返す", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("[broken\n"), 0o600))
		_, err := queryLocalEnvVars([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "XDG_CONFIG_HOME=" + home})
		require.Error(t, err)
	})

	t.Run("実物の git が GIT_DIR を含む一覧を返す", func(t *testing.T) {
		t.Parallel()
		out, err := queryLocalEnvVars(withoutGitPrefix(os.Environ()))
		require.NoError(t, err)
		assert.Contains(t, strings.Fields(string(out)), "GIT_DIR")
	})
}

func Test_withoutGitPrefix(t *testing.T) {
	t.Parallel()

	t.Run("GIT_ で始まる変数だけを除き、順序を保つ", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"B=2", "A=1"}, withoutGitPrefix([]string{"GIT_X=1", "B=2", "A=1", "GIT_Y="}))
	})

	t.Run("空なら空", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, withoutGitPrefix(nil))
	})
}

func Test_without(t *testing.T) {
	t.Parallel()

	t.Run("名前が完全に一致する変数だけを除く", func(t *testing.T) {
		t.Parallel()
		got := without([]string{"GIT_DIR=/x", "GIT_DIRX=1", "A=GIT_DIR=", "GIT_DIR"}, []string{"GIT_DIR"})
		assert.Equal(t, []string{"GIT_DIRX=1", "A=GIT_DIR="}, got)
	})
}
