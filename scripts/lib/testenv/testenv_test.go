package testenv_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/testenv"
)

func TestRequireNonRoot(t *testing.T) {
	// t.Setenv は並列実行と両立しない。
	t.Run("正常系", func(t *testing.T) {
		t.Run("root でなければ何もせず戻る", func(t *testing.T) {
			if os.Geteuid() == 0 {
				t.Skip("root で実行している")
			}
			testenv.RequireNonRoot(t, "この理由は使われない")
			assert.False(t, t.Skipped(), "root でないのに skip した")
		})
	})

	t.Run("異常系", func(t *testing.T) {
		// 環境変数が立っていても、root でなければ通常どおり戻る。立っているだけで
		// 落ちるなら、非 root の CI がこの経路を通った瞬間に全部赤くなる。
		t.Run("環境変数が立っていても root でなければ戻る", func(t *testing.T) {
			if os.Geteuid() == 0 {
				t.Skip("root で実行している")
			}
			t.Setenv(testenv.RequireNonRootEnv, "1")

			testenv.RequireNonRoot(t, "この理由は使われない")
			assert.False(t, t.Skipped(), "root でないのに skip した")
		})
	})
}

func TestRequireNonRootEnv(t *testing.T) {
	t.Parallel()

	// 名前は workflow が env として立てる文字列と一致していなければならない。
	// ずれると、skip を失敗へ変える経路が CI で働かないまま緑が残る。
	assert.Equal(t, "REQUIRE_NONROOT", testenv.RequireNonRootEnv)
}
