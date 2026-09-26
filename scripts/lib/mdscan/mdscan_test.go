package mdscan

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLines(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			content string
			want    []Line
		}{
			"フェンスが無ければ全行を1始まりの番号で返す": {
				content: "一\n二",
				want:    []Line{{Number: 1, Text: "一"}, {Number: 2, Text: "二"}},
			},
			"フェンスの開始行・内部・終了行をいずれも返さない": {
				content: "外\n```\n中\n```\n外2",
				want:    []Line{{Number: 1, Text: "外"}, {Number: 5, Text: "外2"}},
			},
			"チルダのフェンスも同じく除く": {
				content: "外\n~~~\n中\n~~~\n外2",
				want:    []Line{{Number: 1, Text: "外"}, {Number: 5, Text: "外2"}},
			},
			"バッククォートのフェンスはチルダでは閉じない": {
				content: "```\n~~~\n中\n```\n外",
				want:    []Line{{Number: 5, Text: "外"}},
			},
			"長いフェンスの中の短いフェンスは閉じない": {
				content: "````\n```\n中\n```\n````\n外",
				want:    []Line{{Number: 6, Text: "外"}},
			},
			"開きより長い閉じでも閉じる": {
				content: "```\n中\n`````\n外",
				want:    []Line{{Number: 4, Text: "外"}},
			},
			"info string を持つ行は閉じとして扱わない": {
				content: "```go\n中\n```go\nまだ中\n```\n外",
				want:    []Line{{Number: 6, Text: "外"}},
			},
			"字下げされたフェンスも区切りとして読む": {
				content: "外\n   ```\n   中\n   ```\n外2",
				want:    []Line{{Number: 1, Text: "外"}, {Number: 5, Text: "外2"}},
			},
			"閉じられないフェンスは以降をすべて飲み込む": {
				content: "外\n```\n中\n中2",
				want:    []Line{{Number: 1, Text: "外"}},
			},
			"バッククォート2つは区切りにならない": {
				content: "``まだ本文``",
				want:    []Line{{Number: 1, Text: "``まだ本文``"}},
			},
			"空の入力では1行の空文字を返す": {
				content: "",
				want:    []Line{{Number: 1, Text: ""}},
			},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, tt.want, Lines([]byte(tt.content)))
			})
		}
	})
}

func Test_fenceMarker(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			line       string
			wantMarker string
			wantInfo   string
			wantOK     bool
		}{
			"3つのバッククォートは区切り":      {line: "```", wantMarker: "```", wantInfo: "", wantOK: true},
			"info string を分けて返す":  {line: "```go", wantMarker: "```", wantInfo: "go", wantOK: true},
			"4つ以上も区切り":            {line: "````", wantMarker: "````", wantInfo: "", wantOK: true},
			"チルダも区切り":             {line: "~~~", wantMarker: "~~~", wantInfo: "", wantOK: true},
			"先頭の空白は無視する":          {line: "  ```", wantMarker: "```", wantInfo: "", wantOK: true},
			"2つでは区切りにならない":        {line: "``x``", wantMarker: "", wantInfo: "", wantOK: false},
			"行の途中のバッククォートは区切りでない": {line: "text ```", wantMarker: "", wantInfo: "", wantOK: false},
			"空行は区切りでない":           {line: "", wantMarker: "", wantInfo: "", wantOK: false},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				marker, info, ok := fenceMarker(tt.line)
				assert.Equal(t, tt.wantOK, ok)
				assert.Equal(t, tt.wantMarker, marker)
				assert.Equal(t, tt.wantInfo, info)
			})
		}
	})
}

func TestInlineCodes(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			line string
			want []string
		}{
			"1つ取り出す":              {line: "本文 `make foo` 本文", want: []string{"make foo"}},
			"同じ行の複数を現れた順に返す":      {line: "`a` と `b`", want: []string{"a", "b"}},
			"前後の空白を落とす":           {line: "`` `foo` ``", want: []string{"`foo`"}},
			"長さが違う閉じでは閉じない":       {line: "``foo`", want: nil},
			"閉じが無ければ span を作らない":  {line: "`foo", want: nil},
			"バッククォートが無ければ空":       {line: "本文だけ", want: nil},
			"閉じの無い2連は span を作らない": {line: "a `` b", want: nil},
			"閉じた後の残りも走査する":        {line: "`a` `b` `c", want: []string{"a", "b"}},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, tt.want, InlineCodes(tt.line))
			})
		}
	})
}

func Test_findClose(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			line     string
			from     int
			want     int
			wantWant int
			wantOK   bool
		}{
			"同じ長さの連なりの位置を返す":    {line: "`a`", from: 1, wantWant: 1, want: 2, wantOK: true},
			"長さが違う連なりは飛ばす":      {line: "`a``b`", from: 1, wantWant: 1, want: 5, wantOK: true},
			"見つからなければ false":    {line: "`a", from: 1, wantWant: 1, wantOK: false},
			"長い連なりだけなら見つからない":   {line: "`a``", from: 1, wantWant: 1, wantOK: false},
			"2つ連なりの閉じも長さで一致させる": {line: "``a``", from: 2, wantWant: 2, want: 3, wantOK: true},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				got, ok := findClose(tt.line, tt.from, tt.wantWant)
				assert.Equal(t, tt.wantOK, ok)
				if tt.wantOK {
					assert.Equal(t, tt.want, got)
				}
			})
		}
	})
}

func Test_runLen(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			line string
			i    int
			want int
		}{
			"1つ":         {line: "`a", i: 0, want: 1},
			"3つ":         {line: "```", i: 0, want: 3},
			"途中から数える":    {line: "a``", i: 1, want: 2},
			"バッククォートでない": {line: "ab", i: 0, want: 0},
			"行末を超えない":    {line: "`", i: 0, want: 1},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, tt.want, runLen(tt.line, tt.i))
			})
		}
	})
}
