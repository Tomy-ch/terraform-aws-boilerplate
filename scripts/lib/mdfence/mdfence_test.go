package mdfence_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/mdfence"
)

func TestFor(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("バッククォートを含まなければ CommonMark の下限 3 を返す", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, "```", mdfence.For("普通の理由"))
		})

		// 値と同じ長さのフェンスを返すと、値の側がフェンスを閉じて Markdown を脱出できる。
		t.Run("3 連バッククォートを含む値には 4 連を返す", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, "````", mdfence.For("``` を含む理由"))
		})

		t.Run("最長の連より 1 つ長い長さを返す", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, "``````", mdfence.For("a `````"))
		})

		t.Run("連が途切れれば長さは数え直す", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, "```", mdfence.For("`a`b`c`"))
		})

		t.Run("空の本文でも下限を返す", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, "```", mdfence.For(""))
		})
	})
}

func TestSection(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("見出しに件数を添えて本文をフェンスで包む", func(t *testing.T) {
			t.Parallel()
			var b strings.Builder
			mdfence.Section(&b, "cooldown 未達", []string{"- a@v1", "- b@v2"})
			assert.Equal(t, "## cooldown 未達 (2)\n\n```text\n- a@v1\n- b@v2\n```\n\n", b.String())
		})

		t.Run("本文が閉じられない長さのフェンスを使う", func(t *testing.T) {
			t.Parallel()
			var b strings.Builder
			mdfence.Section(&b, "見出し", []string{"``` を含む行"})
			assert.Contains(t, b.String(), "````text\n``` を含む行\n````")
		})
	})
}
