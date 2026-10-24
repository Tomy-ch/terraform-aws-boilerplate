package markers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarkers_kind(t *testing.T) {
	t.Parallel()
	m := New("sample")

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("印の語を返す", func(t *testing.T) {
			t.Parallel()
			kind, ok, err := m.kind("x: 1 # sample:replace-end")
			require.NoError(t, err)
			assert.True(t, ok)
			assert.Equal(t, "replace-end", kind)
		})

		t.Run("印が無ければ ok を返さない", func(t *testing.T) {
			t.Parallel()
			_, ok, err := m.kind("# 普通のコメント")
			require.NoError(t, err)
			assert.False(t, ok)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 語の後ろを語境界で切ると、line-x が line として通る。
		t.Run("既知の語で始まる別の語はエラーにする", func(t *testing.T) {
			t.Parallel()
			_, _, err := m.kind("# sample:line-x")
			require.ErrorIs(t, err, ErrUnknownMarker)
		})

		t.Run("語の無い印はエラーにする", func(t *testing.T) {
			t.Parallel()
			_, _, err := m.kind("# sample:")
			require.ErrorIs(t, err, ErrUnknownMarker)
		})

		t.Run("1行に2つの印があればエラーにする", func(t *testing.T) {
			t.Parallel()
			_, _, err := m.kind("# sample:begin # sample:end")
			require.ErrorIs(t, err, ErrUnknownMarker)
		})
	})
}

func Test_restore(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name string
			in   string
			want string
		}{
			{name: "# の行から字下げを残して戻す", in: "    # = key: v", want: "    key: v"},
			{name: "// の行を戻す", in: "// = x := 1", want: "x := 1"},
			{name: "HTML コメントの行を戻す", in: "<!-- = 本文 -->", want: "本文"},
			{name: "= の後ろが空なら空行に戻す", in: "# =", want: ""},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				got, err := restore(tt.in)
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("= の無いコメントはエラーにする", func(t *testing.T) {
			t.Parallel()
			_, err := restore("# key: v")
			require.ErrorIs(t, err, ErrBadReplacement)
		})
	})
}
