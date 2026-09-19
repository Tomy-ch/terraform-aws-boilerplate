package branches

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 宣言は定数なので、**宣言を読み上げるだけの assert は書かない。** `Protected` が
// `ReleasePrefix + "**/*"` から組まれている以上、それを含むことを確かめても、同じ式を
// 二度書いただけで何も守らない。ここで固定するのは、宣言の外側の何か —— 読む側が
// 前提にしている性質と、空への退化 —— に限る。

func Test_ReleasePattern(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		// base-branch の parseLine は m[1] m[2] m[3] を長さ検査なしで読む。
		// 捕捉群を減らすと、一致した瞬間に index out of range で落ちる。
		t.Run("捕捉群はちょうど3つで major / minor / patch の順に並ぶ", func(t *testing.T) {
			t.Parallel()
			require.Equal(t, 3, ReleasePattern.NumSubexp())
			assert.Equal(t, []string{"release/v2.10.3", "2", "10", "3"},
				ReleasePattern.FindStringSubmatch("release/v2.10.3"))
		})

		// 接頭辞を変えたとき、パターンが追随しないと base-branch が何も見つけられなくなる。
		t.Run("ReleasePrefix で始まる名前に一致する", func(t *testing.T) {
			t.Parallel()
			assert.NotNil(t, ReleasePattern.FindStringSubmatch(ReleasePrefix+"v1.0.0"))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// プレリリースや別の線を最新版に選ぶと、release が作らない形のブランチを
		// base として指すことになる。
		for _, name := range []string{
			"release/v1.2.3-rc1",
			"release/v1.2",
			"release/next",
			"hotfix/v1.2.3",
			"refs/heads/release/v1.2.3",
			"",
		} {
			t.Run("一致しない: "+name, func(t *testing.T) {
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
		// そのまま適用へ流れる。Deploy と Protected は別の変数なので、これは
		// 独立した2つの宣言の突合になる。
		t.Run("Deploy の全要素を含む", func(t *testing.T) {
			t.Parallel()
			for _, d := range Deploy {
				assert.Contains(t, Protected, d, d)
			}
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

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 空へ退化すると、生成先の保護対象が0件になる。生成側にもガードがあるが、
		// 宣言そのものが空でないことをここで直接主張する。
		t.Run("空でない", func(t *testing.T) {
			t.Parallel()
			assert.NotEmpty(t, Protected)
		})
	})
}

func Test_Deploy(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		// repo-setup はこの順序でブランチを作り、最後のものをデフォルトブランチへ移す。
		// Default が末尾でないと、未作成のブランチを指しうる。
		t.Run("末尾が Default である", func(t *testing.T) {
			t.Parallel()
			require.NotEmpty(t, Deploy)
			assert.Equal(t, Default, Deploy[len(Deploy)-1])
		})

		t.Run("重複を持たない", func(t *testing.T) {
			t.Parallel()
			seen := map[string]bool{}
			for _, d := range Deploy {
				require.False(t, seen[d], "重複: %s", d)
				seen[d] = true
			}
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 空だと repo-setup が引数無しの `git push origin` を組む。
		t.Run("空でない", func(t *testing.T) {
			t.Parallel()
			assert.NotEmpty(t, Deploy)
		})
	})
}

func Test_GatePush(t *testing.T) {
	t.Parallel()

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 空へ退化すると、その workflow は push で一度も走らない。
		t.Run("空でない", func(t *testing.T) {
			t.Parallel()
			assert.NotEmpty(t, GatePush)
		})
	})
}

func Test_ReleasePush(t *testing.T) {
	t.Parallel()

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("空でない", func(t *testing.T) {
			t.Parallel()
			assert.NotEmpty(t, ReleasePush)
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
		t.Run("Lines が返す線はすべて Valid で、接頭辞が重複しない", func(t *testing.T) {
			t.Parallel()
			require.NotEmpty(t, Lines())

			seen := map[string]bool{}
			for _, l := range Lines() {
				assert.True(t, l.Valid(), string(l))
				require.False(t, seen[l.Prefix()], "接頭辞が重複: %s", l.Prefix())
				seen[l.Prefix()] = true
			}
		})

		// 接頭辞とブランチ名を連結するので、区切りが無いと `releasev1.0.0` になる。
		t.Run("接頭辞はスラッシュで終わる", func(t *testing.T) {
			t.Parallel()
			for _, l := range Lines() {
				assert.True(t, strings.HasSuffix(l.Prefix(), "/"), string(l))
			}
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 未知の線を空接頭辞で通すと、`v1.2.3` という接頭辞の無いブランチが作られる。
		for _, name := range []string{"no-such", "", "release/", "RELEASE"} {
			t.Run("Valid でない: "+name, func(t *testing.T) {
				t.Parallel()
				assert.False(t, Line(name).Valid())
				assert.Empty(t, Line(name).Prefix())
			})
		}
	})
}
