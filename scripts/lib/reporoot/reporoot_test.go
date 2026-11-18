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

func initRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	out, err := exec.CommandContext(t.Context(), "git", "init", "-q", dir).CombinedOutput()
	require.NoError(t, err, string(out))

	return dir
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
			require.ErrorIs(t, err, errNotRoot)
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

		t.Run("作業ディレクトリが無ければエラーにする", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()

			require.Error(t, Require(fixed(filepath.Join(root, "missing")), fixed(root)))
		})

		t.Run("直下が無ければエラーにする", func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()

			require.Error(t, Require(fixed(dir), fixed(filepath.Join(dir, "missing"))))
		})
	})
}

//nolint:paralleltest // t.Chdir はプロセス全体の作業ディレクトリを変えるため並列化できない
func TestToplevel(t *testing.T) {
	t.Run("正常系", func(t *testing.T) {
		t.Run("直下の下からでも直下を返す", func(t *testing.T) {
			root := initRepo(t)
			sub := filepath.Join(root, "scripts")
			require.NoError(t, os.Mkdir(sub, 0o750))
			t.Chdir(sub)

			got, err := Toplevel()
			require.NoError(t, err)
			require.NoError(t, Require(fixed(got), fixed(root)))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Run("git の外では errNoToplevel を返す", func(t *testing.T) {
			t.Chdir(t.TempDir())

			_, err := Toplevel()
			require.ErrorIs(t, err, errNoToplevel)
		})

		t.Run("作業ツリーを持たない .git の中では errNoToplevel を返す", func(t *testing.T) {
			t.Chdir(filepath.Join(initRepo(t), ".git"))

			_, err := Toplevel()
			require.ErrorIs(t, err, errNoToplevel)
		})
	})
}
