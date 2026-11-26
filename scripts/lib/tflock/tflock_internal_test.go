package tflock

import (
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zclconf/go-cty/cty"
)

func blockOf(t *testing.T, src string) *hclsyntax.Block {
	t.Helper()
	file, diags := hclsyntax.ParseConfig([]byte(src), "x", hcl.InitialPos)
	require.False(t, diags.HasErrors(), diags.Error())
	body, ok := file.Body.(*hclsyntax.Body)
	require.True(t, ok)
	require.Len(t, body.Blocks, 1)

	return body.Blocks[0]
}

func Test_providerOf(t *testing.T) {
	t.Parallel()

	t.Run("version と hashes を読み、constraints は読み捨てる", func(t *testing.T) {
		t.Parallel()
		got, err := providerOf(blockOf(t, "provider \"p\" {\n  version = \"1.0.0\"\n  constraints = \"1.0.0\"\n  hashes = [\"zh:1\"]\n}\n"), "x")
		require.NoError(t, err)
		assert.Equal(t, Provider{Addr: "p", Version: "1.0.0", Hashes: []string{"zh:1"}}, got)
	})

	for name, src := range map[string]string{
		"見出しが2つ":       "provider \"p\" \"q\" {\n  version = \"1.0.0\"\n  hashes = [\"zh:1\"]\n}\n",
		"見出しが無い":       "provider {\n  version = \"1.0.0\"\n  hashes = [\"zh:1\"]\n}\n",
		"provider でない": "module \"p\" {\n  version = \"1.0.0\"\n  hashes = [\"zh:1\"]\n}\n",
		"評価できない式":      "provider \"p\" {\n  version = var.x\n  hashes = [\"zh:1\"]\n}\n",
		"hashes が文字列":  "provider \"p\" {\n  version = \"1.0.0\"\n  hashes = \"zh:1\"\n}\n",
	} {
		t.Run(name+"はエラー", func(t *testing.T) {
			t.Parallel()
			_, err := providerOf(blockOf(t, src), "x")
			require.ErrorIs(t, err, ErrInvalidLock)
		})
	}
}

func Test_stringList(t *testing.T) {
	t.Parallel()

	t.Run("文字列の並びを返す", func(t *testing.T) {
		t.Parallel()
		got, err := stringList(cty.TupleVal([]cty.Value{cty.StringVal("a"), cty.StringVal("b")}))
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b"}, got)
	})

	t.Run("並びでなければエラー", func(t *testing.T) {
		t.Parallel()
		_, err := stringList(cty.StringVal("a"))
		require.Error(t, err)
	})

	t.Run("文字列でない要素があればエラー", func(t *testing.T) {
		t.Parallel()
		_, err := stringList(cty.TupleVal([]cty.Value{cty.StringVal("a"), cty.NumberIntVal(1)}))
		require.Error(t, err)
	})
}

func Test_hashCounts(t *testing.T) {
	t.Parallel()

	h1, zh := hashCounts([]string{"h1:a", "h1:a", "h1:b", "zh:1", "zh:1", "x:1"})
	assert.Equal(t, 2, h1, "重複は数えない")
	assert.Equal(t, 1, zh)
}

func Test_sortedKeys(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"a", "b", "c"}, sortedKeys(map[string]int{"c": 1, "a": 2, "b": 3}))
}
