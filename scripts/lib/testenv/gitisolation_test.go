package testenv

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

// installGitFakes は、gitEnvVars の出力と unsetenv を差し替え、外した名前を控えます。
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

// GIT_ISOLATE（.makefiles/git-isolation.mk）を持たないレシピは外側を書き換え、持つレシピは書き換えない。
func Test_GIT_ISOLATE_外側を守る(t *testing.T) {
	t.Parallel()

	names, err := gitEnvVars()
	require.NoError(t, err)
	clean := withoutVars(os.Environ(), names)

	mk, err := filepath.Abs(filepath.Join("..", "..", "..", ".makefiles", "git-isolation.mk"))
	require.NoError(t, err)

	for _, c := range []struct {
		name    string
		isolate string
		changed bool
	}{
		{"持たなければ外側が変わる", "", true},
		{"持てば外側は変わらない", "$(GIT_ISOLATE); ", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			outer := initRepo(t, clean)
			work := t.TempDir()
			makefile := filepath.Join(t.TempDir(), "Makefile")
			require.NoError(t, os.WriteFile(makefile, []byte(fmt.Sprintf(
				"include %s\nrecipe:\n\t@set -eu; %scd %q && git -c core.hooksPath=/dev/null init -q && echo x > f && git add f\n",
				mk, c.isolate, work)), 0o600))

			before := snapshot(t, outer)
			cmd := exec.CommandContext(t.Context(), "make", "-s", "-f", makefile, "recipe")
			cmd.Env = withoutMakeVars(leakInto(clean, outer))
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, string(out))

			if c.changed {
				assert.NotEqual(t, before, snapshot(t, outer))
			} else {
				assert.Equal(t, before, snapshot(t, outer))
				assert.DirExists(t, filepath.Join(work, ".git"))
			}
		})
	}
}

// tempRepoRecipe は、一時のリポジトリを建てるレシピの印（git init）です。
var tempRepoRecipe = regexp.MustCompile(`\binit -q\b`)

// 一時のリポジトリを建てる .makefiles のレシピは、先頭で $(GIT_ISOLATE) を実行する。
func Test_makefiles_一時のリポジトリを建てるレシピはGIT_ISOLATEを持つ(t *testing.T) {
	t.Parallel()

	recipes := makeRecipes(t, filepath.Join("..", "..", "..", ".makefiles"))
	var found []string
	for target, body := range recipes {
		i := tempRepoRecipe.FindStringIndex(body)
		if i == nil {
			continue
		}
		found = append(found, target)
		j := strings.Index(body, "$(GIT_ISOLATE)")
		assert.True(t, j >= 0 && j < i[0], "%s は一時のリポジトリを建てる前に $(GIT_ISOLATE) を実行していない", target)
	}

	// 見つけたレシピが0件なら、印か読み方が壊れている。
	assert.Subset(t, found, []string{
		"infra-code-gate-test", "gitleaks-canary", "trufflehog-canary",
		"editorconfig-check-canary", "editorconfig-directive-canary",
	})
}

// go-test と go-test-cover は、git の位置の変数が壊れた場所を指す環境で走る。
func Test_makefiles_go_testは壊した環境で走る(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..", "..")
	for _, target := range []string{"go-test", "go-test-cover"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()

			cmd := exec.CommandContext(t.Context(), "make", "-C", root, "-n", "--no-print-directory", target, "RUNNER_MODE=host", "GO_TOOL=")
			cmd.Env = withoutMakeVars(os.Environ())
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, string(out))

			var lines []string
			for _, line := range strings.Split(string(out), "\n") {
				if strings.Contains(line, "go -C scripts test") {
					lines = append(lines, line)
				}
			}
			require.Len(t, lines, 1, string(out))
			assert.Contains(t, lines[0], "GIT_DIR=/dev/null/git-isolation-required")
			assert.Contains(t, lines[0], "GIT_INDEX_FILE=/dev/null/git-isolation-required")
		})
	}
}

// makeRecipes は dir の下の .mk を読み、target ごとにレシピの行を連結して返します。
func makeRecipes(t *testing.T, dir string) map[string]string {
	t.Helper()

	rule := regexp.MustCompile(`^([A-Za-z0-9_.-]+):(?:[^=]|$)`)
	recipes := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".mk" {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()

		target := ""
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			if m := rule.FindStringSubmatch(line); m != nil {
				target = m[1]

				continue
			}
			if target != "" && strings.HasPrefix(line, "\t") {
				recipes[target] += line + "\n"

				continue
			}
			target = ""
		}

		return sc.Err()
	})
	require.NoError(t, err)
	require.NotEmpty(t, recipes)

	return recipes
}

func withoutVars(env, names []string) []string {
	return slices.DeleteFunc(slices.Clone(env), func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")

		return slices.Contains(names, name)
	})
}

func withoutMakeVars(env []string) []string {
	return withoutVars(env, []string{"MAKEFLAGS", "MFLAGS", "MAKELEVEL"})
}

func leakInto(env []string, outer string) []string {
	return append(slices.Clone(env), "GIT_DIR="+filepath.Join(outer, ".git"), "GIT_INDEX_FILE="+filepath.Join(outer, ".git", "index"))
}

func git(t *testing.T, dir string, env []string, args ...string) {
	t.Helper()

	// 自動メンテナンスは commit の後に detach して .git を書き、snapshot の比較と競合する。
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
