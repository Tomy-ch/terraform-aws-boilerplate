package branches

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 宣言は定数なので、値そのものではなく**値が満たすべき関係**を固定する。
// 値を書き写すテストは、宣言を変えたときに2箇所を直させるだけで何も守らない。

func Test_ReleasePattern(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		// 捕捉群の順序が変わると base-branch が別の版を「最新」と判定する。
		t.Run("major / minor / patch の順で捕捉する", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, []string{"release/v2.10.3", "2", "10", "3"},
				ReleasePattern.FindStringSubmatch("release/v2.10.3"))
		})

		// parseLine は m[1] m[2] m[3] を長さ検査なしで読む。3群を割ると panic する。
		t.Run("捕捉群はちょうど3つである", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, 3, ReleasePattern.NumSubexp())
		})

		t.Run("接頭辞は ReleasePrefix と同じ出所から組む", func(t *testing.T) {
			t.Parallel()
			assert.Contains(t, ReleasePattern.String(), ReleasePrefix)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// プレリリースやビルドメタデータを最新版に選ぶと、release が作らない形の
		// ブランチを base として指すことになる。
		for _, name := range []string{
			"release/v1.2.3-rc1",
			"release/v1.2",
			"release/next",
			"hotfix/v1.2.3",
			"refs/heads/release/v1.2.3",
		} {
			t.Run(name+" は一致しない", func(t *testing.T) {
				t.Parallel()
				assert.Nil(t, ReleasePattern.FindStringSubmatch(name))
			})
		}
	})
}

func Test_Protected(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		// 実環境へ届くブランチが保護対象から漏れると、レビューを経ない変更が
		// そのまま適用へ流れる。
		t.Run("Deploy の全要素を含む", func(t *testing.T) {
			t.Parallel()
			for _, d := range Deploy {
				assert.Contains(t, Protected, d, d)
			}
		})

		// release 線と hotfix 線は glob で覆う。個別のブランチ名では列挙できない。
		t.Run("release 線と hotfix 線を glob で覆う", func(t *testing.T) {
			t.Parallel()
			assert.Contains(t, Protected, ReleasePrefix+"**/*")
			assert.Contains(t, Protected, HotfixPrefix+"**/*")
		})

		t.Run("重複を持たない", func(t *testing.T) {
			t.Parallel()
			seen := map[string]bool{}
			for _, p := range Protected {
				require.False(t, seen[p], "重複: %s", p)
				seen[p] = true
			}
		})
	})
}

func Test_Deploy(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		// repo-setup はこの順序でブランチを作る。Default が最後でないと、
		// デフォルトブランチの移動が未作成のブランチを指しうる。
		t.Run("末尾が Default である", func(t *testing.T) {
			t.Parallel()
			require.NotEmpty(t, Deploy)
			assert.Equal(t, Default, Deploy[len(Deploy)-1])
		})
	})
}

func Test_Line(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("線ごとに接頭辞を返す", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, ReleasePrefix, LineRelease.Prefix())
			assert.Equal(t, HotfixPrefix, LineHotfix.Prefix())
		})

		// 一覧が実装とずれると、usage が受け付けない線を案内する。
		t.Run("Lines が返す線はすべて Valid である", func(t *testing.T) {
			t.Parallel()
			require.NotEmpty(t, Lines())
			for _, l := range Lines() {
				assert.True(t, l.Valid(), string(l))
			}
		})

		t.Run("接頭辞はスラッシュで終わる", func(t *testing.T) {
			t.Parallel()
			for _, l := range Lines() {
				assert.True(t, strings.HasSuffix(l.Prefix(), "/"), string(l))
			}
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 未知の線を空接頭辞で通すと、`/v1.2.3` のようなブランチ名が作られる。
		t.Run("未知の線は Valid でない", func(t *testing.T) {
			t.Parallel()
			assert.False(t, Line("no-such").Valid())
			assert.Empty(t, Line("no-such").Prefix())
		})

		t.Run("空文字列は Valid でない", func(t *testing.T) {
			t.Parallel()
			assert.False(t, Line("").Valid())
		})
	})
}
