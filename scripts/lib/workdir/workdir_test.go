package workdir

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/gitenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// git は、dir で git を起動し、失敗すればテストを落とします。
func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-c", "core.hooksPath=/dev/null"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

// snapshot は、dir の下のファイルの中身を、dir からのパスで引けるように集めます。
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	fsys := os.DirFS(dir)
	files := map[string]string{}
	require.NoError(t, fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(fsys, p)
		files[p] = string(b)
		return err
	}))
	return files
}

// caseInsensitive は、t.TempDir() のファイルシステムが大文字小文字を区別しないかを返します。
func caseInsensitive(t *testing.T) bool {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a"), nil, 0o600))
	_, err := os.Lstat(filepath.Join(dir, "A"))
	return err == nil
}

func TestResolve(t *testing.T) {
	t.Run("tmp が symlink で git を起動できなければ ErrInvalid", func(t *testing.T) {
		root, err := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)
		require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(root, "tmp")))
		t.Setenv("PATH", t.TempDir())
		_, err = Resolve(root, "tmp/x")
		require.ErrorIs(t, err, ErrInvalid)
		assert.Contains(t, err.Error(), gitenv.ErrNames.Error(), "env を切り離す段で落ちる")
	})

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

	t.Run("tmp が git の追跡していない symlink なら、その先を tmp/ として扱う", func(t *testing.T) {
		t.Parallel()
		root, elsewhere := realDir(t), realDir(t)
		git(t, root, "init", "-q")
		require.NoError(t, os.Symlink(elsewhere, filepath.Join(root, "tmp")))
		got, err := Resolve(root, "tmp/x")
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(root, "tmp", "x"), got)
	})

	t.Run("tmp が git の追跡している symlink なら ErrInvalid で、その先も .git も変えない", func(t *testing.T) {
		t.Parallel()
		root, outside := realDir(t), realDir(t)
		git(t, root, "init", "-q")
		require.NoError(t, os.WriteFile(filepath.Join(outside, "keep"), []byte("keep"), 0o600))
		require.NoError(t, os.Symlink(outside, filepath.Join(root, "tmp")))
		git(t, root, "add", "tmp")
		before, dotGit := snapshot(t, outside), snapshot(t, filepath.Join(root, ".git"))
		_, err := Resolve(root, "tmp/x")
		require.ErrorIs(t, err, ErrInvalid)
		assert.Contains(t, err.Error(), "追跡している symlink", "原因を添える")
		assert.Equal(t, before, snapshot(t, outside))
		assert.Equal(t, dotGit, snapshot(t, filepath.Join(root, ".git")))
	})

	t.Run("tmp が symlink で root がリポジトリでなければ、追跡していないと確かめられず ErrInvalid", func(t *testing.T) {
		t.Parallel()
		root, elsewhere := realDir(t), realDir(t)
		require.NoError(t, os.Symlink(elsewhere, filepath.Join(root, "tmp")))
		_, err := Resolve(root, "tmp/x")
		require.ErrorIs(t, err, ErrInvalid)
		assert.Contains(t, err.Error(), "確かめられません")
	})

	t.Run("大文字小文字を区別しないファイルシステムで、git の追跡している TMP が symlink なら ErrInvalid", func(t *testing.T) {
		t.Parallel()
		if !caseInsensitive(t) {
			t.Skip("大文字小文字を区別するファイルシステムでは TMP を tmp として開かないので、この経路が無い")
		}
		root, outside := realDir(t), realDir(t)
		git(t, root, "init", "-q")
		require.NoError(t, os.Symlink(outside, filepath.Join(root, "TMP")))
		git(t, root, "add", "TMP")
		_, err := Resolve(root, "tmp/x")
		require.ErrorIs(t, err, ErrInvalid)
		assert.Contains(t, err.Error(), "追跡している symlink")
	})

	t.Run("git の追跡している symlink の tmp の先の symlink が外を指せば、追跡している tmp を原因に ErrInvalid", func(t *testing.T) {
		t.Parallel()
		root, outside, further := realDir(t), realDir(t), realDir(t)
		git(t, root, "init", "-q")
		require.NoError(t, os.Symlink(outside, filepath.Join(root, "tmp")))
		require.NoError(t, os.Symlink(further, filepath.Join(outside, "link")))
		git(t, root, "add", "tmp")
		_, err := Resolve(root, "tmp/link/x")
		require.ErrorIs(t, err, ErrInvalid)
		assert.Contains(t, err.Error(), "追跡している symlink")
		assert.NotContains(t, err.Error(), " の下にありません", "tmp の検証が先に落とす")
	})

	for name, track := range map[string]bool{"追跡していない": false, "追跡している": true} {
		t.Run("tmp が git の"+name+"ファイルなら ErrInvalid", func(t *testing.T) {
			t.Parallel()
			root := realDir(t)
			git(t, root, "init", "-q")
			require.NoError(t, os.WriteFile(filepath.Join(root, "tmp"), []byte("f"), 0o600))
			if track {
				git(t, root, "add", "tmp")
			}
			_, err := Resolve(root, "tmp/x")
			require.ErrorIs(t, err, ErrInvalid)
			assert.Contains(t, err.Error(), "not a directory")
		})
	}

	t.Run("tmp/ の下の git の追跡している symlink が外を指せば ErrInvalid", func(t *testing.T) {
		t.Parallel()
		root, outside := realDir(t), realDir(t)
		git(t, root, "init", "-q")
		require.NoError(t, os.MkdirAll(filepath.Join(root, "tmp"), 0o750))
		require.NoError(t, os.Symlink(outside, filepath.Join(root, "tmp", "link")))
		git(t, root, "add", "-f", "tmp/link")
		_, err := Resolve(root, "tmp/link/x")
		require.ErrorIs(t, err, ErrInvalid)
		assert.Contains(t, err.Error(), " の下にありません")
	})

	t.Run("tmp が git の追跡していない壊れた symlink なら ErrInvalid", func(t *testing.T) {
		t.Parallel()
		root := realDir(t)
		git(t, root, "init", "-q")
		require.NoError(t, os.Symlink(filepath.Join(root, "none"), filepath.Join(root, "tmp")))
		_, err := Resolve(root, "tmp/x")
		require.ErrorIs(t, err, ErrInvalid)
		assert.Contains(t, err.Error(), "lstat "+filepath.Join(root, "none")+": ", "落とすのは symlink の先の検証")
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

func Test_resolve(t *testing.T) {
	t.Parallel()

	t.Run("tmp が symlink でなければ git を起動しない", func(t *testing.T) {
		t.Parallel()
		root, err := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)
		require.NoError(t, os.MkdirAll(filepath.Join(root, "tmp"), 0o750))
		got, err := resolve(root, "tmp/x", func(string) ([]byte, error) {
			t.Error("git を起動した")
			return nil, nil
		})
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(root, "tmp", "x"), got)
	})

	t.Run("tmp が symlink で git を起動できなければ ErrInvalid で work と原因を名指す", func(t *testing.T) {
		t.Parallel()
		root, err := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)
		require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(root, "tmp")))
		_, err = resolve(root, "tmp/x", func(string) ([]byte, error) { return nil, exec.ErrNotFound })
		require.ErrorIs(t, err, ErrInvalid)
		assert.Contains(t, err.Error(), `"tmp/x": `)
		assert.Contains(t, err.Error(), exec.ErrNotFound.Error())
	})
}

func Test_untrackedIfSymlink(t *testing.T) {
	t.Parallel()

	never := func(t *testing.T) lsFilesFunc {
		t.Helper()
		return func(string) ([]byte, error) {
			t.Error("git を起動した")
			return nil, nil
		}
	}

	t.Run("tmp が無ければ git を起動せずに通す", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, untrackedIfSymlink(t.TempDir(), never(t)))
	})

	t.Run("tmp が実ディレクトリなら git を起動せずに通す", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "tmp"), 0o750))
		require.NoError(t, untrackedIfSymlink(root, never(t)))
	})

	t.Run("tmp を調べられなければ（ENOTDIR）git を起動せずに落とす", func(t *testing.T) {
		t.Parallel()
		file := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(file, nil, 0o600))
		require.Error(t, untrackedIfSymlink(file, never(t)))
	})

	for name, c := range map[string]struct {
		out  []byte
		err  error
		pass bool
	}{
		"git の出力が空なら通す":  {pass: true},
		"git の出力があれば落とす": {out: []byte("tmp\x00")},
		"git が失敗すれば落とす":  {err: exec.ErrNotFound},
	} {
		t.Run("tmp が symlink で、"+name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(root, "tmp")))
			var asked string
			err := untrackedIfSymlink(root, func(r string) ([]byte, error) {
				asked = r
				return c.out, c.err
			})
			assert.Equal(t, root, asked, "root で git を起動する")
			if c.pass {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
		})
	}
}

func Test_lsFilesTmp(t *testing.T) {
	trackedTmp := func(t *testing.T) string {
		t.Helper()
		root := t.TempDir()
		git(t, root, "init", "-q")
		require.NoError(t, os.MkdirAll(filepath.Join(root, "tmp"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(root, "tmp", "a"), nil, 0o600))
		git(t, root, "add", "-f", "tmp/a")
		return root
	}

	t.Run("外側のリポジトリを指す GIT_DIR と GIT_INDEX_FILE を外して、root のリポジトリに問う", func(t *testing.T) {
		root, other := trackedTmp(t), t.TempDir()
		git(t, other, "init", "-q")
		t.Setenv("GIT_DIR", filepath.Join(other, ".git"))
		t.Setenv("GIT_INDEX_FILE", filepath.Join(other, ".git", "index"))
		out, err := lsFilesTmp(root)
		require.NoError(t, err)
		assert.Equal(t, "tmp/a\x00", string(out))
	})

	t.Run("GIT_LITERAL_PATHSPECS が立っていても、大文字小文字を区別せずに問う", func(t *testing.T) {
		root := trackedTmp(t)
		t.Setenv("GIT_LITERAL_PATHSPECS", "1")
		out, err := lsFilesTmp(root)
		require.NoError(t, err)
		assert.Equal(t, "tmp/a\x00", string(out))
	})

	t.Run("追跡している TMP を、大文字小文字を区別せずに返す", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		git(t, root, "init", "-q")
		require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(root, "TMP")))
		git(t, root, "add", "TMP")
		out, err := lsFilesTmp(root)
		require.NoError(t, err)
		assert.Equal(t, "TMP\x00", string(out))
	})

	t.Run("追跡している tmp の下を返し、名前が tmp で始まるだけのものは返さない", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		git(t, root, "init", "-q")
		require.NoError(t, os.MkdirAll(filepath.Join(root, "tmp"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(root, "tmp", "a"), nil, 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(root, "tmpx"), nil, 0o600))
		git(t, root, "add", "tmpx")
		out, err := lsFilesTmp(root)
		require.NoError(t, err)
		assert.Empty(t, out)
		git(t, root, "add", "-f", "tmp/a")
		out, err = lsFilesTmp(root)
		require.NoError(t, err)
		assert.Equal(t, "tmp/a\x00", string(out))
	})

	t.Run("リポジトリでなければ、git の stderr を添えて失敗する", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		cmd := exec.CommandContext(t.Context(), "git", "ls-files")
		cmd.Dir = dir
		stderr, gitErr := cmd.CombinedOutput()
		require.Error(t, gitErr)
		_, err := lsFilesTmp(dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exit status 128: "+strings.TrimSpace(string(stderr)))
	})
}

func TestRecreate(t *testing.T) {
	t.Parallel()

	t.Run("在る -work を中身ごと消し、空のディレクトリとして作り直して root からのパスを返す", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "tmp", "w", "old"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(root, "tmp", "w", "old", "f"), []byte("x"), 0o600))
		got, err := Recreate(root, "tmp/w")
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(root, "tmp", "w"), got)
		entries, err := os.ReadDir(got)
		require.NoError(t, err)
		assert.Empty(t, entries)
	})

	t.Run("tmp も -work も無ければ作る", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		got, err := Recreate(root, "tmp/a/b")
		require.NoError(t, err)
		info, err := os.Stat(got)
		require.NoError(t, err)
		assert.True(t, info.IsDir())
	})

	for _, work := range []string{"", "tmp", "../x", "/abs/tmp/x", "keep"} {
		t.Run(work+" は ErrInvalid で、何も消さない", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, "keep"), []byte("k"), 0o600))
			before := snapshot(t, root)
			_, err := Recreate(root, work)
			require.ErrorIs(t, err, ErrInvalid)
			assert.Equal(t, before, snapshot(t, root))
		})
	}

	t.Run("tmp/ の下を指す絶対パスの symlink を経た -work も、実パスの側で作り直す", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		tmp := filepath.Join(root, "tmp")
		require.NoError(t, os.MkdirAll(filepath.Join(tmp, "real", "w"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(tmp, "real", "w", "old"), []byte("x"), 0o600))
		require.NoError(t, os.Symlink(filepath.Join(tmp, "real"), filepath.Join(tmp, "link")))
		_, err := Recreate(root, "tmp/link/w")
		require.NoError(t, err)
		entries, err := os.ReadDir(filepath.Join(tmp, "real", "w"))
		require.NoError(t, err)
		assert.Empty(t, entries)
	})

	t.Run("tmp/ の下の symlink が外を指せば ErrInvalid で、外は消さない", func(t *testing.T) {
		t.Parallel()
		root, outside := t.TempDir(), t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(outside, "f"), []byte("o"), 0o600))
		require.NoError(t, os.MkdirAll(filepath.Join(root, "tmp"), 0o750))
		require.NoError(t, os.Symlink(outside, filepath.Join(root, "tmp", "out")))
		_, err := Recreate(root, "tmp/out/w")
		require.ErrorIs(t, err, ErrInvalid)
		assert.Equal(t, map[string]string{"f": "o"}, snapshot(t, outside))
	})
}

func Test_realRel(t *testing.T) {
	t.Parallel()

	t.Run("tmp の下を指す絶対パスの symlink を解き、実パスからの相対パスを返す", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		tmp := filepath.Join(root, "tmp")
		require.NoError(t, os.MkdirAll(filepath.Join(tmp, "real"), 0o750))
		require.NoError(t, os.Symlink(filepath.Join(tmp, "real"), filepath.Join(tmp, "link")))
		tmpReal, rel, err := realRel(tmp, filepath.Join(tmp, "link", "w"))
		require.NoError(t, err)
		want, err := filepath.EvalSymlinks(tmp)
		require.NoError(t, err)
		assert.Equal(t, want, tmpReal)
		assert.Equal(t, filepath.Join("real", "w"), rel)
	})

	t.Run("解いた先が tmp 自身か外なら失敗する", func(t *testing.T) {
		t.Parallel()
		root, outside := t.TempDir(), t.TempDir()
		tmp := filepath.Join(root, "tmp")
		require.NoError(t, os.MkdirAll(tmp, 0o750))
		require.NoError(t, os.Symlink(tmp, filepath.Join(tmp, "self")))
		require.NoError(t, os.Symlink(outside, filepath.Join(tmp, "out")))
		_, _, err := realRel(tmp, filepath.Join(tmp, "self"))
		require.Error(t, err)
		_, _, err = realRel(tmp, filepath.Join(tmp, "out", "w"))
		require.Error(t, err)
	})

	t.Run("tmp が無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, _, err := realRel(filepath.Join(t.TempDir(), "tmp"), "x")
		require.Error(t, err)
	})
}

func Test_rebuild(t *testing.T) {
	t.Parallel()

	t.Run("検証の後に途中が外への symlink へ差し替えられても、外へは辿らずに失敗し、外は消さない", func(t *testing.T) {
		t.Parallel()
		tmp, outside := t.TempDir(), t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(outside, "w"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(outside, "w", "f"), []byte("o"), 0o600))
		require.NoError(t, os.Symlink(outside, filepath.Join(tmp, "x")))
		require.Error(t, rebuild(tmp, "x/w"))
		assert.Equal(t, map[string]string{"w/f": "o"}, snapshot(t, outside))
	})

	t.Run("tmp を開けなければ失敗する", func(t *testing.T) {
		t.Parallel()
		require.Error(t, rebuild(filepath.Join(t.TempDir(), "none"), "w"))
	})
}

func TestTemp(t *testing.T) {
	t.Parallel()

	t.Run("parent の下に一時ディレクトリを作り、後片付けはその子だけを消す", func(t *testing.T) {
		t.Parallel()
		parent := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(parent, "keep"), []byte("k"), 0o600))
		dir, cleanup, err := Temp(parent, "x-")
		require.NoError(t, err)
		assert.Equal(t, parent, filepath.Dir(dir))
		assert.True(t, strings.HasPrefix(filepath.Base(dir), "x-"))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "f"), []byte("t"), 0o600))
		cleanup()
		_, err = os.Lstat(dir)
		require.ErrorIs(t, err, fs.ErrNotExist)
		assert.Equal(t, map[string]string{"keep": "k"}, snapshot(t, parent))
	})

	t.Run("parent が無ければ失敗し、後片付けを返さない", func(t *testing.T) {
		t.Parallel()
		dir, cleanup, err := Temp(filepath.Join(t.TempDir(), "none"), "x-")
		require.Error(t, err)
		assert.Empty(t, dir)
		assert.Nil(t, cleanup)
	})

	t.Run("後片付けの前に parent が消えていても、後片付けは何もせずに戻る", func(t *testing.T) {
		t.Parallel()
		parent := filepath.Join(t.TempDir(), "p")
		require.NoError(t, os.Mkdir(parent, 0o750))
		dir, cleanup, err := Temp(parent, "x-")
		require.NoError(t, err)
		require.NoError(t, os.Remove(dir))
		require.NoError(t, os.Remove(parent))
		assert.NotPanics(t, cleanup)
	})
}

func TestRemove(t *testing.T) {
	t.Parallel()

	t.Run("-work を中身ごと消し、tmp/ のほかは残す", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "tmp", "w", "sub"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(root, "tmp", "w", "sub", "f"), []byte("x"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(root, "tmp", "keep"), []byte("k"), 0o600))
		require.NoError(t, Remove(root, "tmp/w"))
		assert.Equal(t, map[string]string{"tmp/keep": "k"}, snapshot(t, root))
	})

	t.Run("-work も tmp も無ければ何もしない", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		require.NoError(t, Remove(root, "tmp/w"))
		require.NoError(t, os.Mkdir(filepath.Join(root, "tmp"), 0o750))
		require.NoError(t, Remove(root, "tmp/w"))
	})

	for _, work := range []string{"", "tmp", "../x", "keep"} {
		t.Run(work+" は ErrInvalid で、何も消さない", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, "keep"), []byte("k"), 0o600))
			require.ErrorIs(t, Remove(root, work), ErrInvalid)
			assert.Equal(t, map[string]string{"keep": "k"}, snapshot(t, root))
		})
	}

	t.Run("tmp/ の下の symlink が外を指せば ErrInvalid で、外は消さない", func(t *testing.T) {
		t.Parallel()
		root, outside := t.TempDir(), t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(outside, "f"), []byte("o"), 0o600))
		require.NoError(t, os.MkdirAll(filepath.Join(root, "tmp"), 0o750))
		require.NoError(t, os.Symlink(outside, filepath.Join(root, "tmp", "out")))
		require.ErrorIs(t, Remove(root, "tmp/out"), ErrInvalid)
		require.ErrorIs(t, Remove(root, "tmp/out/f"), ErrInvalid)
		assert.Equal(t, map[string]string{"f": "o"}, snapshot(t, outside))
	})
}

func Test_recreate(t *testing.T) {
	t.Parallel()

	t.Run("tmp が symlink で追跡していないことを確かめられなければ ErrInvalid で、何も消さない", func(t *testing.T) {
		t.Parallel()
		root, target := t.TempDir(), t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(target, "w"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(target, "w", "f"), []byte("x"), 0o600))
		require.NoError(t, os.Symlink(target, filepath.Join(root, "tmp")))
		_, err := recreate(root, "tmp/w", func(string) ([]byte, error) { return nil, exec.ErrNotFound })
		require.ErrorIs(t, err, ErrInvalid)
		assert.Equal(t, map[string]string{"w/f": "x"}, snapshot(t, target))
	})

	t.Run("tmp が追跡していない symlink なら、その先で作り直す", func(t *testing.T) {
		t.Parallel()
		root, target := t.TempDir(), t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(target, "w"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(target, "w", "f"), []byte("x"), 0o600))
		require.NoError(t, os.Symlink(target, filepath.Join(root, "tmp")))
		_, err := recreate(root, "tmp/w", func(string) ([]byte, error) { return nil, nil })
		require.NoError(t, err)
		assert.Empty(t, snapshot(t, target))
		assert.DirExists(t, filepath.Join(target, "w"))
	})
}

func Test_remove(t *testing.T) {
	t.Parallel()

	t.Run("tmp が追跡している symlink なら ErrInvalid で、その先を消さない", func(t *testing.T) {
		t.Parallel()
		root, target := t.TempDir(), t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(target, "w"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(target, "w", "f"), []byte("x"), 0o600))
		require.NoError(t, os.Symlink(target, filepath.Join(root, "tmp")))
		err := remove(root, "tmp/w", func(string) ([]byte, error) { return []byte("tmp\x00"), nil })
		require.ErrorIs(t, err, ErrInvalid)
		assert.Equal(t, map[string]string{"w/f": "x"}, snapshot(t, target))
	})

	t.Run("tmp が追跡していない symlink なら、その先の -work を消す", func(t *testing.T) {
		t.Parallel()
		root, target := t.TempDir(), t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(target, "w"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(target, "w", "f"), []byte("x"), 0o600))
		require.NoError(t, os.Symlink(target, filepath.Join(root, "tmp")))
		require.NoError(t, remove(root, "tmp/w", func(string) ([]byte, error) { return nil, nil }))
		assert.NoDirExists(t, filepath.Join(target, "w"))
	})
}
