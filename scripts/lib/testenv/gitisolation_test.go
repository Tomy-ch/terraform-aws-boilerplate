package testenv

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRunner struct {
	code  int
	calls int
}

func (f *fakeRunner) Run() int {
	f.calls++

	return f.code
}

var errBoom = errors.New("boom")

func installGitFakes(t *testing.T, out string, outErr error, failOn string) *[]string {
	t.Helper()

	oldList, oldUnset := localEnvVars, unsetenv
	t.Cleanup(func() { localEnvVars, unsetenv = oldList, oldUnset })

	var unset []string
	localEnvVars = func() ([]byte, error) { return []byte(out), outErr }
	unsetenv = func(name string) error {
		unset = append(unset, name)
		if name == failOn {
			return errBoom
		}

		return nil
	}

	return &unset
}

func TestRunGitIsolated(t *testing.T) { //nolint:paralleltest // プロセスの環境とパッケージ変数を差し替える
	t.Run("立っていた変数を外してから走らせる", func(t *testing.T) {
		names, err := gitEnvVars()
		require.NoError(t, err)
		for _, name := range names {
			t.Setenv(name, "/dev/null/git-isolation-required")
		}

		m := &fakeRunner{code: 7}
		require.Equal(t, 7, RunGitIsolated(m))
		assert.Equal(t, 1, m.calls)
		for _, name := range names {
			_, set := os.LookupEnv(name)
			assert.False(t, set, "%s が外れていない", name)
		}
	})

	t.Run("外せなければ走らせずに 2 を返す", func(t *testing.T) {
		installGitFakes(t, "", errBoom, "")
		old := stderr
		t.Cleanup(func() { stderr = old })
		var buf bytes.Buffer
		stderr = &buf

		m := &fakeRunner{code: 7}
		assert.Equal(t, 2, RunGitIsolated(m))
		assert.Zero(t, m.calls)
		assert.Contains(t, buf.String(), "git rev-parse --local-env-vars")
	})
}

func Test_isolateGitEnv(t *testing.T) { //nolint:paralleltest // パッケージ変数を差し替える
	t.Run("一覧の変数をすべて外す", func(t *testing.T) {
		unset := installGitFakes(t, "GIT_DIR\nGIT_INDEX_FILE\n", nil, "")
		require.NoError(t, isolateGitEnv())
		assert.Equal(t, []string{"GIT_DIR", "GIT_INDEX_FILE"}, *unset)
	})

	t.Run("一覧を得られなければ何も外さない", func(t *testing.T) {
		unset := installGitFakes(t, "", errBoom, "")
		require.ErrorIs(t, isolateGitEnv(), errGitEnvVars)
		assert.Empty(t, *unset)
	})

	// 後の成功が先の失敗を上書きしないこと。
	t.Run("途中で外せなければ、そこで止める", func(t *testing.T) {
		unset := installGitFakes(t, "GIT_DIR\nGIT_INDEX_FILE\n", nil, "GIT_DIR")
		err := isolateGitEnv()
		require.ErrorIs(t, err, errGitUnset)
		require.ErrorIs(t, err, errBoom)
		assert.Equal(t, []string{"GIT_DIR"}, *unset)
	})
}

func Test_gitEnvVars(t *testing.T) { //nolint:paralleltest // パッケージ変数を差し替える
	t.Run("git が返す一覧を読む", func(t *testing.T) {
		names, err := gitEnvVars()
		require.NoError(t, err)
		assert.Subset(t, names, []string{"GIT_DIR", "GIT_INDEX_FILE", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_CONFIG_PARAMETERS"})
	})

	t.Run("git が失敗したらエラー", func(t *testing.T) {
		installGitFakes(t, "", errBoom, "")
		_, err := gitEnvVars()
		require.ErrorIs(t, err, errGitEnvVars)
		require.ErrorIs(t, err, errBoom)
	})

	t.Run("一覧が空ならエラー", func(t *testing.T) {
		installGitFakes(t, " \n", nil, "")
		_, err := gitEnvVars()
		require.ErrorIs(t, err, errGitEnvVars)
	})
}

func Test_withoutGitEnv(t *testing.T) {
	t.Parallel()

	t.Run("GIT_ で始まる変数だけを除き、順序を保つ", func(t *testing.T) {
		t.Parallel()
		got := withoutGitEnv([]string{"PATH=/bin", "GIT_DIR=/x", "HOME=/h", "GIT_INTERNAL_SUPER_PREFIX=y", "MYGIT_X=1", "git_dir=z"})
		assert.Equal(t, []string{"PATH=/bin", "HOME=/h", "MYGIT_X=1", "git_dir=z"}, got)
	})

	t.Run("空なら空", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, withoutGitEnv(nil))
	})
}

// hook が渡す GIT_DIR と GIT_INDEX_FILE を継いだまま一時リポジトリへ git -C を打つと、外側の
// リポジトリが書き換わる。gitEnvVars の一覧を外すと、外側は変わらない。
func Test_外側のリポジトリ_事故の再現と防御(t *testing.T) {
	t.Parallel()

	names, err := gitEnvVars()
	require.NoError(t, err)
	clean := withoutVars(os.Environ(), names)

	t.Run("外さなければ外側が変わる", func(t *testing.T) {
		t.Parallel()

		outer := initRepo(t, clean)
		before := snapshot(t, outer)
		commitIn(t, t.TempDir(), leakInto(clean, outer))

		assert.NotEqual(t, before, snapshot(t, outer))
	})

	t.Run("外せば外側は変わらない", func(t *testing.T) {
		t.Parallel()

		outer := initRepo(t, clean)
		before := snapshot(t, outer)
		env := withoutVars(leakInto(clean, outer), names)
		inner := t.TempDir()
		git(t, inner, env, "init", "-q")
		commitIn(t, inner, env)

		assert.Equal(t, before, snapshot(t, outer))
	})
}

func withoutVars(env, names []string) []string {
	return slices.DeleteFunc(slices.Clone(env), func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")

		return slices.Contains(names, name)
	})
}

func leakInto(env []string, outer string) []string {
	return append(slices.Clone(env), "GIT_DIR="+filepath.Join(outer, ".git"), "GIT_INDEX_FILE="+filepath.Join(outer, ".git", "index"))
}

func git(t *testing.T, dir string, env []string, args ...string) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", append([]string{
		"-C", dir, "-c", "core.hooksPath=/dev/null", "-c", "maintenance.auto=false", "-c", "gc.auto=0",
	}, args...)...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

func initRepo(t *testing.T, env []string) string {
	t.Helper()

	dir := t.TempDir()
	git(t, dir, env, "init", "-q")

	return dir
}

func commitIn(t *testing.T, dir string, env []string) {
	t.Helper()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o600))
	git(t, dir, env, "add", "f")
	git(t, dir, env, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "x")
}

func snapshot(t *testing.T, repo string) string {
	t.Helper()

	var b strings.Builder
	err := filepath.WalkDir(filepath.Join(repo, ".git"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(&b, "%s %x\n", path, sha256.Sum256(body))

		return nil
	})
	require.NoError(t, err)

	return b.String()
}
