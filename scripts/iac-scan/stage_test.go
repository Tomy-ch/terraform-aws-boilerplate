package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/testenv"
)

func Test_parseScan(t *testing.T) {
	t.Parallel()

	t.Run("-base と root を読み、固有の flag を足せる", func(t *testing.T) {
		t.Parallel()
		var extra string
		s, err := parseScan("x", []string{"-base", "b", "-extra", "v", "usecases", "env"}, &bytes.Buffer{}, func(f *flag.FlagSet) {
			f.StringVar(&extra, "extra", "", "")
		})
		require.NoError(t, err)
		assert.Equal(t, scanArgs{base: "b", roots: []string{"usecases", "env"}}, s)
		assert.Equal(t, "v", extra)
	})

	t.Run("-base の既定は .", func(t *testing.T) {
		t.Parallel()
		s, err := parseScan("x", []string{"modules"}, &bytes.Buffer{}, nil)
		require.NoError(t, err)
		assert.Equal(t, ".", s.base)
	})

	t.Run("root が無ければ errUsage", func(t *testing.T) {
		t.Parallel()
		_, err := parseScan("x", []string{"-base", "b"}, &bytes.Buffer{}, nil)
		require.ErrorIs(t, err, errUsage)
	})

	t.Run("知らない flag は errUsage", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		_, err := parseScan("x", []string{"-nope", "env"}, &out, nil)
		require.ErrorIs(t, err, errUsage)
		assert.Contains(t, out.String(), "-nope")
	})
}

func Test_readResult(t *testing.T) {
	t.Parallel()

	t.Run("通常のファイルを読む", func(t *testing.T) {
		t.Parallel()
		dir := tree(t, map[string]string{"a.json": "{}"})
		got, err := readResult(filepath.Join(dir, "a.json"))
		require.NoError(t, err)
		assert.Equal(t, "{}", string(got))
	})

	t.Run("無いファイルは errResult", func(t *testing.T) {
		t.Parallel()
		_, err := readResult(filepath.Join(t.TempDir(), "none.json"))
		require.ErrorIs(t, err, errResult)
	})

	t.Run("symlink は辿らずに errResult", func(t *testing.T) {
		t.Parallel()
		dir := tree(t, map[string]string{"real.json": "{}"})
		require.NoError(t, os.Symlink("real.json", filepath.Join(dir, "a.json")))
		_, err := readResult(filepath.Join(dir, "a.json"))
		require.ErrorIs(t, err, errResult)
		assert.Contains(t, err.Error(), "通常のファイルではありません")
	})

	t.Run("ディレクトリは errResult", func(t *testing.T) {
		t.Parallel()
		_, err := readResult(t.TempDir())
		require.ErrorIs(t, err, errResult)
	})
}

func Test_readJSON(t *testing.T) {
	t.Parallel()

	t.Run("数を json.Number のまま読む", func(t *testing.T) {
		t.Parallel()
		dir := tree(t, map[string]string{"a.json": `{"n": 12345678901234567890}`})
		v, err := readJSON(filepath.Join(dir, "a.json"))
		require.NoError(t, err)
		assert.Equal(t, json.Number("12345678901234567890"), v.(map[string]any)["n"])
	})

	t.Run("無いファイルは errResult", func(t *testing.T) {
		t.Parallel()
		_, err := readJSON(filepath.Join(t.TempDir(), "none.json"))
		require.ErrorIs(t, err, errResult)
	})

	t.Run("JSON でなければ errResult", func(t *testing.T) {
		t.Parallel()
		dir := tree(t, map[string]string{"a.json": "not json"})
		_, err := readJSON(filepath.Join(dir, "a.json"))
		require.ErrorIs(t, err, errResult)
	})

	t.Run("空のファイルは errResult", func(t *testing.T) {
		t.Parallel()
		dir := tree(t, map[string]string{"a.json": ""})
		_, err := readJSON(filepath.Join(dir, "a.json"))
		require.ErrorIs(t, err, errResult)
	})

	t.Run("symlink は辿らずに errResult", func(t *testing.T) {
		t.Parallel()
		dir := tree(t, map[string]string{"real.json": "{}"})
		require.NoError(t, os.Symlink("real.json", filepath.Join(dir, "a.json")))
		_, err := readJSON(filepath.Join(dir, "a.json"))
		require.ErrorIs(t, err, errResult)
	})
}

func Test_intField(t *testing.T) {
	t.Parallel()

	t.Run("整数を返す", func(t *testing.T) {
		t.Parallel()
		got, err := intField(map[string]any{"n": json.Number("3")}, "n")
		require.NoError(t, err)
		assert.Equal(t, int64(3), got)
	})

	for name, v := range map[string]any{
		"欄が無い": nil,
		"文字列":  "3",
		"小数":   json.Number("1.5"),
		"真偽値":  true,
		"null": nil,
	} {
		t.Run(name+"は errResult", func(t *testing.T) {
			t.Parallel()
			m := map[string]any{}
			if name != "欄が無い" {
				m["n"] = v
			}
			_, err := intField(m, "n")
			require.ErrorIs(t, err, errResult)
		})
	}
}

func Test_checkRoots(t *testing.T) {
	t.Parallel()

	t.Run("root を / 区切りに整えて返す", func(t *testing.T) {
		t.Parallel()
		base := tfTree(t)
		got, err := checkRoots(base, []string{"usecases/", "modules/b/../b", "env"})
		require.NoError(t, err)
		assert.Equal(t, []string{"usecases", "modules/b", "env"}, got)
	})

	t.Run("前置きだけが一致する root は重ならない", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"env/a.tf": "", "envx/a.tf": ""})
		got, err := checkRoots(base, []string{"env", "envx"})
		require.NoError(t, err)
		assert.Equal(t, []string{"env", "envx"}, got)
	})

	for name, roots := range map[string][]string{
		"空":             {""},
		"絶対パス":          {"/env"},
		"親へ出る":          {"../env"},
		"途中で親へ出る":       {"env/../../x"},
		"base 自身":       {"."},
		"戻って base 自身":   {"env/.."},
		"同じ root":       {"env", "env"},
		"同じ root の別の綴り": {"env", "env/"},
		"先の root が後を含む": {"usecases", "usecases/a"},
		"後の root が先を含む": {"usecases/a", "usecases"},
	} {
		t.Run(name+"は errUsage", func(t *testing.T) {
			t.Parallel()
			_, err := checkRoots(tfTree(t), roots)
			require.ErrorIs(t, err, errUsage)
		})
	}

	t.Run("無い root は errMissingRoot", func(t *testing.T) {
		t.Parallel()
		_, err := checkRoots(tfTree(t), []string{"env", "nothing"})
		require.ErrorIs(t, err, errMissingRoot)
	})

	t.Run("ファイルの root は errMissingRoot", func(t *testing.T) {
		t.Parallel()
		_, err := checkRoots(tree(t, map[string]string{"env": ""}), []string{"env"})
		require.ErrorIs(t, err, errMissingRoot)
	})
}

func Test_stageTerraform(t *testing.T) {
	t.Parallel()

	t.Run("root の下を同じ相対パスで写し、.terraform だけを飛ばす", func(t *testing.T) {
		t.Parallel()
		base := tfTree(t)
		dst := filepath.Join(t.TempDir(), "src")
		staged, err := stageTerraform(base, []string{"usecases", "modules", "env"}, dst)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"usecases/a/main.tf", "usecases/a/.terraform.lock.hcl", "modules/b/main.tf", "env/c/main.tf"}, staged)
		assert.FileExists(t, filepath.Join(dst, "usecases", "a", ".terraform.lock.hcl"))
		assert.NoDirExists(t, filepath.Join(dst, "usecases", "a", ".terraform"))
	})

	t.Run("中身を写す", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"env/a/main.tf": "resource \"x\" \"y\" {}\n"})
		dst := t.TempDir()
		_, err := stageTerraform(base, []string{"env"}, dst)
		require.NoError(t, err)
		got, err := os.ReadFile(filepath.Join(dst, "env", "a", "main.tf"))
		require.NoError(t, err)
		assert.Equal(t, "resource \"x\" \"y\" {}\n", string(got))
	})

	t.Run("名前が .terraform のファイルも飛ばす", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"env/a/main.tf": "", "env/a/.terraform": ""})
		staged, err := stageTerraform(base, []string{"env"}, t.TempDir())
		require.NoError(t, err)
		assert.Equal(t, []string{"env/a/main.tf"}, staged)
	})

	t.Run("宣言した root が1つでも無ければ errMissingRoot", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"env/a/main.tf": ""})
		_, err := stageTerraform(base, []string{"env", "modules"}, t.TempDir())
		require.ErrorIs(t, err, errMissingRoot)
	})

	t.Run("root がファイルなら errMissingRoot", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"env": ""})
		_, err := stageTerraform(base, []string{"env"}, t.TempDir())
		require.ErrorIs(t, err, errMissingRoot)
	})

	t.Run("写すファイルが0件なら errNoTargets", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"env/a/.terraform/x.tf": ""})
		_, err := stageTerraform(base, []string{"env"}, t.TempDir())
		require.ErrorIs(t, err, errNoTargets)
	})

	t.Run("symlink は errInput", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"env/a/main.tf": ""})
		require.NoError(t, os.Symlink("main.tf", filepath.Join(base, "env", "a", "link.tf")))
		_, err := stageTerraform(base, []string{"env"}, t.TempDir())
		require.ErrorIs(t, err, errInput)
		assert.Contains(t, err.Error(), "link.tf")
	})

	t.Run("読めないファイルは errInput", func(t *testing.T) {
		t.Parallel()
		testenv.RequireNonRoot(t, "権限を落として読めないファイルを作る")
		base := tree(t, map[string]string{"env/a/main.tf": "", "env/a/secret.tf": ""})
		require.NoError(t, os.Chmod(filepath.Join(base, "env", "a", "secret.tf"), 0))
		_, err := stageTerraform(base, []string{"env"}, t.TempDir())
		require.ErrorIs(t, err, errInput)
	})

	for name, root := range map[string]string{"空": "", "絶対パス": "/env", "親へ出る": "../env", "途中で親へ出る": "env/../../x", "base 自身": "."} {
		t.Run(name+"の root は errUsage", func(t *testing.T) {
			t.Parallel()
			base := tree(t, map[string]string{"env/a/main.tf": ""})
			_, err := stageTerraform(base, []string{root}, t.TempDir())
			require.ErrorIs(t, err, errUsage)
		})
	}
}

func Test_copyFile(t *testing.T) {
	t.Parallel()

	t.Run("中身を写す", func(t *testing.T) {
		t.Parallel()
		dir := tree(t, map[string]string{"a": "body"})
		require.NoError(t, copyFile(filepath.Join(dir, "a"), filepath.Join(dir, "b")))
		got, err := os.ReadFile(filepath.Join(dir, "b"))
		require.NoError(t, err)
		assert.Equal(t, "body", string(got))
	})

	t.Run("無い元は errInput", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.ErrorIs(t, copyFile(filepath.Join(dir, "a"), filepath.Join(dir, "b")), errInput)
		assert.NoFileExists(t, filepath.Join(dir, "b"))
	})

	t.Run("書けない先は errInput", func(t *testing.T) {
		t.Parallel()
		dir := tree(t, map[string]string{"a": "body"})
		require.ErrorIs(t, copyFile(filepath.Join(dir, "a"), filepath.Join(dir, "no", "b")), errInput)
	})
}

func Test_countSuffix(t *testing.T) {
	t.Parallel()

	paths := []string{"a/main.tf", "a/.terraform.lock.hcl", "a/t.tftest.hcl", "a/README.md", "a/x.tf.json"}
	assert.Equal(t, 1, countSuffix(paths, ".tf"))
	assert.Equal(t, 3, countSuffix(paths, ".tf", ".hcl"))
	assert.Equal(t, 0, countSuffix(nil, ".tf"))
}
