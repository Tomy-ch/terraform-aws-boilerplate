package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/testenv"
)

func Test_runFixtures(t *testing.T) {
	t.Parallel()

	t.Run("txtar の1ファイルを1ケースとして読み、ケース名で引ける JSON を書く", func(t *testing.T) {
		t.Parallel()
		dir := writeTree(t, map[string]string{
			"b_case.txtar": "-- m/main.tf --\nresource \"aws_s3_bucket\" \"b\" {}\n-- m/README.md --\n# m\n",
			"a_case.txtar": "comment\n-- env/x/main.tf --\n",
			"note.md":      "txtar ではない",
		})
		out := filepath.Join(t.TempDir(), "fx.json")
		var stdout bytes.Buffer
		require.NoError(t, run([]string{"fixtures", "-out", out, dir}, &stdout))
		raw, err := os.ReadFile(out)
		require.NoError(t, err)
		assert.JSONEq(t, `{"fixtures": {
			"a_case": {"files": [{"path": "env/x/main.tf", "parsed": true}], "blocks": []},
			"b_case": {"files": [{"path": "m/README.md", "parsed": false}, {"path": "m/main.tf", "parsed": true}],
				"blocks": [{"file": "m/main.tf", "line": 1, "type": "resource", "labels": ["aws_s3_bucket", "b"], "blocks": [], "attributes": []}]}
		}}`, string(raw))
		assert.Equal(t, "✅ hcl-input fixtures: 2 件のケースを "+out+" へ書きました\n", stdout.String())
	})

	t.Run("複数のディレクトリのケースを1つの JSON にまとめる", func(t *testing.T) {
		t.Parallel()
		lib := writeTree(t, map[string]string{"lib_case.txtar": "-- m/main.tf --\n"})
		tree := writeTree(t, map[string]string{"tree_case.txtar": "-- env/x/main.tf --\n"})
		out := filepath.Join(t.TempDir(), "fx.json")
		var stdout bytes.Buffer
		require.NoError(t, run([]string{"fixtures", "-out", out, lib, tree}, &stdout))
		raw, err := os.ReadFile(out)
		require.NoError(t, err)
		assert.JSONEq(t, `{"fixtures": {
			"lib_case": {"files": [{"path": "m/main.tf", "parsed": true}], "blocks": []},
			"tree_case": {"files": [{"path": "env/x/main.tf", "parsed": true}], "blocks": []}
		}}`, string(raw))
		assert.Equal(t, "✅ hcl-input fixtures: 2 件のケースを "+out+" へ書きました\n", stdout.String())
	})

	t.Run("ディレクトリをまたいでケースの名前が重なれば errFixture で、出力を書かない", func(t *testing.T) {
		t.Parallel()
		a := writeTree(t, map[string]string{"same.txtar": "-- m/main.tf --\n"})
		b := writeTree(t, map[string]string{"same.txtar": "-- env/x/main.tf --\n"})
		out := filepath.Join(t.TempDir(), "fx.json")
		err := run([]string{"fixtures", "-out", out, a, b}, &bytes.Buffer{})
		require.ErrorIs(t, err, errFixture)
		assert.Contains(t, err.Error(), `"same"`)
		assert.NoFileExists(t, out)
	})

	t.Run("2つ目のディレクトリの txtar が0件なら errFixture で、出力を書かない", func(t *testing.T) {
		t.Parallel()
		a := writeTree(t, map[string]string{"a.txtar": "-- m/main.tf --\n"})
		empty := writeTree(t, map[string]string{"x.md": ""})
		out := filepath.Join(t.TempDir(), "fx.json")
		require.ErrorIs(t, run([]string{"fixtures", "-out", out, a, empty}, &bytes.Buffer{}), errFixture)
		assert.NoFileExists(t, out)
	})

	t.Run("失敗は出力を書かない", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			files map[string]string
			want  error
		}{
			"txtar が0件":    {files: map[string]string{"x.md": ""}, want: errFixture},
			"ファイルの無いケース":   {files: map[string]string{"a.txtar": "comment only\n"}, want: errFixture},
			"構文の誤りのケース":    {files: map[string]string{"a.txtar": "-- m/a.tf --\nresource {\n"}, want: errSyntax},
			"構文として読むものが無い": {files: map[string]string{"a.txtar": "-- m/a.md --\n"}, want: errNoInput},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				dir := writeTree(t, tt.files)
				out := filepath.Join(t.TempDir(), "fx.json")
				require.ErrorIs(t, run([]string{"fixtures", "-out", out, dir}, &bytes.Buffer{}), tt.want)
				assert.NoFileExists(t, out)
			})
		}
	})

	t.Run("書き込めなければ失敗し、出力を残さない", func(t *testing.T) {
		t.Parallel()
		testenv.RequireNonRoot(t, "特権実行では読み取り専用ディレクトリへも書けるため検証できない")
		dir := writeTree(t, map[string]string{"a.txtar": "-- m/a.tf --\n"})
		outDir := t.TempDir()
		require.NoError(t, os.Chmod(outDir, 0o500))
		t.Cleanup(func() { _ = os.Chmod(outDir, 0o750) })
		out := filepath.Join(outDir, "fx.json")
		require.ErrorIs(t, run([]string{"fixtures", "-out", out, dir}, &bytes.Buffer{}), fs.ErrPermission)
		assert.NoFileExists(t, out)
	})

	t.Run("引数が足りなければ errUsage", func(t *testing.T) {
		t.Parallel()
		for name, args := range map[string][]string{
			"ディレクトリが無い": {"-out", "o.json"},
			"-out が無い":  {"."},
			"知らないフラグ":   {"-x"},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				require.ErrorIs(t, runFixtures(args, &bytes.Buffer{}), errUsage)
			})
		}
	})
}

func Test_readCase(t *testing.T) {
	t.Parallel()

	t.Run("名前から中身への対応を返す", func(t *testing.T) {
		t.Parallel()
		dir := writeTree(t, map[string]string{"a.txtar": "-- m/a.tf --\nx\n-- m/b/c.md --\n"})
		got, err := readCase(filepath.Join(dir, "a.txtar"))
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"m/a.tf": "x\n", "m/b/c.md": ""}, got)
	})

	t.Run("読めない、または木の中の名前でないものは errFixture", func(t *testing.T) {
		t.Parallel()

		tests := map[string]string{
			"ファイルが無い":       "comment\n",
			"絶対パス":          "-- /m/a.tf --\n",
			"上へ出る":          "-- ../a.tf --\n",
			"途中で上へ出る":       "-- m/../../a.tf --\n",
			"正規化されていない":     "-- m//a.tf --\n",
			"ディレクトリを持たない名前": "-- a.tf --\n",
			"同じ名前が2つ":       "-- m/a.tf --\n-- m/a.tf --\n",
		}
		for name, src := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				dir := writeTree(t, map[string]string{"a.txtar": src})
				_, err := readCase(filepath.Join(dir, "a.txtar"))
				require.ErrorIs(t, err, errFixture)
			})
		}
		_, err := readCase(filepath.Join(t.TempDir(), "missing.txtar"))
		require.ErrorIs(t, err, errFixture)
	})
}

func Test_buildCase(t *testing.T) {
	t.Parallel()

	t.Run("最初の段を走査の root にして読む", func(t *testing.T) {
		t.Parallel()
		got, err := buildCase(map[string]string{"usecases/b/main.tf": "", "env/x/main.tf": ""})
		require.NoError(t, err)
		assert.Equal(t, []fileEntry{{Path: "env/x/main.tf", Parsed: true}, {Path: "usecases/b/main.tf", Parsed: true}}, got.Files)
	})

	t.Run("構文として読むものが無ければ errNoInput", func(t *testing.T) {
		t.Parallel()
		_, err := buildCase(map[string]string{"m/a.md": ""})
		require.ErrorIs(t, err, errNoInput)
	})

	// 名前は辞書順に置くので、ファイルの名前が同じ名前のディレクトリの中の名前より先に来る。逆の順（ディレクトリが
	// 先）は辞書順では起きない。
	t.Run("ファイルとディレクトリの名前が衝突すれば errFixture", func(t *testing.T) {
		t.Parallel()
		_, err := buildCase(map[string]string{"m/a/b.tf": "", "m/a": ""})
		require.ErrorIs(t, err, errFixture)
	})

	// 辞書順なら m/a を先に置き、次の m/a/b.tf で必ず落ちる。順が map の順なら m/a/c.tf が先になる回があり、
	// そのときは m/a を置けずに落ちる。
	t.Run("置く順は名前の辞書順で決まる", func(t *testing.T) {
		t.Parallel()
		for range 20 {
			_, err := buildCase(map[string]string{"m/a": "", "m/a/b.tf": "", "m/a/c.tf": ""})
			require.ErrorIs(t, err, errFixture)
			assert.Contains(t, err.Error(), "m/a/b.tf を置けません")
		}
	})
}
