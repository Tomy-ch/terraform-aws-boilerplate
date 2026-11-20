package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/testenv"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	moduleTF  = "resource \"aws_s3_bucket_policy\" \"this\" {\n  bucket = aws_s3_bucket.this[module.x.key].id\n}\n"
	usecaseTF = "module \"a\" {\n  source = \"../../modules/a\"\n}\n"
)

// fixture は base に構成と find の一覧を置き、-listed と -out のパスを返す。listed が nil なら、置いた
// .tf のうち .terraform の外のものを書く（layer.mk の find と同じ）。
func fixture(t *testing.T, files map[string]string, listed []string) (base, listedPath, outPath string) {
	t.Helper()
	base = writeTree(t, files)
	if listed == nil {
		for rel := range files {
			if strings.HasSuffix(rel, ".tf") && !strings.Contains(rel, "/.terraform/") {
				listed = append(listed, rel)
			}
		}
	}
	work := t.TempDir()
	listedPath = filepath.Join(work, "found.txt")
	require.NoError(t, os.WriteFile(listedPath, []byte(strings.Join(listed, "\n")+"\n"), 0o600))

	return base, listedPath, filepath.Join(work, "out", "hcl-input.json")
}

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("読んだ構成を JSON に書き、件数を報告する", func(t *testing.T) {
		t.Parallel()
		base, listed, out := fixture(t, map[string]string{
			"modules/a/main.tf":                     moduleTF,
			"usecases/u/main.tf":                    usecaseTF,
			"modules/a/tests/broken.tftest.hcl":     "run {",
			"modules/a/.terraform/modules/m/bad.tf": "resource {",
		}, nil)
		require.NoError(t, os.MkdirAll(filepath.Dir(out), 0o750))
		var stdout bytes.Buffer
		require.NoError(t, run([]string{"-base", base, "-listed", listed, "-out", out, "usecases", "./modules"}, &stdout))

		raw, err := os.ReadFile(out)
		require.NoError(t, err)
		assert.JSONEq(t, `{"hcl_input": {
			"files": ["modules/a/main.tf", "usecases/u/main.tf"],
			"blocks": [
				{"file": "modules/a/main.tf", "kind": "resource", "labels": ["aws_s3_bucket_policy", "this"], "attributes": [
					{"path": "bucket", "refs": [
						{"root": "aws_s3_bucket", "steps": [{"kind": "attr", "name": "this"}, {"kind": "index"}, {"kind": "attr", "name": "id"}], "within": []},
						{"root": "module", "steps": [{"kind": "attr", "name": "x"}, {"kind": "attr", "name": "key"}], "within": [{"ref": 0, "step": 1}]}
					]}
				]},
				{"file": "usecases/u/main.tf", "kind": "module", "labels": ["a"], "attributes": [{"path": "source", "refs": []}]}
			]
		}}`, string(raw))
		assert.Equal(t, "✅ hcl-input: 2 件のファイルから 2 個の block を "+out+" へ書きました\n", stdout.String())
	})

	t.Run("引数が足りなければ errUsage", func(t *testing.T) {
		t.Parallel()

		tests := map[string][]string{
			"引数が無い":        nil,
			"-base が無い":    {"-listed", "l.txt", "-out", "o.json", "modules"},
			"-listed が無い":  {"-base", ".", "-out", "o.json", "modules"},
			"-out が無い":     {"-base", ".", "-listed", "l.txt", "modules"},
			"root が無い":     {"-base", ".", "-listed", "l.txt", "-out", "o.json"},
			"知らないフラグ":      {"-x", "-base", ".", "-listed", "l.txt", "-out", "o.json", "modules"},
			"知らない -format": {"-format", "refs", "-base", ".", "-listed", "l.txt", "-out", "o.json", "modules"},
		}
		for name, args := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				require.ErrorIs(t, run(args, &bytes.Buffer{}), errUsage)
			})
		}
	})

	t.Run("失敗した実行は出力を書かない", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			files  map[string]string
			listed []string
			roots  []string
			want   error
		}{
			"root が無い": {
				files: map[string]string{"modules/a/main.tf": moduleTF}, roots: []string{"modules", "env"}, want: errMissingRoot,
			},
			".tf が0件": {
				files: map[string]string{"modules/a/README.md": ""}, listed: []string{}, roots: []string{"modules"}, want: errNoInput,
			},
			"構文の誤り": {
				files: map[string]string{"modules/a/main.tf": "resource \"aws_s3_bucket\" {\n"}, roots: []string{"modules"}, want: errSyntax,
			},
			"ファイルの直下の属性": {
				files: map[string]string{"modules/a/main.tf": "x = 1\n"}, roots: []string{"modules"}, want: errUnsupportedInput,
			},
			"一覧に無いファイルを読んだ": {
				files: map[string]string{"modules/a/main.tf": moduleTF, "modules/a/b.tf": ""}, listed: []string{"modules/a/main.tf"},
				roots: []string{"modules"}, want: errFileSetMismatch,
			},
			"一覧のファイルを読んでいない": {
				files: map[string]string{"modules/a/main.tf": moduleTF}, listed: []string{"modules/a/main.tf", "modules/a/gone.tf"},
				roots: []string{"modules"}, want: errFileSetMismatch,
			},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				base, listed, out := fixture(t, tt.files, tt.listed)
				require.NoError(t, os.MkdirAll(filepath.Dir(out), 0o750))
				args := append([]string{"-base", base, "-listed", listed, "-out", out}, tt.roots...)
				require.ErrorIs(t, run(args, &bytes.Buffer{}), tt.want)
				assert.NoFileExists(t, out)
			})
		}
	})

	t.Run("一覧を読めなければ errListed で、出力を書かない", func(t *testing.T) {
		t.Parallel()
		base, _, out := fixture(t, map[string]string{"modules/a/main.tf": moduleTF}, nil)
		err := run([]string{"-base", base, "-listed", filepath.Join(base, "missing.txt"), "-out", out, "modules"}, &bytes.Buffer{})
		require.ErrorIs(t, err, errListed)
		assert.NoFileExists(t, out)
	})

	t.Run("走査の分類は一覧との突き合わせより先に落とす", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			files   map[string]string
			listed  []string
			symlink string
			want    error
		}{
			".tf.json を一覧が持つ": {
				files:  map[string]string{"modules/a/main.tf": moduleTF, "modules/a/o.tf.json": "{}"},
				listed: []string{"modules/a/main.tf", "modules/a/o.tf.json"}, want: errUnsupportedInput,
			},
			".tf.json を一覧が持たない": {
				files:  map[string]string{"modules/a/main.tf": moduleTF, "modules/a/o.tf.json": "{}"},
				listed: []string{"modules/a/main.tf"}, want: errUnsupportedInput,
			},
			".tf の名前のディレクトリを一覧が持つ": {
				files:  map[string]string{"modules/a/main.tf": moduleTF, "modules/a/d.tf/x.txt": ""},
				listed: []string{"modules/a/d.tf", "modules/a/main.tf"}, want: errUnsupportedInput,
			},
			".tf の名前のディレクトリの下の .tf": {
				files:  map[string]string{"modules/a/main.tf": moduleTF, "modules/a/d.tf/x.tf": ""},
				listed: []string{"modules/a/d.tf", "modules/a/d.tf/x.tf", "modules/a/main.tf"}, want: errUnsupportedInput,
			},
			"symlink を一覧が持つ": {
				files:  map[string]string{"modules/a/main.tf": moduleTF},
				listed: []string{"modules/a/link.tf", "modules/a/main.tf"}, symlink: "modules/a/link.tf", want: errSymlink,
			},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				base, listed, out := fixture(t, tt.files, tt.listed)
				if tt.symlink != "" {
					require.NoError(t, os.Symlink("main.tf", filepath.Join(base, filepath.FromSlash(tt.symlink))))
				}
				err := run([]string{"-base", base, "-listed", listed, "-out", out, "modules"}, &bytes.Buffer{})
				require.ErrorIs(t, err, tt.want)
				require.NotErrorIs(t, err, errFileSetMismatch)
				assert.NoFileExists(t, out)
			})
		}
	})

	t.Run("構成ファイルを読めなければ失敗し、出力を書かない", func(t *testing.T) {
		t.Parallel()
		testenv.RequireNonRoot(t, "特権実行では権限の無いファイルも読めるため検証できない")
		base, listed, out := fixture(t, map[string]string{"modules/a/main.tf": moduleTF}, nil)
		require.NoError(t, os.Chmod(filepath.Join(base, "modules", "a", "main.tf"), 0o000))
		err := run([]string{"-base", base, "-listed", listed, "-out", out, "modules"}, &bytes.Buffer{})
		require.ErrorIs(t, err, fs.ErrPermission)
		assert.NoFileExists(t, out)
	})

	t.Run("書き込めなければ失敗し、出力を残さない", func(t *testing.T) {
		t.Parallel()
		testenv.RequireNonRoot(t, "特権実行では読み取り専用ディレクトリへも書けるため検証できない")
		base, listed, out := fixture(t, map[string]string{"modules/a/main.tf": moduleTF}, nil)
		dir := filepath.Dir(out)
		require.NoError(t, os.MkdirAll(dir, 0o750))
		require.NoError(t, os.Chmod(dir, 0o500))
		t.Cleanup(func() { _ = os.Chmod(dir, 0o750) })
		err := run([]string{"-base", base, "-listed", listed, "-out", out, "modules"}, &bytes.Buffer{})
		require.ErrorIs(t, err, fs.ErrPermission)
		assert.NoFileExists(t, out)
	})
}

func Test_run_syntax(t *testing.T) {
	t.Parallel()

	t.Run("-format syntax は全ファイルと構文の木を書き、件数を報告する", func(t *testing.T) {
		t.Parallel()
		files := map[string]string{
			"modules/a/main.tf":             "terraform {\n  cloud {}\n}\n",
			"modules/a/tests/x.tftest.hcl":  "run \"ok\" {}\n",
			"modules/a/README.md":           "# a\n",
			"modules/a/o.tf.json":           "{}",
			"modules/a/.terraform/m/bad.tf": "resource {",
		}
		base, listed, out := fixture(t, files, []string{"modules/a/README.md", "modules/a/main.tf", "modules/a/o.tf.json", "modules/a/tests/x.tftest.hcl"})
		require.NoError(t, os.MkdirAll(filepath.Dir(out), 0o750))
		var stdout bytes.Buffer
		require.NoError(t, run([]string{"-format", "syntax", "-base", base, "-listed", listed, "-out", out, "modules"}, &stdout))
		raw, err := os.ReadFile(out)
		require.NoError(t, err)
		assert.JSONEq(t, `{
			"files": [
				{"path": "modules/a/README.md", "parsed": false},
				{"path": "modules/a/main.tf", "parsed": true},
				{"path": "modules/a/o.tf.json", "parsed": false},
				{"path": "modules/a/tests/x.tftest.hcl", "parsed": true}
			],
			"blocks": [
				{"file": "modules/a/main.tf", "line": 1, "type": "terraform", "labels": [],
					"blocks": [{"path": "cloud", "parent": -1, "line": 2, "type": "cloud", "labels": [], "dynamic": false}], "attributes": []},
				{"file": "modules/a/tests/x.tftest.hcl", "line": 1, "type": "run", "labels": ["ok"], "blocks": [], "attributes": []}
			]
		}`, string(raw))
		assert.Equal(t, "✅ hcl-input: 4 件のファイル（構文として 2 件）から 2 個の block を "+out+" へ書きました\n", stdout.String())
	})

	t.Run("-format syntax の失敗は出力を書かない", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			files  map[string]string
			listed []string
			want   error
		}{
			"構文として読むファイルが0件": {files: map[string]string{"modules/a/README.md": ""}, listed: []string{"modules/a/README.md"}, want: errNoInput},
			"構文の誤り":          {files: map[string]string{"modules/a/t.tftest.hcl": "run {"}, listed: []string{"modules/a/t.tftest.hcl"}, want: errSyntax},
			"一覧に無いファイル":      {files: map[string]string{"modules/a/main.tf": "", "modules/a/x.md": ""}, listed: []string{"modules/a/main.tf"}, want: errFileSetMismatch},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				base, listed, out := fixture(t, tt.files, tt.listed)
				require.NoError(t, os.MkdirAll(filepath.Dir(out), 0o750))
				err := run([]string{"-format", "syntax", "-base", base, "-listed", listed, "-out", out, "modules"}, &bytes.Buffer{})
				require.ErrorIs(t, err, tt.want)
				assert.NoFileExists(t, out)
			})
		}
	})
}

func Test_runSyntax(t *testing.T) {
	t.Parallel()

	t.Run("一覧を読めなければ errListed", func(t *testing.T) {
		t.Parallel()
		base := writeTree(t, map[string]string{"modules/a/main.tf": ""})
		out := filepath.Join(t.TempDir(), "o.json")
		require.ErrorIs(t, runSyntax(base, filepath.Join(base, "none.txt"), out, []string{"modules"}, &bytes.Buffer{}), errListed)
		assert.NoFileExists(t, out)
	})

	t.Run("書き込めなければ失敗する", func(t *testing.T) {
		t.Parallel()
		testenv.RequireNonRoot(t, "特権実行では読み取り専用ディレクトリへも書けるため検証できない")
		base, listed, out := fixture(t, map[string]string{"modules/a/main.tf": ""}, nil)
		dir := filepath.Dir(out)
		require.NoError(t, os.MkdirAll(dir, 0o750))
		require.NoError(t, os.Chmod(dir, 0o500))
		t.Cleanup(func() { _ = os.Chmod(dir, 0o750) })
		require.ErrorIs(t, runSyntax(base, listed, out, []string{"modules"}, &bytes.Buffer{}), fs.ErrPermission)
	})
}

func Test_buildSyntax(t *testing.T) {
	t.Parallel()

	t.Run("symlink は一覧との突き合わせより先に落とす", func(t *testing.T) {
		t.Parallel()
		base := writeTree(t, map[string]string{"modules/a/main.tf": ""})
		require.NoError(t, os.Symlink("main.tf", filepath.Join(base, "modules/a/link.md")))
		_, err := buildSyntax(base, []string{"modules"}, []string{"modules/a/link.md", "modules/a/main.tf"})
		require.ErrorIs(t, err, errSymlink)
	})

	t.Run("構成ファイルを読めなければ失敗する", func(t *testing.T) {
		t.Parallel()
		testenv.RequireNonRoot(t, "特権実行では権限の無いファイルも読めるため検証できない")
		base := writeTree(t, map[string]string{"modules/a/main.tf": ""})
		require.NoError(t, os.Chmod(filepath.Join(base, "modules/a/main.tf"), 0o000))
		_, err := buildSyntax(base, []string{"modules"}, []string{"modules/a/main.tf"})
		require.ErrorIs(t, err, fs.ErrPermission)
	})

	t.Run("空のファイルは block 0個で、0件ではない", func(t *testing.T) {
		t.Parallel()
		base := writeTree(t, map[string]string{"modules/a/main.tf": ""})
		doc, err := buildSyntax(base, []string{"modules"}, []string{"modules/a/main.tf"})
		require.NoError(t, err)
		assert.Equal(t, []fileEntry{{Path: "modules/a/main.tf", Parsed: true}}, doc.Files)
		assert.Empty(t, doc.Blocks)
	})
}

func Test_countParsed(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 0, countParsed(nil))
	assert.Equal(t, 1, countParsed([]fileEntry{{Path: "a.tf", Parsed: true}, {Path: "a.md"}}))
}

func Test_exitCode(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		err  error
		want int
	}{
		"エラーが無い":        {want: 0},
		"構成ファイルが0件":     {err: xerrors.Wrapf(errNoInput, "x"), want: 2},
		"一覧と読んだファイルが違う": {err: xerrors.Wrapf(errFileSetMismatch, "x"), want: 1},
		"構文の誤り":         {err: xerrors.Wrapf(errSyntax, "x"), want: 1},
		"引数の解釈の失敗":      {err: xerrors.New("x"), want: 1},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, exitCode(tt.err))
		})
	}
}
