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
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeReporter は判断が呼んだ経路を控えます。実際の *testing.T は Skip も Fatalf も
// runtime.Goexit するため、分岐の到達を観測できません。
type fakeReporter struct {
	skipped bool
	failed  bool
	message string
}

func (f *fakeReporter) Helper() {}

func (f *fakeReporter) Skip(args ...any) {
	f.skipped = true
	if len(args) > 0 {
		f.message, _ = args[0].(string)
	}
}

func (f *fakeReporter) Fatalf(format string, args ...any) {
	f.failed = true
	f.message = fmt.Sprintf(format, args...)
}

func found(string) (string, error)   { return "/usr/bin/shellcheck", nil }
func missing(string) (string, error) { return "", os.ErrNotExist }

func swap(t *testing.T, uid int, env string, look func(string) (string, error)) *fakeReporter {
	t.Helper()

	oldEuid, oldEnv, oldLook := geteuid, getenv, lookPath
	geteuid = func() int { return uid }
	getenv = func(string) string { return env }
	lookPath = look
	t.Cleanup(func() { geteuid, getenv, lookPath = oldEuid, oldEnv, oldLook })

	return &fakeReporter{}
}

// 既定値が本物を指していることを、実際の syscall と突き合わせて見ます。差し替え可能に
// したことで、既定値を書き間違えても全部緑になる余地が生まれるため。
func Test_外界の既定値(t *testing.T) { //nolint:paralleltest // パッケージ変数を読む
	assert.Equal(t, os.Geteuid(), geteuid(), "geteuid が os.Geteuid を指していない")
	assert.Equal(t, os.Getenv("PATH"), getenv("PATH"), "getenv が os.Getenv を指していない")

	want, wantErr := exec.LookPath("go")
	got, gotErr := lookPath("go")
	assert.Equal(t, want, got, "lookPath が exec.LookPath を指していない")
	assert.Equal(t, wantErr == nil, gotErr == nil)

	assert.Equal(t, reflect.ValueOf(gitLocalEnvVars).Pointer(), reflect.ValueOf(localEnvVars).Pointer(),
		"localEnvVars が gitLocalEnvVars を指していない")
	assert.Equal(t, reflect.ValueOf(os.Unsetenv).Pointer(), reflect.ValueOf(unsetenv).Pointer(),
		"unsetenv が os.Unsetenv を指していない")
	assert.Same(t, os.Stderr, stderr, "stderr が os.Stderr を指していない")
}

func Test_requireNonRoot(t *testing.T) { //nolint:paralleltest // パッケージ変数を差し替える
	t.Run("正常系", func(t *testing.T) {
		t.Run("root でなければ skip も失敗もしない", func(t *testing.T) {
			f := swap(t, 1000, "", nil)

			requireNonRoot(f, "理由")

			assert.False(t, f.skipped)
			assert.False(t, f.failed)
		})

		// 環境変数が立っているだけで落ちるなら、非 root の CI がこの経路を通った瞬間に
		// 全部赤くなる。
		t.Run("root でなければ環境変数が立っていても失敗しない", func(t *testing.T) {
			f := swap(t, 1000, "1", nil)

			requireNonRoot(f, "理由")

			assert.False(t, f.skipped)
			assert.False(t, f.failed)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Run("root なら理由を添えて skip する", func(t *testing.T) {
			f := swap(t, 0, "", nil)

			requireNonRoot(f, "権限を落としても効かない")

			assert.True(t, f.skipped)
			assert.False(t, f.failed)
			assert.Equal(t, "権限を落としても効かない", f.message)
		})

		// これがこのパッケージの存在理由。skip は既定の出力に現れないので、CI では
		// 失敗へ変えられなければ「報告より少ない検査で緑」が残り続ける。
		t.Run("root かつ環境変数が立っていれば skip せず失敗する", func(t *testing.T) {
			f := swap(t, 0, "1", nil)

			requireNonRoot(f, "権限を落としても効かない")

			assert.True(t, f.failed)
			assert.False(t, f.skipped, "失敗させるべき場面で skip した")
			assert.Contains(t, f.message, RequireNonRootEnv)
		})
	})
}

func Test_requireShellcheck(t *testing.T) { //nolint:paralleltest // パッケージ変数を差し替える
	t.Run("正常系", func(t *testing.T) {
		t.Run("PATH に在れば skip も失敗もしない", func(t *testing.T) {
			f := swap(t, 1000, "", found)

			requireShellcheck(f)

			assert.False(t, f.skipped)
			assert.False(t, f.failed)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Run("PATH に無ければ skip する", func(t *testing.T) {
			f := swap(t, 1000, "", missing)

			requireShellcheck(f)

			assert.True(t, f.skipped)
			assert.False(t, f.failed)
		})

		t.Run("PATH に無く環境変数が立っていれば skip せず失敗する", func(t *testing.T) {
			f := swap(t, 1000, "1", missing)

			requireShellcheck(f)

			assert.True(t, f.failed)
			assert.False(t, f.skipped, "失敗させるべき場面で skip した")
			assert.Contains(t, f.message, RequireShellcheckEnv)
		})
	})
}

func swapIsolation(t *testing.T, names []string, listErr, unsetErr error) *[]string {
	t.Helper()

	var unset []string
	oldList, oldUnset := localEnvVars, unsetenv
	localEnvVars = func() ([]string, error) { return names, listErr }
	unsetenv = func(name string) error {
		unset = append(unset, name)

		return unsetErr
	}
	t.Cleanup(func() { localEnvVars, unsetenv = oldList, oldUnset })

	return &unset
}

func Test_isolateGit(t *testing.T) { //nolint:paralleltest // パッケージ変数を差し替える
	t.Run("正常系", func(t *testing.T) {
		t.Run("git が返した名前をすべて外す", func(t *testing.T) {
			unset := swapIsolation(t, []string{"GIT_DIR", "GIT_INDEX_FILE"}, nil, nil)

			require.NoError(t, isolateGit())

			assert.Equal(t, []string{"GIT_DIR", "GIT_INDEX_FILE"}, *unset)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Run("一覧を得られなければ何も外さずエラーを返す", func(t *testing.T) {
			want := errors.New("git が無い")
			unset := swapIsolation(t, nil, want, nil)

			require.ErrorIs(t, isolateGit(), want)
			assert.Empty(t, *unset)
		})

		// 0件を「外すものが無い」と読むと、防御が働かないまま緑になる。
		t.Run("一覧が0件ならエラーを返す", func(t *testing.T) {
			swapIsolation(t, []string{}, nil, nil)

			require.ErrorIs(t, isolateGit(), ErrNoLocalEnvVars)
		})

		t.Run("外せなければ名前を添えてエラーを返す", func(t *testing.T) {
			want := errors.New("unsetenv 失敗")
			swapIsolation(t, []string{"GIT_DIR"}, nil, want)

			err := isolateGit()

			require.ErrorIs(t, err, want)
			assert.Contains(t, err.Error(), "GIT_DIR")
		})
	})
}

func Test_gitLocalEnvVars(t *testing.T) { //nolint:paralleltest // t.Setenv を使う
	t.Run("正常系", func(t *testing.T) {
		t.Run("事故に効いた変数を含む", func(t *testing.T) {
			names, err := gitLocalEnvVars()

			require.NoError(t, err)
			assert.Contains(t, names, "GIT_DIR")
			assert.Contains(t, names, "GIT_INDEX_FILE")
		})

		// make go-test は GIT_DIR を存在しえない場所へ向けて走る。そこで答えなければ、
		// 防御そのものが毒で落ちる。
		t.Run("GIT_DIR が存在しない場所を指していても答える", func(t *testing.T) {
			t.Setenv("GIT_DIR", "/dev/null/git-isolation-required")

			names, err := gitLocalEnvVars()

			require.NoError(t, err)
			assert.Contains(t, names, "GIT_DIR")
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Run("git が PATH に無ければエラーを返す", func(t *testing.T) {
			t.Setenv("PATH", t.TempDir())

			_, err := gitLocalEnvVars()

			require.Error(t, err)
		})
	})
}

// gitRun は、いまのプロセスの環境変数のまま git を起動します。
func gitRun(t *testing.T, args ...string) {
	t.Helper()

	out, err := exec.CommandContext(t.Context(), "git", args...).CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}

func snapshot(t *testing.T, dir string) map[string][32]byte {
	t.Helper()

	got := map[string][32]byte{}
	require.NoError(t, filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		got[rel] = sha256.Sum256(body)

		return nil
	}))

	return got
}

// hook が渡す GIT_DIR / GIT_INDEX_FILE を外側のリポジトリへ向けたまま、一時リポジトリを建てる
// テストと同じ操作を走らせる。防御なしで外側が変わることを先に確かめるので、git の挙動が変わって
// 事故が起きなくなれば、このテストが何も見ていないことが分かる。
func Test_isolateGit_外側のリポジトリ(t *testing.T) { //nolint:paralleltest // t.Setenv を使う
	ops := func(t *testing.T) string {
		t.Helper()

		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "evil.tf"), []byte("x\n"), 0o600))
		gitRun(t, "-C", dir, "init", "-q")
		gitRun(t, "-C", dir, "config", "user.name", "repo-setup-test")
		gitRun(t, "-C", dir, "add", "-A")
		gitRun(t, "-C", dir, "-c", "user.email=t@example.com", "-c", "core.hooksPath=/dev/null",
			"-c", "commit.gpgsign=false", "commit", "-q", "-m", "fixture")
		gitRun(t, "-C", dir, "branch", "develop")
		gitRun(t, "-C", dir, "tag", "v0.0.0")

		return dir
	}
	outer := func(t *testing.T) string {
		t.Helper()

		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "README"), []byte("outer\n"), 0o600))
		gitRun(t, "-C", dir, "init", "-q")
		gitRun(t, "-C", dir, "add", "README")
		gitRun(t, "-C", dir, "-c", "user.name=outer", "-c", "user.email=outer@example.com",
			"-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "outer")
		t.Setenv("GIT_DIR", filepath.Join(dir, ".git"))
		t.Setenv("GIT_INDEX_FILE", filepath.Join(dir, ".git", "index"))

		return dir
	}

	t.Run("防御なしでは外側が書き換わる", func(t *testing.T) {
		o := outer(t)
		before := snapshot(t, filepath.Join(o, ".git"))

		ops(t)

		assert.NotEqual(t, before, snapshot(t, filepath.Join(o, ".git")))
	})

	t.Run("防御ありでは外側が変わらず、一時リポジトリへ書く", func(t *testing.T) {
		o := outer(t)
		before := snapshot(t, filepath.Join(o, ".git"))

		require.NoError(t, isolateGit())
		dir := ops(t)

		assert.Equal(t, before, snapshot(t, filepath.Join(o, ".git")), "外側のリポジトリが書き換わった")
		config, err := os.ReadFile(filepath.Join(dir, ".git", "config"))
		require.NoError(t, err)
		assert.True(t, bytes.Contains(config, []byte("repo-setup-test")), "一時リポジトリへ書いていない")
		for _, ref := range []string{"refs/heads/develop", "refs/tags/v0.0.0"} {
			_, err := os.Stat(filepath.Join(dir, ".git", ref))
			assert.NoError(t, err, "%s が一時リポジトリに無い", ref)
		}
	})
}
