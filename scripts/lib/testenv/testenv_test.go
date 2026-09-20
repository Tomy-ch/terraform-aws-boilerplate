package testenv

import (
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 環境変数の名前は workflow 側にリテラルで書かれています。テスト内のリテラルと突き合わせても
// 「自分で書いた文字列と一致した」しか言えないので、workflow そのものを読みます。
// 定数だけを改名すると、CI では skip が失敗へ変わらなくなります。
func TestRequireNonRootEnv_workflowと一致する(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile("../../../.github/workflows/go-test.yaml")
	require.NoError(t, err)

	re := regexp.MustCompile(`(?m)^\s+` + regexp.QuoteMeta(RequireNonRootEnv) + `:\s`)
	assert.True(t, re.Match(body), "go-test.yaml が %s を立てていない", RequireNonRootEnv)
}
