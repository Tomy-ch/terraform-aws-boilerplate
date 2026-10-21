package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const tflintFixture = `# コメント
config {
  format = "compact"
}

plugin "terraform" {
  enabled = true
}

plugin "aws" {
  enabled = true
  version = "0.49.0" # 固定
  source  = "github.com/terraform-linters/tflint-ruleset-aws"
}

rule "terraform_unused_declarations" {
  enabled = true
}
`

func Test_parseTflintPlugins(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("source を持つ plugin を github backend の道具として返す", func(t *testing.T) {
			t.Parallel()
			got, err := parseTflintPlugins([]byte(tflintFixture))
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, "github:terraform-linters/tflint-ruleset-aws@0.49.0", got[0].id())
		})

		t.Run("2つ目の plugin は1つ目の属性を引き継がない", func(t *testing.T) {
			t.Parallel()
			got, err := parseTflintPlugins([]byte("plugin \"aws\" {\n  version = \"0.49.0\"\n  source = \"github.com/terraform-linters/r\"\n}\nplugin \"terraform\" {\n  enabled = true\n}\n"))
			require.NoError(t, err)
			assert.Len(t, got, 1)
		})

		t.Run("plugin を持たないファイルは0件を返す", func(t *testing.T) {
			t.Parallel()
			got, err := parseTflintPlugins([]byte("config {\n  format = \"compact\"\n}\n"))
			require.NoError(t, err)
			assert.Empty(t, got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 理由は parseTflintPlugins の doc コメントが持つ。
		t.Run("plugin の中の解釈できない行はエラーにする", func(t *testing.T) {
			t.Parallel()
			_, err := parseTflintPlugins([]byte("plugin \"aws\" {\n  version = \"0.49.0\"\n  signing_key = <<-KEY\n  KEY\n}\n"))
			require.ErrorIs(t, err, ErrInvalidTflint)
		})

		// 開始行として読めない plugin を他の block と同じく飛ばすと、窓を掛けないまま取得される。
		for name, content := range map[string]string{
			"1行で書いた plugin":       "plugin \"aws\" { source = \"github.com/o/r\" }\n",
			"波括弧を次の行に置いた plugin":  "plugin \"aws\"\n{\n  source = \"github.com/o/r\"\n  version = \"1.0.0\"\n}\n",
			"ラベルを引用符で囲まない plugin": "plugin aws {\n}\n",
			"複数行のコメントの中の plugin":  "/*\nplugin \"aws\" {\n*/\n",
			"同じ属性を2度書いた plugin":   "plugin \"aws\" {\n  version = \"1\"\n  version = \"2\"\n}\n",
			"数値の version":         "plugin \"aws\" {\n  version = 1\n}\n",
			"式の version":          "plugin \"aws\" {\n  version = var.v\n}\n",
		} {
			t.Run(name+"はエラーにする", func(t *testing.T) {
				t.Parallel()
				_, err := parseTflintPlugins([]byte(content))
				require.ErrorIs(t, err, ErrInvalidTflint)
			})
		}

		t.Run("閉じていない plugin はエラーにする", func(t *testing.T) {
			t.Parallel()
			_, err := parseTflintPlugins([]byte("plugin \"aws\" {\n  version = \"0.49.0\"\n"))
			require.ErrorIs(t, err, ErrInvalidTflint)
		})

		t.Run("block の中の属性の誤りを、閉じた行と plugin 名を添えて返す", func(t *testing.T) {
			t.Parallel()
			_, err := parseTflintPlugins([]byte("plugin \"aws\" {\n  source = \"github.com/o/r\"\n}\n"))
			require.ErrorIs(t, err, ErrInvalidTflint)
			assert.Contains(t, err.Error(), `3 行目で閉じる plugin "aws"`)
		})
	})
}

func Test_tflintPluginTool(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("github.com の source を github backend へ写す", func(t *testing.T) {
			t.Parallel()
			got, ok, err := tflintPluginTool("aws", map[string]string{
				"source": "github.com/terraform-linters/tflint-ruleset-aws", "version": "0.49.0",
			})
			require.NoError(t, err)
			require.True(t, ok)
			assert.Equal(t, "github:terraform-linters/tflint-ruleset-aws", got.key)
			assert.Equal(t, "0.49.0", got.version)
		})

		t.Run("source も version も無い同梱の ruleset は対象にしない", func(t *testing.T) {
			t.Parallel()
			_, ok, err := tflintPluginTool("terraform", map[string]string{"enabled": "true"})
			require.NoError(t, err)
			assert.False(t, ok)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("source があるのに version が無ければエラーにする", func(t *testing.T) {
			t.Parallel()
			_, _, err := tflintPluginTool("aws", map[string]string{"source": "github.com/o/r"})
			require.ErrorIs(t, err, ErrInvalidTflint)
		})

		t.Run("version が空ならエラーにする", func(t *testing.T) {
			t.Parallel()
			_, _, err := tflintPluginTool("aws", map[string]string{"source": "github.com/o/r", "version": ""})
			require.ErrorIs(t, err, ErrInvalidTflint)
		})

		t.Run("version があるのに source が無ければエラーにする", func(t *testing.T) {
			t.Parallel()
			_, _, err := tflintPluginTool("aws", map[string]string{"version": "0.49.0"})
			require.ErrorIs(t, err, ErrInvalidTflint)
		})

		t.Run("terraform-linters 以外の org の source はエラーにする", func(t *testing.T) {
			t.Parallel()
			_, _, err := tflintPluginTool("x", map[string]string{"source": "github.com/someone/tflint-ruleset-x", "version": "1.0.0"})
			require.ErrorIs(t, err, ErrUntrustedPlugin)
		})

		// 窓を測る取得元が無い source を黙って外すと、その plugin は検査されないまま取得される。
		for _, source := range []string{"example.com/o/r", "github.com/o", "github.com/o/r/extra"} {
			t.Run(source+" はエラーにする", func(t *testing.T) {
				t.Parallel()
				_, _, err := tflintPluginTool("x", map[string]string{"source": source, "version": "1.0.0"})
				require.ErrorIs(t, err, errUnsupportedBackend)
			})
		}
	})
}
