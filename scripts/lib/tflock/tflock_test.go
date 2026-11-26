package tflock_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/tflock"
)

const decl = "# provider の版の正本\n[aws]\nsource = \"hashicorp/aws\"\nversion = \"6.73.0\"\n"

func lockOf(blocks ...string) string {
	return "# This file is maintained automatically by \"terraform init\".\n\n" + strings.Join(blocks, "\n")
}

func block(addr, version string, hashes ...string) string {
	quoted := make([]string, 0, len(hashes))
	for _, h := range hashes {
		quoted = append(quoted, "    \""+h+"\",\n")
	}

	return "provider \"" + addr + "\" {\n  version     = \"" + version + "\"\n  constraints = \"6.73.0\"\n  hashes = [\n" + strings.Join(quoted, "") + "  ]\n}\n"
}

var awsBlock = block("registry.terraform.io/hashicorp/aws", "6.73.0", "h1:a=", "h1:b=", "h1:c=", "zh:01")

func TestDecl_Addr(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "registry.terraform.io/hashicorp/aws", tflock.Decl{Source: "hashicorp/aws"}.Addr())
}

func TestParseDecl(t *testing.T) {
	t.Parallel()

	t.Run("節ごとに source と version を返す", func(t *testing.T) {
		t.Parallel()

		got, err := tflock.ParseDecl([]byte(decl + "\n[random]\nversion = \"3.7.2\"\nsource = \"hashicorp/random\"\n"))
		require.NoError(t, err)
		assert.Equal(t, []tflock.Decl{{Name: "aws", Source: "hashicorp/aws", Version: "6.73.0"}, {Name: "random", Source: "hashicorp/random", Version: "3.7.2"}}, got)
	})

	for name, content := range map[string]string{
		"空":                 "",
		"コメントだけ":            "# x\n",
		"節の外の key":          "source = \"hashicorp/aws\"\n",
		"節の重複":              decl + "[aws]\nsource = \"hashicorp/aws\"\nversion = \"6.73.0\"\n",
		"key の重複":           decl + "version = \"6.74.0\"\n",
		"知らない key":          decl + "constraints = \"x\"\n",
		"範囲の版":              "[aws]\nsource = \"hashicorp/aws\"\nversion = \">= 6.0\"\n",
		"host を前置した source": "[aws]\nsource = \"registry.terraform.io/hashicorp/aws\"\nversion = \"6.73.0\"\n",
		"version が無い":       "[aws]\nsource = \"hashicorp/aws\"\n",
		"source の重複":        decl + "[aws2]\nsource = \"hashicorp/aws\"\nversion = \"6.73.0\"\n",
		"引用符の無い値":           "[aws]\nsource = hashicorp/aws\nversion = \"6.73.0\"\n",
	} {
		t.Run(name+"はエラー", func(t *testing.T) {
			t.Parallel()

			_, err := tflock.ParseDecl([]byte(content))
			require.ErrorIs(t, err, tflock.ErrInvalidDecl)
		})
	}
}

func TestParseDeclFile(t *testing.T) {
	t.Parallel()

	t.Run("ファイルを読む", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), tflock.DeclFile)
		require.NoError(t, os.WriteFile(path, []byte(decl), 0o600))
		got, err := tflock.ParseDeclFile(path)
		require.NoError(t, err)
		assert.Len(t, got, 1)
	})

	t.Run("無いファイルはエラー", func(t *testing.T) {
		t.Parallel()

		_, err := tflock.ParseDeclFile(filepath.Join(t.TempDir(), tflock.DeclFile))
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("読めない中身は ErrInvalidDecl", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), tflock.DeclFile)
		require.NoError(t, os.WriteFile(path, []byte("x\n"), 0o600))
		_, err := tflock.ParseDeclFile(path)
		require.ErrorIs(t, err, tflock.ErrInvalidDecl)
	})
}

func TestVersionsTF(t *testing.T) {
	t.Parallel()

	got := tflock.VersionsTF([]tflock.Decl{{Name: "aws", Source: "hashicorp/aws", Version: "6.73.0"}})
	assert.Equal(t, "terraform {\n  required_providers {\n    aws = {\n      source  = \"hashicorp/aws\"\n      version = \"6.73.0\"\n    }\n  }\n}\n", got)
}

func TestParseLock(t *testing.T) {
	t.Parallel()

	t.Run("provider の block を読む", func(t *testing.T) {
		t.Parallel()

		got, err := tflock.ParseLock([]byte(lockOf(awsBlock)), "x")
		require.NoError(t, err)
		assert.Equal(t, []tflock.Provider{{Addr: "registry.terraform.io/hashicorp/aws", Version: "6.73.0", Hashes: []string{"h1:a=", "h1:b=", "h1:c=", "zh:01"}}}, got)
	})

	for name, content := range map[string]string{
		"空":                 "",
		"コメントだけ":            lockOf(),
		"HCL として読めない":       "provider \"x\" {\n",
		"最上位の属性":            lockOf(awsBlock) + "x = 1\n",
		"知らない block":        lockOf(awsBlock) + "module \"m\" {\n}\n",
		"知らない属性":            lockOf(strings.Replace(awsBlock, "  hashes", "  extra = 1\n  hashes", 1)),
		"provider の重複":      lockOf(awsBlock, awsBlock),
		"version が無い":       lockOf(strings.Replace(awsBlock, "  version     = \"6.73.0\"\n", "", 1)),
		"hashes が空":         lockOf(block("registry.terraform.io/hashicorp/aws", "6.73.0")),
		"字下げした provider の行": lockOf(awsBlock) + "  provider \"registry.terraform.io/hashicorp/x\" {\n  version = \"1.0.0\"\n  hashes = [\"zh:1\"]\n}\n",
		"block の中の block":   lockOf(strings.Replace(awsBlock, "  hashes", "  nested {\n  }\n  hashes", 1)),
		"文字列でない version":    lockOf(strings.Replace(awsBlock, "\"6.73.0\"\n  constraints", "6\n  constraints", 1)),
		"文字列でない hashes の要素": lockOf(strings.Replace(awsBlock, "\"zh:01\"", "1", 1)),
	} {
		t.Run(name+"はエラー", func(t *testing.T) {
			t.Parallel()

			_, err := tflock.ParseLock([]byte(content), "x")
			require.ErrorIs(t, err, tflock.ErrInvalidLock)
		})
	}
}

func TestPlatforms(t *testing.T) {
	t.Parallel()

	t.Run("mise の名前を terraform の platform へ写す", func(t *testing.T) {
		t.Parallel()

		got, err := tflock.Platforms([]string{"linux-x64", "linux-arm64", "macos-arm64", "macos-x64"})
		require.NoError(t, err)
		assert.Equal(t, []string{"linux_amd64", "linux_arm64", "darwin_arm64", "darwin_amd64"}, got)
	})

	t.Run("空はエラー", func(t *testing.T) {
		t.Parallel()

		_, err := tflock.Platforms(nil)
		require.ErrorIs(t, err, tflock.ErrInvalidPlatform)
	})

	t.Run("知らない名前はエラー", func(t *testing.T) {
		t.Parallel()

		_, err := tflock.Platforms([]string{"linux-x64", "windows-x64"})
		require.ErrorIs(t, err, tflock.ErrInvalidPlatform)
	})
}

func TestCheck(t *testing.T) {
	t.Parallel()

	decls := []tflock.Decl{{Name: "aws", Source: "hashicorp/aws", Version: "6.73.0"}}
	aws := tflock.Provider{Addr: "registry.terraform.io/hashicorp/aws", Version: "6.73.0", Hashes: []string{"h1:a=", "h1:b=", "h1:c=", "zh:01"}}

	t.Run("宣言と同じ集合・版で、h1 が platform の数なら食い違い0件", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, tflock.Check(decls, []tflock.Provider{aws}, 3))
	})

	for name, c := range map[string]struct {
		providers []tflock.Provider
		platforms int
		want      string
	}{
		"lock に無い provider":     {nil, 3, "lock にありません"},
		"版が違う":                  {[]tflock.Provider{{Addr: aws.Addr, Version: "6.70.0", Hashes: aws.Hashes}}, 3, "6.70.0"},
		"宣言に無い provider":        {[]tflock.Provider{aws, {Addr: "registry.terraform.io/hashicorp/x", Version: "1.0.0", Hashes: aws.Hashes}}, 3, "providers.toml にありません"},
		"h1 が platform の数より少ない": {[]tflock.Provider{aws}, 4, "h1: が 3 種"},
		"重複した h1 で数を満たす":        {[]tflock.Provider{{Addr: aws.Addr, Version: aws.Version, Hashes: []string{"h1:a=", "h1:a=", "h1:a=", "zh:01"}}}, 3, "h1: が 1 種"},
		"zh が無い":                {[]tflock.Provider{{Addr: aws.Addr, Version: aws.Version, Hashes: []string{"h1:a=", "h1:b=", "h1:c="}}}, 3, "zh: がありません"},
	} {
		t.Run(name+"を名指す", func(t *testing.T) {
			t.Parallel()

			got := tflock.Check(decls, c.providers, c.platforms)
			require.Len(t, got, 1)
			assert.Contains(t, got[0], c.want)
		})
	}
}
