package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/testenv"
)

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	base := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(base, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	}

	return base
}

func Test_walkFiles(t *testing.T) {
	t.Parallel()

	t.Run("root の下の .tf だけを、base からの / 区切りの相対パスで辞書順に返す", func(t *testing.T) {
		t.Parallel()
		base := writeTree(t, map[string]string{
			"modules/b/main.tf":                   "",
			"modules/a/main.tf":                   "",
			"modules/a/z/deep.tf":                 "",
			"modules/a/tests/x.tftest.hcl":        "",
			"modules/a/README.md":                 "",
			"modules/a/.terraform/modules/m/x.tf": "",
			"env/dev/.hidden/h.tf":                "",
			"other/o.tf":                          "",
		})
		got, err := walkFiles(base, []string{"modules", "./env"})
		require.NoError(t, err)
		assert.Equal(t, []string{"env/dev/.hidden/h.tf", "modules/a/main.tf", "modules/a/z/deep.tf", "modules/b/main.tf"}, got)
	})

	t.Run("重なった root のファイルを1度だけ数える", func(t *testing.T) {
		t.Parallel()
		base := writeTree(t, map[string]string{"modules/a/main.tf": ""})
		got, err := walkFiles(base, []string{"modules", "modules/a"})
		require.NoError(t, err)
		assert.Equal(t, []string{"modules/a/main.tf"}, got)
	})

	t.Run(".tf が無ければ空", func(t *testing.T) {
		t.Parallel()
		base := writeTree(t, map[string]string{"modules/a/README.md": ""})
		got, err := walkFiles(base, []string{"modules"})
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("root が無いか、ディレクトリでなければ errMissingRoot", func(t *testing.T) {
		t.Parallel()
		base := writeTree(t, map[string]string{"modules/a/main.tf": "", "file": ""})
		for name, root := range map[string]string{"無い": "usecases", "ファイル": "file"} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				_, err := walkFiles(base, []string{"modules", root})
				require.ErrorIs(t, err, errMissingRoot)
				assert.Contains(t, err.Error(), root)
			})
		}
	})

	t.Run("symlink は errSymlink", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			link   string
			target string
			root   string
		}{
			"ファイルの symlink":       {link: "modules/a/link.tf", target: "main.tf", root: "modules"},
			"名前が .tf でない symlink": {link: "modules/a/link.txt", target: "main.tf", root: "modules"},
			"壊れた symlink":         {link: "modules/a/dangling.tf", target: "../../nowhere.tf", root: "modules"},
			"ディレクトリの symlink":     {link: "modules/b", target: "a", root: "modules"},
			"root そのものの symlink":  {link: "env", target: "modules", root: "env"},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				base := writeTree(t, map[string]string{"modules/a/main.tf": ""})
				require.NoError(t, os.Symlink(tt.target, filepath.Join(base, filepath.FromSlash(tt.link))))
				_, err := walkFiles(base, []string{tt.root})
				require.ErrorIs(t, err, errSymlink)
				assert.Contains(t, err.Error(), tt.link)
			})
		}
	})

	t.Run(".terraform の下の symlink は歩かない", func(t *testing.T) {
		t.Parallel()
		base := writeTree(t, map[string]string{"modules/a/main.tf": "", "modules/a/.terraform/x": ""})
		require.NoError(t, os.Symlink("../main.tf", filepath.Join(base, "modules/a/.terraform/link.tf")))
		got, err := walkFiles(base, []string{"modules"})
		require.NoError(t, err)
		assert.Equal(t, []string{"modules/a/main.tf"}, got)
	})

	t.Run("読めないディレクトリは、走査の失敗として返す", func(t *testing.T) {
		t.Parallel()
		testenv.RequireNonRoot(t, "特権実行では権限の無いディレクトリも読めるため検証できない")
		base := writeTree(t, map[string]string{"modules/a/main.tf": ""})
		dir := filepath.Join(base, "modules", "a")
		require.NoError(t, os.Chmod(dir, 0o000))
		t.Cleanup(func() { _ = os.Chmod(dir, 0o750) })
		_, err := walkFiles(base, []string{"modules"})
		require.ErrorIs(t, err, fs.ErrPermission)
	})

	t.Run(".tf.json と、構成ファイルの名前のディレクトリは errUnsupportedInput", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			files map[string]string
			want  string
		}{
			".tf.json":            {files: map[string]string{"modules/a/override.tf.json": "{}"}, want: "modules/a/override.tf.json"},
			".tf の名前のディレクトリ":      {files: map[string]string{"modules/a/x.tf/inner.txt": ""}, want: "modules/a/x.tf"},
			".tf.json の名前のディレクトリ": {files: map[string]string{"modules/a/x.tf.json/inner.txt": ""}, want: "modules/a/x.tf.json"},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				tt.files["modules/a/main.tf"] = ""
				base := writeTree(t, tt.files)
				_, err := walkFiles(base, []string{"modules"})
				require.ErrorIs(t, err, errUnsupportedInput)
				assert.Contains(t, err.Error(), tt.want)
			})
		}
	})
}

func Test_readListed(t *testing.T) {
	t.Parallel()

	t.Run("行ごとのパスを、./ を外して辞書順の重複なしで返す", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(t.TempDir(), "found.txt")
		require.NoError(t, os.WriteFile(p, []byte("modules/b/main.tf\n./modules/a/main.tf\n\nmodules/a/main.tf\n"), 0o600))
		got, err := readListed(p)
		require.NoError(t, err)
		assert.Equal(t, []string{"modules/a/main.tf", "modules/b/main.tf"}, got)
	})

	t.Run("空の一覧は空", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(t.TempDir(), "found.txt")
		require.NoError(t, os.WriteFile(p, nil, 0o600))
		got, err := readListed(p)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("読めなければ errListed", func(t *testing.T) {
		t.Parallel()
		_, err := readListed(filepath.Join(t.TempDir(), "missing.txt"))
		require.ErrorIs(t, err, errListed)
	})
}

func Test_compareFiles(t *testing.T) {
	t.Parallel()

	t.Run("同じ集合なら通す", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, compareFiles([]string{"a.tf", "b.tf"}, []string{"a.tf", "b.tf"}))
	})

	t.Run("どちらも空なら通す", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, compareFiles(nil, nil))
	})

	t.Run("過不足は、どちらの向きも errFileSetMismatch で名指す", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			walked []string
			listed []string
			want   []string
		}{
			"一覧に無いファイルを読んだ":  {walked: []string{"a.tf", "b.tf"}, listed: []string{"a.tf"}, want: []string{"b.tf"}},
			"一覧のファイルを読んでいない": {walked: []string{"a.tf"}, listed: []string{"a.tf", "c.tf"}, want: []string{"c.tf"}},
			"両向き":  {walked: []string{"a.tf", "b.tf"}, listed: []string{"a.tf", "c.tf"}, want: []string{"b.tf", "c.tf"}},
			"一覧が空": {walked: []string{"a.tf"}, listed: nil, want: []string{"a.tf"}},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				err := compareFiles(tt.walked, tt.listed)
				require.ErrorIs(t, err, errFileSetMismatch)
				for _, w := range tt.want {
					assert.Contains(t, err.Error(), w)
				}
			})
		}
	})
}

func Test_walkAllFiles(t *testing.T) {
	t.Parallel()

	t.Run("root の下の通常のファイルすべてを、構文として読むかと合わせて辞書順に返す", func(t *testing.T) {
		t.Parallel()
		base := writeTree(t, map[string]string{
			"modules/b/main.tf":                   "",
			"modules/a/main.tf":                   "",
			"modules/a/tests/x.tftest.hcl":        "",
			"modules/a/override.tf.json":          "{}",
			"modules/a/README.md":                 "",
			"modules/a/.terraform.lock.hcl":       "",
			"modules/a/.terraform/modules/m/x.tf": "",
			"env/dev/.hidden/h.tf":                "",
			"other/o.tf":                          "",
		})
		got, err := walkAllFiles(base, []string{"modules", "./env", "modules/a"})
		require.NoError(t, err)
		assert.Equal(t, []fileEntry{
			{Path: "env/dev/.hidden/h.tf", Parsed: true},
			{Path: "modules/a/.terraform.lock.hcl", Parsed: false},
			{Path: "modules/a/README.md", Parsed: false},
			{Path: "modules/a/main.tf", Parsed: true},
			{Path: "modules/a/override.tf.json", Parsed: false},
			{Path: "modules/a/tests/x.tftest.hcl", Parsed: true},
			{Path: "modules/b/main.tf", Parsed: true},
		}, got)
	})

	t.Run(".terraform という名前の通常のファイルは列挙し、隣の .tf も列挙する", func(t *testing.T) {
		t.Parallel()
		base := writeTree(t, map[string]string{"modules/a/.terraform": "", "modules/a/main.tf": ""})
		got, err := walkAllFiles(base, []string{"modules"})
		require.NoError(t, err)
		assert.Equal(t, []fileEntry{{Path: "modules/a/.terraform"}, {Path: "modules/a/main.tf", Parsed: true}}, got)
	})

	t.Run("ファイルが無ければ空", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(base, "modules"), 0o750))
		got, err := walkAllFiles(base, []string{"modules"})
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("root が無いか、ディレクトリでなければ errMissingRoot", func(t *testing.T) {
		t.Parallel()
		base := writeTree(t, map[string]string{"modules/a/main.tf": "", "file": ""})
		for name, root := range map[string]string{"無い": "usecases", "ファイル": "file"} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				_, err := walkAllFiles(base, []string{"modules", root})
				require.ErrorIs(t, err, errMissingRoot)
			})
		}
	})

	t.Run("symlink は、名前を問わず errSymlink", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			link, target, root string
		}{
			"ファイルの symlink":      {link: "modules/a/link.tf", target: "main.tf", root: "modules"},
			"README の symlink":   {link: "modules/a/README.md", target: "main.tf", root: "modules"},
			"ディレクトリの symlink":    {link: "modules/b", target: "a", root: "modules"},
			"root そのものの symlink": {link: "env", target: "modules", root: "env"},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				base := writeTree(t, map[string]string{"modules/a/main.tf": ""})
				require.NoError(t, os.Symlink(tt.target, filepath.Join(base, filepath.FromSlash(tt.link))))
				_, err := walkAllFiles(base, []string{tt.root})
				require.ErrorIs(t, err, errSymlink)
				assert.Contains(t, err.Error(), tt.link)
			})
		}
	})

	t.Run("構成ファイルの名前のディレクトリは errUnsupportedInput", func(t *testing.T) {
		t.Parallel()
		for _, d := range []string{"x.tf", "x.tf.json", "x.tftest.hcl", "d.tftest.json"} {
			t.Run(d, func(t *testing.T) {
				t.Parallel()
				base := writeTree(t, map[string]string{"modules/a/main.tf": "", "modules/a/" + d + "/inner.txt": ""})
				_, err := walkAllFiles(base, []string{"modules"})
				require.ErrorIs(t, err, errUnsupportedInput)
				assert.Contains(t, err.Error(), "modules/a/"+d)
			})
		}
	})

	t.Run("構成ファイルの名前の root は errUnsupportedInput", func(t *testing.T) {
		t.Parallel()
		base := writeTree(t, map[string]string{"foo.tf/a/main.tf": ""})
		_, err := walkAllFiles(base, []string{"foo.tf"})
		require.ErrorIs(t, err, errUnsupportedInput)
	})

	t.Run("通常のファイルでもディレクトリでもないもの（FIFO）は errUnsupportedInput", func(t *testing.T) {
		t.Parallel()
		base := writeTree(t, map[string]string{"modules/a/main.tf": ""})
		require.NoError(t, syscall.Mkfifo(filepath.Join(base, "modules/a/pipe"), 0o600))
		_, err := walkAllFiles(base, []string{"modules"})
		require.ErrorIs(t, err, errUnsupportedInput)
		assert.Contains(t, err.Error(), "modules/a/pipe")
	})

	t.Run("読めないディレクトリは、走査の失敗として返す", func(t *testing.T) {
		t.Parallel()
		testenv.RequireNonRoot(t, "特権実行では権限の無いディレクトリも読めるため検証できない")
		base := writeTree(t, map[string]string{"modules/a/main.tf": ""})
		dir := filepath.Join(base, "modules", "a")
		require.NoError(t, os.Chmod(dir, 0o000))
		t.Cleanup(func() { _ = os.Chmod(dir, 0o750) })
		_, err := walkAllFiles(base, []string{"modules"})
		require.ErrorIs(t, err, fs.ErrPermission)
	})
}

func Test_isParsed(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		"main.tf": true, "a.tftest.hcl": true, "a.tf.json": false, "a.tfquery.hcl": false,
		".terraform.lock.hcl": false, "README.md": false, "terraform.tfvars": false,
	}
	for name, want := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, want, isParsed(name))
		})
	}
}

func Test_hasSuffix(t *testing.T) {
	t.Parallel()

	assert.True(t, hasSuffix("a.tf.json", []string{".tf", ".tf.json"}))
	assert.False(t, hasSuffix("a.tfvars", []string{".tf", ".tf.json"}))
	assert.False(t, hasSuffix("a.tf", nil))
}
