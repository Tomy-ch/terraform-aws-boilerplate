package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

	assert.Equal(t, "registry.terraform.io/hashicorp/aws", providerAddr("HashiCorp/aws"))
	assert.Equal(t, "example.com/x/y", providerAddr("Example.com/x/y"))
}

func Test_placeLock(t *testing.T) {
	t.Parallel()

	c := canonical{decls: []tflock.Decl{{Name: "aws", Source: "hashicorp/aws", Version: "6.73.0"}}, content: []byte(testLock)}

	t.Run("正本と同じ集合を要求するディレクトリに lock を置く", func(t *testing.T) {
		t.Parallel()
		dir := tree(t, map[string]string{"versions.tf": awsRequired})
		require.NoError(t, placeLock(dir, c))
		assert.Equal(t, testLock, readFile(t, filepath.Join(dir, tflock.LockFile)))
	})

	t.Run("provider を要求しないディレクトリの lock を消し、無くてもよい", func(t *testing.T) {
		t.Parallel()
		dir := tree(t, map[string]string{"main.tf": "", tflock.LockFile: "stale"})
		require.NoError(t, placeLock(dir, c))
		assert.NoFileExists(t, filepath.Join(dir, tflock.LockFile))
		require.NoError(t, placeLock(dir, c))
	})

	t.Run("正本に無い provider を要求すれば errProviders で、何も書かない", func(t *testing.T) {
		t.Parallel()
		dir := tree(t, map[string]string{"versions.tf": strings.Replace(awsRequired, "hashicorp/aws", "hashicorp/random", 1)})
		require.ErrorIs(t, placeLock(dir, c), errProviders)
		assert.NoFileExists(t, filepath.Join(dir, tflock.LockFile))
	})

	t.Run("正本の一部だけを要求すれば errProviders", func(t *testing.T) {
		t.Parallel()
		two := canonical{decls: append(append([]tflock.Decl{}, c.decls...), tflock.Decl{Name: "r", Source: "hashicorp/random", Version: "3.0.0"}), content: c.content}
		dir := tree(t, map[string]string{"versions.tf": awsRequired})
		require.ErrorIs(t, placeLock(dir, two), errProviders)
	})

	t.Run("要求を読めなければエラー", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, placeLock(tree(t, map[string]string{"versions.tf": "terraform {\n"}), c), errProviders)
	})
}
