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

// h1 と zh は照合値の形を満たす値を作る。
func h1(c string) string { return "h1:" + strings.Repeat(c, 43) + "=" }
func zh(c string) string { return "zh:" + strings.Repeat(c, 64) }

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

var (
	awsHashes = []string{h1("a"), h1("b"), h1("c"), zh("0")}
	awsBlock  = block("registry.terraform.io/hashicorp/aws", "6.73.0", awsHashes...)
)

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

	t.Run("CRLF の改行も読む", func(t *testing.T) {
		t.Parallel()

		got, err := tflock.ParseDecl([]byte(strings.ReplaceAll(decl, "\n", "\r\n")))
		require.NoError(t, err)
		assert.Equal(t, []tflock.Decl{{Name: "aws", Source: "hashicorp/aws", Version: "6.73.0"}}, got)
	})

	for name, content := range map[string]string{
		"空":                 "",
		"コメントだけ":            "# x\n",
		"節の外の key":          "source = \"hashicorp/aws\"\n",
		"節の重複":              decl + "[aws]\nsource = \"hashicorp/aws\"\nversion = \"6.73.0\"\n",
		"key の重複":           decl + "version = \"6.74.0\"\n",
		"知らない key":          decl + "constraints = \"x\"\n",
		"範囲の版":              "[aws]\nsource = \"hashicorp/aws\"\nversion = \">= 6.0\"\n",
		"4桁の版":              "[aws]\nsource = \"hashicorp/aws\"\nversion = \"6.73.0.1\"\n",
		"host を前置した source": "[aws]\nsource = \"registry.terraform.io/hashicorp/aws\"\nversion = \"6.73.0\"\n",
		"空の source":         "[aws]\nsource = \"\"\nversion = \"6.73.0\"\n",
		"version が無い":       "[aws]\nsource = \"hashicorp/aws\"\n",
		"source の重複":        decl + "[aws2]\nsource = \"hashicorp/aws\"\nversion = \"6.73.0\"\n",
		"引用符の無い値":           "[aws]\nsource = hashicorp/aws\nversion = \"6.73.0\"\n",
		"単一引用符の値":           "[aws]\nsource = 'hashicorp/aws'\nversion = \"6.73.0\"\n",
		"行末のコメント":           "[aws]\nsource = \"hashicorp/aws\"\nversion = \"6.73.0\" # x\n",
		"入れ子の表":             "[aws.x]\nsource = \"hashicorp/aws\"\nversion = \"6.73.0\"\n",
		"表の配列":              "[[aws]]\nsource = \"hashicorp/aws\"\nversion = \"6.73.0\"\n",
		"大文字の節名":            "[AWS]\nsource = \"hashicorp/aws\"\nversion = \"6.73.0\"\n",
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

	t.Run("ディレクトリは読み取りのエラー", func(t *testing.T) {
		t.Parallel()

		_, err := tflock.ParseDeclFile(t.TempDir())
		require.Error(t, err)
		require.NotErrorIs(t, err, tflock.ErrInvalidDecl)
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
		assert.Equal(t, []tflock.Provider{{Addr: "registry.terraform.io/hashicorp/aws", Version: "6.73.0", Hashes: awsHashes}}, got)
	})

	t.Run("属性の並びが違っても読める", func(t *testing.T) {
		t.Parallel()

		src := "provider \"registry.terraform.io/hashicorp/aws\" {\n  hashes = [\"" + zh("0") + "\"]\n  version = \"6.73.0\"\n}\n"
		got, err := tflock.ParseLock([]byte(src), "x")
		require.NoError(t, err)
		assert.Equal(t, []tflock.Provider{{Addr: "registry.terraform.io/hashicorp/aws", Version: "6.73.0", Hashes: []string{zh("0")}}}, got)
	})

	t.Run("字下げした provider の行は、件数の食い違いとして落ちる", func(t *testing.T) {
		t.Parallel()

		_, err := tflock.ParseLock([]byte(lockOf(awsBlock)+"  provider \"registry.terraform.io/hashicorp/x\" {\n  version = \"1.0.0\"\n  hashes = [\"zh:1\"]\n}\n"), "x")
		require.ErrorIs(t, err, tflock.ErrInvalidLock)
		assert.Contains(t, err.Error(), "provider の行 1 件のうち 2 件")
	})

	t.Run("heredoc の中の行頭の provider は、件数の食い違いとして落ちる", func(t *testing.T) {
		t.Parallel()

		src := lockOf(strings.Replace(awsBlock, "  hashes", "  constraints2 = <<EOT\nprovider \"x\"\nEOT\n  hashes", 1))
		_, err := tflock.ParseLock([]byte(src), "x")
		require.ErrorIs(t, err, tflock.ErrInvalidLock)
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
		"version が空":        lockOf(strings.Replace(awsBlock, "\"6.73.0\"\n  constraints", "\"\"\n  constraints", 1)),
		"version が null":    lockOf(strings.Replace(awsBlock, "\"6.73.0\"\n  constraints", "null\n  constraints", 1)),
		"hashes が空":         lockOf(block("registry.terraform.io/hashicorp/aws", "6.73.0")),
		"hashes が null":     lockOf("provider \"registry.terraform.io/hashicorp/aws\" {\n  version = \"6.73.0\"\n  hashes = null\n}\n"),
		"block の中の block":   lockOf(strings.Replace(awsBlock, "  hashes", "  nested {\n  }\n  hashes", 1)),
		"文字列でない version":    lockOf(strings.Replace(awsBlock, "\"6.73.0\"\n  constraints", "6\n  constraints", 1)),
		"文字列でない hashes の要素": lockOf(strings.Replace(awsBlock, "\""+zh("0")+"\"", "1", 1)),
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

	for name, in := range map[string][]string{
		"空":      nil,
		"知らない名前": {"linux-x64", "windows-x64"},
		"重複":     {"linux-x64", "linux-x64"},
	} {
		t.Run(name+"はエラー", func(t *testing.T) {
			t.Parallel()

			_, err := tflock.Platforms(in)
			require.ErrorIs(t, err, tflock.ErrInvalidPlatform)
		})
	}
}

func TestCheck(t *testing.T) {
	t.Parallel()

	decls := []tflock.Decl{{Name: "aws", Source: "hashicorp/aws", Version: "6.73.0"}}
	aws := tflock.Provider{Addr: "registry.terraform.io/hashicorp/aws", Version: "6.73.0", Hashes: awsHashes}

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
		"重複した h1 で数を満たす":        {[]tflock.Provider{{Addr: aws.Addr, Version: aws.Version, Hashes: []string{h1("a"), h1("a"), h1("a"), zh("0")}}}, 3, "h1: が 1 種"},
		"zh が無い":                {[]tflock.Provider{{Addr: aws.Addr, Version: aws.Version, Hashes: []string{h1("a"), h1("b"), h1("c")}}}, 3, "zh: がありません"},
		"platform が0件":          {[]tflock.Provider{aws}, 0, "platform が0件"},
	} {
		t.Run(name+"を名指す", func(t *testing.T) {
			t.Parallel()

			got := tflock.Check(decls, c.providers, c.platforms)
			require.Len(t, got, 1)
			assert.Contains(t, got[0], c.want)
		})
	}

	for name, hash := range map[string]string{
		"h1: の後が空": "h1:",
		"h1: が短い":  "h1:abc=",
		"zh: が大文字": "zh:" + strings.Repeat("A", 64),
		"知らない接頭辞":  "h2:" + strings.Repeat("a", 43) + "=",
		"接頭辞が無い":   strings.Repeat("a", 64),
	} {
		t.Run("照合値の形が違う "+name+" を名指す", func(t *testing.T) {
			t.Parallel()

			got := tflock.Check(decls, []tflock.Provider{{Addr: aws.Addr, Version: aws.Version, Hashes: append(append([]string{}, awsHashes...), hash)}}, 3)
			require.Len(t, got, 1)
			assert.Contains(t, got[0], "形が h1: でも zh: でもありません")
		})
	}

	t.Run("複数の食い違いを全件、決まった順で返す", func(t *testing.T) {
		t.Parallel()

		providers := []tflock.Provider{
			{Addr: "registry.terraform.io/hashicorp/z", Version: "1.0.0", Hashes: []string{h1("a")}},
			{Addr: "registry.terraform.io/hashicorp/b", Version: "1.0.0", Hashes: awsHashes},
		}
		got := tflock.Check(decls, providers, 3)
		assert.Equal(t, []string{
			"registry.terraform.io/hashicorp/aws が lock にありません",
			"registry.terraform.io/hashicorp/b は providers.toml にありません",
			"registry.terraform.io/hashicorp/z は providers.toml にありません",
			"registry.terraform.io/hashicorp/z の h1: が 1 種で、platform の 3 と合いません（make tf-providers-lock で作り直す）",
			"registry.terraform.io/hashicorp/z に zh: がありません",
		}, got)
	})
}
