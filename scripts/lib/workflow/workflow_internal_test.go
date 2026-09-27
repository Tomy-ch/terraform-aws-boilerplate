package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_toLines(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			src    []string
			offset int
			want   []Line
		}{
			// 行番号は1始まりです。0始まりへ滑ると、違反の報告が1行ずつずれたまま
			// すべて緑になります。
			"offset が0の場合、1始まりの行番号を振る": {
				src:    []string{"a", "b", "c"},
				offset: 0,
				want:   []Line{{Number: 1, Text: "a"}, {Number: 2, Text: "b"}, {Number: 3, Text: "c"}},
			},
			// jobs: の後ろを切り出すとき、元の本文での行番号へ戻すために使われます。
			"offset が正の場合、その分だけ行番号をずらす": {
				src:    []string{"x", "y"},
				offset: 5,
				want:   []Line{{Number: 6, Text: "x"}, {Number: 7, Text: "y"}},
			},
			"1件の場合、その1件を返す": {
				src:    []string{"only"},
				offset: 0,
				want:   []Line{{Number: 1, Text: "only"}},
			},
			"空行を含む場合、空行も1行として数える": {
				src:    []string{"", "a"},
				offset: 0,
				want:   []Line{{Number: 1, Text: ""}, {Number: 2, Text: "a"}},
			},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				assert.Equal(t, tt.want, toLines(tt.src, tt.offset))
			})
		}

		t.Run("空の場合、長さ0のスライスを返す", func(t *testing.T) {
			t.Parallel()

			got := toLines(nil, 0)

			assert.NotNil(t, got, "Preamble は append で連結されるため、空でも長さ0のスライスを返す")
			assert.Empty(t, got)
		})
	})
}
