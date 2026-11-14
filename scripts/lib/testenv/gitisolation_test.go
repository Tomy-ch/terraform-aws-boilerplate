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

func TestRunGitIsolated(t *testing.T) {
	t.Parallel()

	m := &fakeRunner{code: 7}
	require.Equal(t, 7, RunGitIsolated(m))
	assert.Equal(t, 1, m.calls)

	names, err := gitEnvVars()
	require.NoError(t, err)
	for _, name := range names {
		_, set := os.LookupEnv(name)
		assert.False(t, set, "%s が外れていない", name)
	}
}

func Test_runGitIsolated(t *testing.T) {
	t.Parallel()

	errBoom := errors.New("boom")

	t.Run("一覧の変数をすべて外してから走らせる", func(t *testing.T) {
		t.Parallel()

		var unset []string
		m := &fakeRunner{code: 3}
		var stderr bytes.Buffer
		code := runGitIsolated(m, func() ([]string, error) { return []string{"GIT_DIR", "GIT_INDEX_FILE"}, nil },
			func(name string) error { unset = append(unset, name); return nil }, &stderr)

		assert.Equal(t, 3, code)
		assert.Equal(t, 1, m.calls)
		assert.Equal(t, []string{"GIT_DIR", "GIT_INDEX_FILE"}, unset)
		assert.Empty(t, stderr.String())
	})

	for _, c := range []struct {
		name  string
		list  func() ([]string, error)
		unset func(string) error
	}{
		{"一覧を得られなければ走らせない", func() ([]string, error) { return nil, errBoom }, func(string) error { return nil }},
		{"一覧が空なら走らせない", func() ([]string, error) { return nil, nil }, func(string) error { return nil }},
		{"外せなければ走らせない", func() ([]string, error) { return []string{"GIT_DIR"}, nil }, func(string) error { return errBoom }},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			m := &fakeRunner{}
			var stderr bytes.Buffer
			assert.Equal(t, 2, runGitIsolated(m, c.list, c.unset, &stderr))
			assert.Zero(t, m.calls)
			assert.Contains(t, stderr.String(), "git の環境変数を外せません")
		})
	}
}

func Test_gitEnvVars(t *testing.T) {
	t.Parallel()

	names, err := gitEnvVars()
	require.NoError(t, err)
	assert.Subset(t, names, []string{"GIT_DIR", "GIT_INDEX_FILE", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_CONFIG_PARAMETERS"})
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
		leaked := append(slices.Clone(clean), "GIT_DIR="+filepath.Join(outer, ".git"), "GIT_INDEX_FILE="+filepath.Join(outer, ".git", "index"))
		commitIn(t, t.TempDir(), leaked)

		assert.NotEqual(t, before, snapshot(t, outer))
	})

	t.Run("外せば外側は変わらない", func(t *testing.T) {
		t.Parallel()

		outer := initRepo(t, clean)
		before := snapshot(t, outer)
		leaked := append(slices.Clone(clean), "GIT_DIR="+filepath.Join(outer, ".git"), "GIT_INDEX_FILE="+filepath.Join(outer, ".git", "index"))
		inner := t.TempDir()
		git(t, inner, withoutVars(leaked, names), "init", "-q")
		commitIn(t, inner, withoutVars(leaked, names))

		assert.Equal(t, before, snapshot(t, outer))
	})
}

func withoutVars(env, names []string) []string {
	return slices.DeleteFunc(slices.Clone(env), func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")

		return slices.Contains(names, name)
	})
}

func git(t *testing.T, dir string, env []string, args ...string) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir, "-c", "core.hooksPath=/dev/null"}, args...)...)
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
