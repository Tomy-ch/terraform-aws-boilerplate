package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/testenv"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/tflock"
)

func Test_requiredProviders(t *testing.T) {
	t.Parallel()

	t.Run("ディレクトリの .tf の required_providers を、重複を除いて辞書順で返す", func(t *testing.T) {
		t.Parallel()
		dir := tree(t, map[string]string{
			"versions.tf":  awsRequired,
			"other.tf":     "terraform {\n  required_providers {\n    aws = { source = \"HashiCorp/AWS\" }\n    r = { source = \"example.com/x/random\" }\n  }\n}\n",
			"main.tf":      "resource \"aws_s3_bucket\" \"b\" {}\n",
			"x.tftest.hcl": "run \"x\" {}\n",
		})
		got, err := requiredProviders(dir)
		require.NoError(t, err)
		assert.Equal(t, []string{"example.com/x/random", "registry.terraform.io/hashicorp/aws"}, got)
	})

	t.Run("required_providers が無ければ空", func(t *testing.T) {
		t.Parallel()
		got, err := requiredProviders(tree(t, map[string]string{"main.tf": "terraform {\n  required_version = \">= 1.0\"\n}\n"}))
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run(".tf が1つも無ければ空", func(t *testing.T) {
		t.Parallel()
		got, err := requiredProviders(tree(t, map[string]string{"README.md": ""}))
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run(".tf で終わるディレクトリは読まない", func(t *testing.T) {
		t.Parallel()
		got, err := requiredProviders(tree(t, map[string]string{"x.tf/versions.tf": awsRequired}))
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("複数の .tf と terraform block の要求を集め、同じ provider は1件に畳む", func(t *testing.T) {
		t.Parallel()
		got, err := requiredProviders(tree(t, map[string]string{"a.tf": awsRequired + awsRequired, "b.tf": awsRequired}))
		require.NoError(t, err)
		assert.Equal(t, []string{"registry.terraform.io/hashicorp/aws"}, got)
	})

	t.Run(".tf.json があれば errProviders", func(t *testing.T) {
		t.Parallel()
		_, err := requiredProviders(tree(t, map[string]string{"main.tf": "", "versions.tf.json": "{}"}))
		require.ErrorIs(t, err, errProviders)
	})

	t.Run("読めない .tf はエラー", func(t *testing.T) {
		t.Parallel()
		testenv.RequireNonRoot(t, "root はファイルの読み取りの権限を無視する")
		dir := tree(t, map[string]string{"versions.tf": awsRequired})
		require.NoError(t, os.Chmod(filepath.Join(dir, "versions.tf"), 0o000))
		_, err := requiredProviders(dir)
		require.Error(t, err)
	})

	t.Run("無いディレクトリはエラー", func(t *testing.T) {
		t.Parallel()
		_, err := requiredProviders(filepath.Join(t.TempDir(), "no-such"))
		require.ErrorIs(t, err, os.ErrNotExist)
	})
}

func Test_requiredIn(t *testing.T) {
	t.Parallel()

	for name, src := range map[string]string{
		"HCL として壊れている":  "terraform {\n",
		"source の無い要求":  "terraform {\n  required_providers {\n    aws = { version = \"6.0.0\" }\n  }\n}\n",
		"文字列だけの古い書き方":   "terraform {\n  required_providers {\n    aws = \"6.0.0\"\n  }\n}\n",
		"評価できない source": "terraform {\n  required_providers {\n    aws = { source = var.x }\n  }\n}\n",
		"数の source":     "terraform {\n  required_providers {\n    aws = { source = 1 }\n  }\n}\n",
	} {
		t.Run(name+"は errProviders", func(t *testing.T) {
			t.Parallel()
			_, err := requiredIn([]byte(src), "x.tf")
			require.ErrorIs(t, err, errProviders)
		})
	}

	t.Run("terraform block の外の required_providers は読まない", func(t *testing.T) {
		t.Parallel()
		got, err := requiredIn([]byte("locals {\n  x = 1\n}\n"), "x.tf")
		require.NoError(t, err)
		assert.Empty(t, got)
	})
}

func Test_providerAddr(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct{ in, want string }{
		"host を省いた形は Terraform Registry の名前にする": {"HashiCorp/aws", "registry.terraform.io/hashicorp/aws"},
		"host を持つ形は小文字にするだけ":                    {"Example.com/x/y", "example.com/x/y"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, providerAddr(c.in))
		})
	}
}

func Test_planLock(t *testing.T) {
	t.Parallel()

	c := canonical{decls: []tflock.Decl{{Name: "aws", Source: "hashicorp/aws", Version: "6.73.0"}}, content: []byte(testLock)}

	t.Run("正本と同じ集合を要求するディレクトリには置くと決める", func(t *testing.T) {
		t.Parallel()
		dir := tree(t, map[string]string{"versions.tf": awsRequired})
		got, err := planLock(dir, c)
		require.NoError(t, err)
		assert.Equal(t, lockPlan{path: filepath.Join(dir, tflock.LockFile), place: true}, got)
		assert.NoFileExists(t, filepath.Join(dir, tflock.LockFile), "決めるだけで書かない")
	})

	t.Run("provider を要求しないディレクトリには置かないと決める", func(t *testing.T) {
		t.Parallel()
		dir := tree(t, map[string]string{"main.tf": "", tflock.LockFile: "stale"})
		got, err := planLock(dir, c)
		require.NoError(t, err)
		assert.False(t, got.place)
		assert.Equal(t, "stale", readFile(t, filepath.Join(dir, tflock.LockFile)), "決めるだけで消さない")
	})

	for name, c2 := range map[string]struct {
		files map[string]string
		c     canonical
	}{
		"正本に無い provider を要求する": {map[string]string{"versions.tf": strings.Replace(awsRequired, "hashicorp/aws", "hashicorp/random", 1)}, c},
		"正本の一部だけを要求する":         {map[string]string{"versions.tf": awsRequired}, canonical{decls: append(append([]tflock.Decl{}, c.decls...), tflock.Decl{Name: "r", Source: "hashicorp/random", Version: "3.0.0"}), content: c.content}},
		"要求を読めない":              {map[string]string{"versions.tf": "terraform {\n"}, c},
	} {
		t.Run(name+"なら errProviders で、既存の lock も変えない", func(t *testing.T) {
			t.Parallel()
			files := map[string]string{tflock.LockFile: "stale"}
			for k, v := range c2.files {
				files[k] = v
			}
			dir := tree(t, files)
			_, err := planLock(dir, c2.c)
			require.ErrorIs(t, err, errProviders)
			assert.Equal(t, "stale", readFile(t, filepath.Join(dir, tflock.LockFile)))
		})
	}
}

func Test_applyLock(t *testing.T) {
	t.Parallel()

	t.Run("置くなら正本の lock を書く", func(t *testing.T) {
		t.Parallel()
		dir := tree(t, map[string]string{tflock.LockFile: "stale"})
		require.NoError(t, applyLock(lockPlan{path: filepath.Join(dir, tflock.LockFile), place: true}, []byte(testLock)))
		assert.Equal(t, testLock, readFile(t, filepath.Join(dir, tflock.LockFile)))
	})

	t.Run("置かないなら前の lock を消し、無くてもよい", func(t *testing.T) {
		t.Parallel()
		dir := tree(t, map[string]string{tflock.LockFile: "stale"})
		p := lockPlan{path: filepath.Join(dir, tflock.LockFile)}
		require.NoError(t, applyLock(p, []byte(testLock)))
		assert.NoFileExists(t, p.path)
		require.NoError(t, applyLock(p, []byte(testLock)))
	})

	t.Run("書けなければエラー", func(t *testing.T) {
		t.Parallel()
		require.Error(t, applyLock(lockPlan{path: filepath.Join(t.TempDir(), "no-such", tflock.LockFile), place: true}, []byte(testLock)))
	})

	t.Run("消せなければエラー", func(t *testing.T) {
		t.Parallel()
		dir := tree(t, map[string]string{tflock.LockFile + "/x": ""})
		require.Error(t, applyLock(lockPlan{path: filepath.Join(dir, tflock.LockFile)}, []byte(testLock)))
	})
}
