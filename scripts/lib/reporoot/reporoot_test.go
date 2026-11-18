package reporoot

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errFake = xerrors.New("fake")

func fixed(path string) func() (string, error) {
	return func() (string, error) { return path, nil }
}

func failing() (string, error) { return "", errFake }

func git(t *testing.T, args ...string) {
	t.Helper()

	out, err := exec.CommandContext(t.Context(), "git", args...).CombinedOutput()
	require.NoError(t, err, string(out))
}

func initRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	git(t, "init", "-q", dir)

	return dir
}

// fakeGit は、PATH を script だけを置いた git の偽物に差し替える。
func fakeGit(t *testing.T, script string) {
	t.Helper()

	bin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\n"+script), 0o700)) //nolint:gosec // 偽物の道具は実行できなければならない
	t.Setenv("PATH", bin)
}

func realPath(t *testing.T, path string) string {
	t.Helper()

	got, err := filepath.EvalSymlinks(path)
	require.NoError(t, err)

	return got
}

func TestRequire(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("作業ディレクトリが直下なら成功する", func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()

			require.NoError(t, Require(fixed(dir), fixed(dir)))
		})

		// macOS の一時ディレクトリは /var → /private/var の symlink を挟み、git はたどった先を返す。
		t.Run("symlink を挟んだ同じディレクトリは直下と認める", func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			link := filepath.Join(t.TempDir(), "link")
			require.NoError(t, os.Symlink(dir, link))

			require.NoError(t, Require(fixed(link), fixed(dir)))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("直下の下のディレクトリでは止まる", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			sub := filepath.Join(root, "scripts")
			require.NoError(t, os.Mkdir(sub, 0o750))

			err := Require(fixed(sub), fixed(root))
			require.ErrorIs(t, err, ErrNotRoot)
			assert.Contains(t, err.Error(), sub)
			assert.Contains(t, err.Error(), root)
		})

		t.Run("直下を解決できなければそのエラーを返す", func(t *testing.T) {
			t.Parallel()

			require.ErrorIs(t, Require(fixed(t.TempDir()), failing), errFake)
		})

		t.Run("作業ディレクトリを取れなければエラーにする", func(t *testing.T) {
			t.Parallel()

			require.ErrorIs(t, Require(failing, fixed(t.TempDir())), errFake)
		})

		t.Run("作業ディレクトリが無ければ別のディレクトリとは扱わずに stat の失敗を返す", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			missing := filepath.Join(root, "missing")

			err := Require(fixed(missing), fixed(root))
			require.ErrorIs(t, err, os.ErrNotExist)
			require.NotErrorIs(t, err, ErrNotRoot)
			assert.Contains(t, err.Error(), missing)
		})

		t.Run("直下が無ければ別のディレクトリとは扱わずに stat の失敗を返す", func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			missing := filepath.Join(dir, "missing")

			err := Require(fixed(dir), fixed(missing))
			require.ErrorIs(t, err, os.ErrNotExist)
			require.NotErrorIs(t, err, ErrNotRoot)
			assert.Contains(t, err.Error(), missing)
		})
	})
}

//nolint:paralleltest // t.Chdir と t.Setenv はプロセス全体を変えるため並列化できない
func TestToplevel(t *testing.T) {
	t.Run("正常系", func(t *testing.T) {
		t.Run("直下の下からでも直下を返す", func(t *testing.T) {
			root := initRepo(t)
			sub := filepath.Join(root, "scripts")
			require.NoError(t, os.Mkdir(sub, 0o750))
			t.Chdir(sub)

			got, err := Toplevel()
			require.NoError(t, err)
			assert.Equal(t, realPath(t, root), realPath(t, got))
		})

		// worktree と submodule の直下では .git がファイルになる。
		t.Run(".git がファイルの直下も直下として返す", func(t *testing.T) {
			root := initRepo(t)
			git(t, "-C", root, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false",
				"commit", "-q", "--allow-empty", "-m", "init")
			wt := filepath.Join(t.TempDir(), "wt")
			git(t, "-C", root, "worktree", "add", "-q", wt)
			t.Chdir(wt)

			got, err := Toplevel()
			require.NoError(t, err)
			assert.Equal(t, realPath(t, wt), realPath(t, got))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Run("git の外では ErrNoToplevel を返す", func(t *testing.T) {
			t.Chdir(t.TempDir())

			_, err := Toplevel()
			require.ErrorIs(t, err, ErrNoToplevel)
		})

		t.Run("作業ツリーを持たない .git の中では ErrNoToplevel を返す", func(t *testing.T) {
			t.Chdir(filepath.Join(initRepo(t), ".git"))

			_, err := Toplevel()
			require.ErrorIs(t, err, ErrNoToplevel)
		})

		t.Run("bare リポジトリでは ErrNoToplevel を返す", func(t *testing.T) {
			dir := t.TempDir()
			git(t, "init", "-q", "--bare", dir)
			t.Chdir(dir)

			_, err := Toplevel()
			require.ErrorIs(t, err, ErrNoToplevel)
		})

		// core.worktree が直下の下を指すと、git はそこを直下として答え、本来の直下の下からの起動が通る。
		t.Run("core.worktree で付け替えた作業ツリーは .git が無いので ErrNoToplevel を返す", func(t *testing.T) {
			root := initRepo(t)
			sub := filepath.Join(root, "scripts")
			require.NoError(t, os.Mkdir(sub, 0o750))
			git(t, "-C", root, "config", "core.worktree", sub)
			t.Chdir(sub)

			_, err := Toplevel()
			require.ErrorIs(t, err, ErrNoToplevel)
			assert.Contains(t, err.Error(), ".git がありません")
		})

		t.Run("解決できない理由として git の stderr を添える", func(t *testing.T) {
			t.Chdir(t.TempDir())
			t.Setenv("LC_ALL", "C")

			_, err := Toplevel()
			require.ErrorIs(t, err, ErrNoToplevel)
			assert.Contains(t, err.Error(), "not a git repository")
		})

		t.Run("stderr の無い失敗でも ErrNoToplevel を返す", func(t *testing.T) {
			fakeGit(t, "exit 1\n")

			_, err := Toplevel()
			require.ErrorIs(t, err, ErrNoToplevel)
			assert.Contains(t, err.Error(), "exit status 1")
		})

		t.Run("git を起動できなければ ErrNoToplevel を返す", func(t *testing.T) {
			t.Setenv("PATH", t.TempDir())

			_, err := Toplevel()
			require.ErrorIs(t, err, ErrNoToplevel)
		})

		t.Run("git が空を返せば ErrNoToplevel を返す", func(t *testing.T) {
			fakeGit(t, "exit 0\n")

			_, err := Toplevel()
			require.ErrorIs(t, err, ErrNoToplevel)
		})

		for _, name := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE"} {
			for _, empty := range []bool{false, true} {
				label := "パス"
				if empty {
					label = "空文字"
				}
				t.Run(name+" が"+label+"で立っていれば git に訊かずに止まる", func(t *testing.T) {
					root := initRepo(t)
					sub := filepath.Join(root, "scripts")
					require.NoError(t, os.Mkdir(sub, 0o750))
					t.Chdir(sub)
					value := filepath.Join(root, ".git")
					if empty {
						value = ""
					}
					t.Setenv(name, value)

					_, err := Toplevel()
					require.ErrorIs(t, err, ErrRepoEnv)
					assert.Contains(t, err.Error(), name)
				})
			}
		}
	})
}
