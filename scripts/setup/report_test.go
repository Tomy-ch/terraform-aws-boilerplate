package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_newReport(t *testing.T) {
	t.Parallel()

	t.Run("見出しを出す", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer

		newReport(&out, "T")
		assert.Equal(t, "# T\n", out.String())
	})
}

func Test_report_check(t *testing.T) {
	t.Parallel()

	t.Run("真は ✅、偽は ❌ を付けて数える", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer

		r := newReport(&out, "T")
		r.check(true, "a")
		r.check(false, "b")
		assert.Equal(t, "# T\n✅ a\n❌ b\n", out.String())
		assert.Equal(t, 2, r.count)
		assert.Equal(t, 1, r.ngs)
	})
}

func Test_report_ng(t *testing.T) {
	t.Parallel()

	t.Run("❌ として数える", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer

		r := newReport(&out, "T")
		r.ng("x")
		require.ErrorIs(t, r.result(), errNG)
		assert.Contains(t, out.String(), "❌ x\n")
	})
}

func Test_report_result(t *testing.T) {
	t.Parallel()

	t.Run("すべて ✅ なら nil", func(t *testing.T) {
		t.Parallel()

		r := newReport(&bytes.Buffer{}, "T")
		r.check(true, "a")
		require.NoError(t, r.result())
	})

	t.Run("1件も確かめていなければ errNG", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer

		r := newReport(&out, "T")
		require.ErrorIs(t, r.result(), errNG)
		assert.Contains(t, out.String(), "❌ 確かめた項目がありません")
	})
}
