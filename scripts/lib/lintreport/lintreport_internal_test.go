package lintreport

import (
	"math"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_itoa(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			n    int
			want string
		}{
			// 0 だけが早期 return を通ります。ループは `n > 0` なので、固定しないと
			// 空文字を返す形へ壊れても気づけません。
			"0 の場合、0 を返す": {n: 0, want: "0"},

			"1桁の場合、そのまま返す":      {n: 7, want: "7"},
			"1桁の上限の場合、そのまま返す":   {n: 9, want: "9"},
			"2桁の下限の場合、桁上がりする":   {n: 10, want: "10"},
			"2桁の上限の場合、そのまま返す":   {n: 99, want: "99"},
			"3桁の下限の場合、桁上がりする":   {n: 100, want: "100"},
			"負の1桁の場合、符号を付ける":    {n: -1, want: "-1"},
			"負の2桁の場合、符号を付ける":    {n: -10, want: "-10"},
			"下位の桁が0の場合、0を落とさない": {n: 1000, want: "1000"},

			// 桁の緩衝は [20]byte です。最大値は19桁、その符号付きは20桁で、
			// 符号側が緩衝をちょうど使い切ります。
			"最大値の場合、桁あふれしない":      {n: math.MaxInt, want: strconv.Itoa(math.MaxInt)},
			"最大値の符号付きの場合、桁あふれしない": {n: -math.MaxInt, want: strconv.Itoa(-math.MaxInt)},
			// **負の下限は -math.MaxInt ではない。** 符号を反転させる実装では、ここだけが
			// 正へ転じずに桁を1つも作らず "-" を返す。1つ内側の -math.MaxInt では通ってしまう。
			"負の下限の場合、桁が消えない": {n: math.MinInt, want: strconv.Itoa(math.MinInt)},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				assert.Equal(t, tt.want, itoa(tt.n))
			})
		}
	})
}
