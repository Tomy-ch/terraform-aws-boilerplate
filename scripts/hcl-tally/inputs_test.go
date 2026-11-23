package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/testenv"
)

func readJSON(t *testing.T, p string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(p)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))

	return got
}

func readText(t *testing.T, p string) string {
	t.Helper()
	raw, err := os.ReadFile(p)
	require.NoError(t, err)

	return string(raw)
}

func Test_runInputs(t *testing.T) {
	t.Parallel()

	tree := map[string]string{
		"usecases/a/main.tf":              "terraform {\n  required_version = \">= 1\"\n}\n\nmodule \"x\" {\n  source = \"../../modules/m\"\n}\nmodule y {\n}\n",
		"usecases/a/README.md":            "# a\n",
		"usecases/a/tests/a.tftest.hcl":   "run \"plan\" {\n  command = plan\n}\n",
		"usecases/a/extra.tf.json":        "{}\n",
		"usecases/a/.terraform/cached.tf": "resource \"x\" \"y\" {}\n",
		"modules/m/main.tf":               "resource \"aws_s3_bucket\" \"b\" {\n  tags = {}\n}\n",
	}

	t.Run("一覧・block の数・構成ファイルの一覧と module の数を書く", func(t *testing.T) {
		t.Parallel()
		base := writeTree(t, tree)
		out := t.TempDir()
		var stdout bytes.Buffer
		require.NoError(t, runInputs([]string{"-base", base, "-out", out, "-files-key", "root_files", "-module-counts-key", "root_module_counts", "usecases", "modules"}, &stdout))

		assert.Equal(t, "modules/m/main.tf\nusecases/a/README.md\nusecases/a/extra.tf.json\nusecases/a/main.tf\nusecases/a/tests/a.tftest.hcl\n",
			readText(t, filepath.Join(out, "found.txt")))
		assert.JSONEq(t, `{
			"hcl_parsed_files": ["modules/m/main.tf", "usecases/a/main.tf", "usecases/a/tests/a.tftest.hcl"],
			"hcl_block_counts": {"modules/m/main.tf": 1, "usecases/a/main.tf": 3, "usecases/a/tests/a.tftest.hcl": 1}
		}`, readText(t, filepath.Join(out, "hcl-counts.json")))
		assert.JSONEq(t, `{
			"root_files": ["modules/m/main.tf", "usecases/a/extra.tf.json", "usecases/a/main.tf"],
			"root_module_counts": {"modules/m/main.tf": 0, "usecases/a/main.tf": 2}
		}`, readText(t, filepath.Join(out, "files.json")))
		assert.Equal(t, "✅ hcl-tally inputs: ファイル 5 件（構成ファイル 3 件、構文として読むもの 3 件）を "+out+" へ数えました\n", stdout.String())
	})

	t.Run("-files-key が無ければ files.json を書かない", func(t *testing.T) {
		t.Parallel()
		base := writeTree(t, tree)
		out := t.TempDir()
		require.NoError(t, runInputs([]string{"-base", base, "-out", out, "usecases", "modules"}, &bytes.Buffer{}))
		assert.NoFileExists(t, filepath.Join(out, "files.json"))
		assert.FileExists(t, filepath.Join(out, "hcl-counts.json"))
	})

	t.Run("-module-counts-key が無ければ files.json は構成ファイルの一覧だけを持つ", func(t *testing.T) {
		t.Parallel()
		base := writeTree(t, tree)
		out := t.TempDir()
		require.NoError(t, runInputs([]string{"-base", base, "-out", out, "-files-key", "layer_files", "modules"}, &bytes.Buffer{}))
		assert.JSONEq(t, `{"layer_files": ["modules/m/main.tf"]}`, readText(t, filepath.Join(out, "files.json")))
	})

	t.Run("引用符と逆斜線を持つファイル名を JSON の文字列として書く", func(t *testing.T) {
		t.Parallel()
		base := writeTree(t, map[string]string{`m/a"b\c.tf`: "locals {}\n"})
		out := t.TempDir()
		require.NoError(t, runInputs([]string{"-base", base, "-out", out, "-files-key", "layer_files", "m"}, &bytes.Buffer{}))
		assert.Equal(t, map[string]any{"layer_files": []any{`m/a"b\c.tf`}}, readJSON(t, filepath.Join(out, "files.json")))
		assert.Equal(t, map[string]any{`m/a"b\c.tf`: float64(1)}, readJSON(t, filepath.Join(out, "hcl-counts.json"))["hcl_block_counts"])
	})

	t.Run("symlink は列挙しない", func(t *testing.T) {
		t.Parallel()
		base := writeTree(t, map[string]string{"m/main.tf": ""})
		require.NoError(t, os.Symlink("main.tf", filepath.Join(base, "m/link.tf")))
		out := t.TempDir()
		require.NoError(t, runInputs([]string{"-base", base, "-out", out, "m"}, &bytes.Buffer{}))
		assert.Equal(t, "m/main.tf\n", readText(t, filepath.Join(out, "found.txt")))
	})

	t.Run("失敗は何も書かない", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			files map[string]string
			roots []string
			want  error
		}{
			"root の1つが無い":     {files: map[string]string{"m/main.tf": ""}, roots: []string{"m", "env"}, want: errMissingRoot},
			"root がファイル":      {files: map[string]string{"m/main.tf": "", "env": ""}, roots: []string{"m", "env"}, want: errMissingRoot},
			".tf が0件":         {files: map[string]string{"m/a.tf.json": "{}", "m/t.tftest.hcl": ""}, roots: []string{"m"}, want: errNoInput},
			".terraform の下だけ": {files: map[string]string{"m/.terraform/a.tf": ""}, roots: []string{"m"}, want: errNoInput},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				base := writeTree(t, tt.files)
				out := t.TempDir()
				args := append([]string{"-base", base, "-out", out, "-files-key", "k"}, tt.roots...)
				require.ErrorIs(t, runInputs(args, &bytes.Buffer{}), tt.want)
				entries, err := os.ReadDir(out)
				require.NoError(t, err)
				assert.Empty(t, entries)
			})
		}
	})

	t.Run("書き込めなければ失敗し、出力を残さない", func(t *testing.T) {
		t.Parallel()
		testenv.RequireNonRoot(t, "特権実行では読み取り専用ディレクトリへも書けるため検証できない")
		base := writeTree(t, map[string]string{"m/main.tf": ""})
		out := t.TempDir()
		require.NoError(t, os.Chmod(out, 0o500))
		t.Cleanup(func() { _ = os.Chmod(out, 0o750) })
		require.Error(t, runInputs([]string{"-base", base, "-out", out, "m"}, &bytes.Buffer{}))
		assert.NoFileExists(t, filepath.Join(out, "found.txt"))
	})

	t.Run("引数の誤りは errUsage", func(t *testing.T) {
		t.Parallel()
		for name, args := range map[string][]string{
			"-base が無い":             {"-out", "o", "m"},
			"-out が無い":              {"-base", ".", "m"},
			"root が無い":              {"-base", ".", "-out", "o"},
			"-module-counts-key だけ": {"-base", ".", "-out", "o", "-module-counts-key", "k", "m"},
			"キーの形が違う":               {"-base", ".", "-out", "o", "-files-key", "A-b", "m"},
			"2つのキーが同じ":              {"-base", ".", "-out", "o", "-files-key", "k", "-module-counts-key", "k", "m"},
			"知らないフラグ":               {"-x"},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				require.ErrorIs(t, runInputs(args, &bytes.Buffer{}), errUsage)
			})
		}
	})
}

func Test_listFiles(t *testing.T) {
	t.Parallel()

	t.Run("複数の root の通常のファイルを辞書順に返し、.terraform に降りない", func(t *testing.T) {
		t.Parallel()
		base := writeTree(t, map[string]string{"b/x.tf": "", "a/.terraform/y.tf": "", "a/z/w.md": ""})
		got, err := listFiles(base, []string{"b", "a"})
		require.NoError(t, err)
		assert.Equal(t, []string{"a/z/w.md", "b/x.tf"}, got)
	})

	t.Run("同じ root を2度渡しても重ねない", func(t *testing.T) {
		t.Parallel()
		base := writeTree(t, map[string]string{"a/x.tf": ""})
		got, err := listFiles(base, []string{"a", "a"})
		require.NoError(t, err)
		assert.Equal(t, []string{"a/x.tf"}, got)
	})

	t.Run("読めないディレクトリは errWalk", func(t *testing.T) {
		t.Parallel()
		testenv.RequireNonRoot(t, "特権実行では読めないディレクトリも読めるため検証できない")
		base := writeTree(t, map[string]string{"a/sub/x.tf": ""})
		sub := filepath.Join(base, "a/sub")
		require.NoError(t, os.Chmod(sub, 0o000))
		t.Cleanup(func() { _ = os.Chmod(sub, 0o750) })
		_, err := listFiles(base, []string{"a"})
		require.ErrorIs(t, err, errWalk)
	})
}

func Test_countLines(t *testing.T) {
	t.Parallel()

	t.Run("トップレベルの block の行頭だけを数える", func(t *testing.T) {
		t.Parallel()
		src := "resource \"a\" \"b\" {\n  nested {\n  }\n}\nlocals {\n}\nmoved{\n}\n# resource \"c\" \"d\" {\nx = 1\n"
		p := filepath.Join(writeTree(t, map[string]string{"a.tf": src}), "a.tf")
		got, err := countLines(p, blockLine)
		require.NoError(t, err)
		assert.Equal(t, 3, got)
	})

	t.Run("module の行頭は字下げと引用符の有無を問わない", func(t *testing.T) {
		t.Parallel()
		src := "module \"a\" {}\n  module b {\n}\nmodules = 1\nmodule = 2\n"
		p := filepath.Join(writeTree(t, map[string]string{"a.tf": src}), "a.tf")
		got, err := countLines(p, moduleLine)
		require.NoError(t, err)
		assert.Equal(t, 2, got)
	})

	t.Run("読めなければ errWalk", func(t *testing.T) {
		t.Parallel()
		_, err := countLines(filepath.Join(t.TempDir(), "missing.tf"), regexp.MustCompile(`x`))
		require.ErrorIs(t, err, errWalk)
	})
}

func Test_joinLines(t *testing.T) {
	t.Parallel()

	t.Run("行ごとに改行で終える", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "a\nb\n", joinLines([]string{"a", "b"}))
	})

	t.Run("空なら空の文字列", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, joinLines(nil))
	})
}

func Test_marshal(t *testing.T) {
	t.Parallel()

	t.Run("字下げした JSON を改行で終える", func(t *testing.T) {
		t.Parallel()
		got, err := marshal(map[string]int{"a": 1})
		require.NoError(t, err)
		assert.Equal(t, "{\n  \"a\": 1\n}\n", got)
	})

	t.Run("JSON にできない値は失敗", func(t *testing.T) {
		t.Parallel()
		_, err := marshal(func() {})
		require.Error(t, err)
	})
}
