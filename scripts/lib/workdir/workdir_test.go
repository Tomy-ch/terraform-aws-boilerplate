package workdir

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolve(t *testing.T) {
	t.Parallel()

	for _, work := range []string{"tmp/x", "./tmp/x", "tmp/x/y", "tmp/x/"} {
		t.Run(work+" は root の下へ解決する", func(t *testing.T) {
			t.Parallel()
			got, err := Resolve("/r", work)
			require.NoError(t, err)
			want := "/r/tmp/x"
			if strings.Contains(work, "x/y") {
				want = "/r/tmp/x/y"
			}
			assert.Equal(t, want, got)
		})
	}

	for _, work := range []string{"", "/tmp/x", "tmp", "tmp/", "tmp/.", "./tmp", "tmp/../x", "tmp/x/../../y", "tmp/x/..", "x/tmp/y", "tmpx/y", "../tmp/x", "tmp/a/../b", "tmp/link/../x"} {
		t.Run(work+" は ErrInvalid", func(t *testing.T) {
			t.Parallel()
			_, err := Resolve("/r", work)
			require.ErrorIs(t, err, ErrInvalid)
		})
	}

	// root と外を、symlink を解いた実パスで建てる。
	realDir := func(t *testing.T) string {
		t.Helper()
		d, err := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)
		return d
	}

	t.Run("tmp/ の下の symlink が外を指せば、その下も自身も ErrInvalid で、外は消さない", func(t *testing.T) {
		t.Parallel()
		root, outside := realDir(t), realDir(t)
		require.NoError(t, os.MkdirAll(filepath.Join(root, "tmp"), 0o750))
		require.NoError(t, os.Symlink(outside, filepath.Join(root, "tmp", "link")))
		for _, work := range []string{"tmp/link/x", "tmp/link", "tmp/link/x/y"} {
			_, err := Resolve(root, work)
			require.ErrorIs(t, err, ErrInvalid, work)
			assert.Contains(t, err.Error(), fmt.Sprintf("%q: ", work), "work の値を名指す")
			assert.Contains(t, err.Error(), " の下にありません", "原因を添える")
		}
	})

	t.Run("tmp/ の下の symlink が tmp/ の下を指せば通る", func(t *testing.T) {
		t.Parallel()
		root := realDir(t)
		require.NoError(t, os.MkdirAll(filepath.Join(root, "tmp", "real"), 0o750))
		require.NoError(t, os.Symlink(filepath.Join(root, "tmp", "real"), filepath.Join(root, "tmp", "link")))
		got, err := Resolve(root, "tmp/link/x")
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(root, "tmp", "link", "x"), got)
	})

	t.Run("在る tmp/ の下の、まだ無い -work は通る", func(t *testing.T) {
		t.Parallel()
		root := realDir(t)
		require.NoError(t, os.MkdirAll(filepath.Join(root, "tmp"), 0o750))
		got, err := Resolve(root, "tmp/a/b")
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(root, "tmp", "a", "b"), got)
	})

	t.Run("tmp が symlink なら、その先を tmp/ として扱う", func(t *testing.T) {
		t.Parallel()
		root, elsewhere := realDir(t), realDir(t)
		require.NoError(t, os.Symlink(elsewhere, filepath.Join(root, "tmp")))
		_, err := Resolve(root, "tmp/x")
		require.NoError(t, err)
	})

	t.Run("tmp が壊れた symlink なら ErrInvalid", func(t *testing.T) {
		t.Parallel()
		root := realDir(t)
		require.NoError(t, os.Symlink(filepath.Join(root, "none"), filepath.Join(root, "tmp")))
		_, err := Resolve(root, "tmp/x")
		require.ErrorIs(t, err, ErrInvalid)
	})

	t.Run("tmp の親（リポジトリ直下）を指す symlink の下も自身も ErrInvalid", func(t *testing.T) {
		t.Parallel()
		root := realDir(t)
		require.NoError(t, os.MkdirAll(filepath.Join(root, "tmp"), 0o750))
		require.NoError(t, os.Symlink(root, filepath.Join(root, "tmp", "up")))
		for _, work := range []string{"tmp/up/x", "tmp/up"} {
			_, err := Resolve(root, work)
			require.ErrorIs(t, err, ErrInvalid, work)
		}
	})

	t.Run("既に在る tmp/x は通る", func(t *testing.T) {
		t.Parallel()
		root := realDir(t)
		require.NoError(t, os.MkdirAll(filepath.Join(root, "tmp", "x"), 0o750))
		got, err := Resolve(root, "tmp/x")
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(root, "tmp", "x"), got)
	})

	t.Run("外を指す壊れた symlink の下も自身も ErrInvalid", func(t *testing.T) {
		t.Parallel()
		root, outside := realDir(t), realDir(t)
		require.NoError(t, os.MkdirAll(filepath.Join(root, "tmp"), 0o750))
		require.NoError(t, os.Symlink(filepath.Join(outside, "none"), filepath.Join(root, "tmp", "dangling")))
		for _, work := range []string{"tmp/dangling/x", "tmp/dangling"} {
			_, err := Resolve(root, work)
			require.ErrorIs(t, err, ErrInvalid, work)
		}
	})

	t.Run("root がファイルで tmp を調べられなければ ErrInvalid", func(t *testing.T) {
		t.Parallel()
		file := filepath.Join(realDir(t), "file")
		require.NoError(t, os.WriteFile(file, nil, 0o600))
		_, err := Resolve(file, "tmp/x")
		require.ErrorIs(t, err, ErrInvalid)
	})

	t.Run("相対の root でも、symlink の無い tmp の下へ解決する", func(t *testing.T) {
		t.Parallel()
		root := realDir(t)
		require.NoError(t, os.MkdirAll(filepath.Join(root, "tmp"), 0o750))
		cwd, err := os.Getwd()
		require.NoError(t, err)
		rel, err := filepath.Rel(cwd, root)
		require.NoError(t, err)
		got, err := Resolve(rel, "tmp/x")
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(rel, "tmp", "x"), got)
	})

	t.Run("-work の途中がファイルなら ErrInvalid", func(t *testing.T) {
		t.Parallel()
		root := realDir(t)
		require.NoError(t, os.MkdirAll(filepath.Join(root, "tmp"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(root, "tmp", "f"), nil, 0o600))
		_, err := Resolve(root, "tmp/f/x")
		require.ErrorIs(t, err, ErrInvalid)
	})
}

func Test_staysUnder(t *testing.T) {
	t.Parallel()

	t.Run("相対パスの base と target も絶対パスへ直して比べ、外を指す symlink を落とし、中を指す symlink を通す", func(t *testing.T) {
		t.Parallel()
		root, err := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)
		outside := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "tmp"), 0o750))
		require.NoError(t, os.Symlink(outside, filepath.Join(root, "tmp", "link")))
		cwd, err := os.Getwd()
		require.NoError(t, err)
		rel, err := filepath.Rel(cwd, root)
		require.NoError(t, err)
		require.Error(t, staysUnder(filepath.Join(rel, "tmp"), filepath.Join(rel, "tmp", "link", "x")))
		require.NoError(t, os.MkdirAll(filepath.Join(root, "tmp", "real"), 0o750))
		require.NoError(t, os.Symlink(filepath.Join(root, "tmp", "real"), filepath.Join(root, "tmp", "in")))
		require.NoError(t, staysUnder(filepath.Join(rel, "tmp"), filepath.Join(rel, "tmp", "in", "x")), "絶対パスを指す中の symlink は通る")
	})

	t.Run("名前が .. で始まるだけのディレクトリ（..x）の下は通る", func(t *testing.T) {
		t.Parallel()
		base, err := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)
		require.NoError(t, os.MkdirAll(filepath.Join(base, "..x"), 0o750))
		require.NoError(t, staysUnder(base, filepath.Join(base, "..x", "y")))
	})

	t.Run("base の親を指す symlink の下も自身も落ちる", func(t *testing.T) {
		t.Parallel()
		root, err := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)
		base := filepath.Join(root, "tmp")
		require.NoError(t, os.MkdirAll(base, 0o750))
		require.NoError(t, os.Symlink(root, filepath.Join(base, "up")))
		require.Error(t, staysUnder(base, filepath.Join(base, "up", "x")))
		require.Error(t, staysUnder(base, filepath.Join(base, "up")))
	})

	t.Run("既に在る base の下のディレクトリは通る", func(t *testing.T) {
		t.Parallel()
		base, err := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)
		require.NoError(t, os.MkdirAll(filepath.Join(base, "x"), 0o750))
		require.NoError(t, staysUnder(base, filepath.Join(base, "x")))
	})

	t.Run("外を指す壊れた symlink の下も自身も落ちる", func(t *testing.T) {
		t.Parallel()
		base, err := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)
		require.NoError(t, os.Symlink(filepath.Join(t.TempDir(), "none"), filepath.Join(base, "dangling")))
		require.Error(t, staysUnder(base, filepath.Join(base, "dangling", "x")))
		require.Error(t, staysUnder(base, filepath.Join(base, "dangling")))
	})

	t.Run("base を調べられなければ（ENOTDIR）落ちる", func(t *testing.T) {
		t.Parallel()
		file := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(file, nil, 0o600))
		require.Error(t, staysUnder(filepath.Join(file, "tmp"), filepath.Join(file, "tmp", "x")))
	})

	t.Run("base が無ければ確かめない", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, staysUnder(filepath.Join(t.TempDir(), "none"), "/anything"))
	})

	t.Run("在る祖先が base 自身なら通り、target 自身が base を指す symlink なら落ちる", func(t *testing.T) {
		t.Parallel()
		base, err := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)
		require.NoError(t, staysUnder(base, filepath.Join(base, "x", "y")))
		self := filepath.Join(base, "self")
		require.NoError(t, os.Symlink(base, self))
		require.Error(t, staysUnder(base, self))
	})
}
