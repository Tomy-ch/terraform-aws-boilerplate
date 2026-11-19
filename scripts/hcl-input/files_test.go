package main

import (
	"io/fs"
	"os"
	"path/filepath"
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
