package mdscan

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			"info string にバッククォートを含む行はフェンスを開かず、後ろの行も返す": {
				content: "```x``` 本文\n後ろ\n## 見出し",
				want:    []Line{{Number: 1, Text: "```x``` 本文"}, {Number: 2, Text: "後ろ"}, {Number: 3, Text: "## 見出し"}},
			},
			"空白4つで字下げした区切りはフェンスを開かず、後ろの行も返す": {
				content: "    ```\n後ろ\n## 見出し",
				want:    []Line{{Number: 1, Text: "    ```"}, {Number: 2, Text: "後ろ"}, {Number: 3, Text: "## 見出し"}},
			},
			"タブで字下げした区切りはフェンスを開かない": {
				content: "\t```\n後ろ",
				want:    []Line{{Number: 1, Text: "\t```"}, {Number: 2, Text: "後ろ"}},
			},
			"空白4つで字下げした区切りはフェンスを閉じない": {
				content: "```\n    ```\n中\n```\n外",
				want:    []Line{{Number: 5, Text: "外"}},
			},
			"番号付きの項目の中で、中身の桁から字下げしたフェンスを読む": {
				content: "11. 本文\n\n    ```text\n    中\n    ```\n\n12. 次",
				want:    []Line{{Number: 1, Text: "11. 本文"}, {Number: 2}, {Number: 6}, {Number: 7, Text: "12. 次"}},
			},
			"入れ子の項目の中のフェンスを読み、閉じた後の項目も返す": {
				content: "1. 外\n   1. 中\n\n      ```bash\n      make x\n      ```\n\n   2. 次\n\n      ```bash\n      make y\n      ```\n後ろ",
				want: []Line{{Number: 1, Text: "1. 外"}, {Number: 2, Text: "   1. 中"}, {Number: 3}, {Number: 7},
					{Number: 8, Text: "   2. 次"}, {Number: 9}, {Number: 13, Text: "後ろ"}},
			},
			"箇条書きの項目の中のフェンスを読む": {
				content: "- a\n  - b\n\n    ```\n    中\n    ```\n外",
				want:    []Line{{Number: 1, Text: "- a"}, {Number: 2, Text: "  - b"}, {Number: 3}, {Number: 7, Text: "外"}},
			},
			"一覧の外の字下げしたコードブロックの ``` はフェンスではない": {
				content: "本文\n\n    ```\n    中\n    ```\n後ろ",
				want: []Line{{Number: 1, Text: "本文"}, {Number: 2}, {Number: 3, Text: "    ```"}, {Number: 4, Text: "    中"},
					{Number: 5, Text: "    ```"}, {Number: 6, Text: "後ろ"}},
			},
			"項目が終わった後の空白4つの ``` はフェンスではない": {
				content: "- a\n\nb\n\n    ```\n中",
				want: []Line{{Number: 1, Text: "- a"}, {Number: 2}, {Number: 3, Text: "b"}, {Number: 4},
					{Number: 5, Text: "    ```"}, {Number: 6, Text: "中"}},
			},
			"10. の印は中身の桁を4にし、7つの字下げはフェンス": {
				content: "10. a\n\n       ```\n       中\n       ```\n外",
				want:    []Line{{Number: 1, Text: "10. a"}, {Number: 2}, {Number: 6, Text: "外"}},
			},
			"1. の印は中身の桁を3にし、7つの字下げはフェンスではない": {
				content: "1. a\n\n       ```\n中",
				want:    []Line{{Number: 1, Text: "1. a"}, {Number: 2}, {Number: 3, Text: "       ```"}, {Number: 4, Text: "中"}},
			},
			"項目の中身の桁より浅い行はフェンスを閉じ、後ろを返す": {
				content: "- a\n  ```\n  中\nb\n後ろ",
				want:    []Line{{Number: 1, Text: "- a"}, {Number: 4, Text: "b"}, {Number: 5, Text: "後ろ"}},
			},
			"遅延した続きの行は項目を閉じず、後ろの字下げしたフェンスを項目の中で読む": {
				content: "- a\n続き\n    ```\n    中\n    ```\n外",
				want:    []Line{{Number: 1, Text: "- a"}, {Number: 2, Text: "続き"}, {Number: 6, Text: "外"}},
			},
			"引用の中の ``` はフェンスではなく、後ろの行も返す": {
				content: "> ```\n> 中\n後ろ",
				want:    []Line{{Number: 1, Text: "> ```"}, {Number: 2, Text: "> 中"}, {Number: 3, Text: "後ろ"}},
			},
			"印だけの項目は直後の空行で終わる": {
				content: "-\n\n     ```\n中",
				want:    []Line{{Number: 1, Text: "-"}, {Number: 2}, {Number: 3, Text: "     ```"}, {Number: 4, Text: "中"}},
			},
			"段落の後の 1 以外で始まる番号は項目を開かない": {
				content: "本文\n2. b\n\n     ```\n中",
				want: []Line{{Number: 1, Text: "本文"}, {Number: 2, Text: "2. b"}, {Number: 3},
					{Number: 4, Text: "     ```"}, {Number: 5, Text: "中"}},
			},
			"段落の後の 1. は項目を開く": {
				content: "本文\n1. b\n\n     ```\n     中\n     ```\n外",
				want:    []Line{{Number: 1, Text: "本文"}, {Number: 2, Text: "1. b"}, {Number: 3}, {Number: 7, Text: "外"}},
			},
			"項目の中のフェンスの空行はフェンスを閉じない": {
				content: "- a\n\n  ```\n  x\n\n  y\n  ```\n外",
				want:    []Line{{Number: 1, Text: "- a"}, {Number: 2}, {Number: 8, Text: "外"}},
			},
			"区切り線は項目を閉じ、後ろの空白4つの ``` はフェンスではない": {
				content: "- a\n---\n\n    ```\n中",
				want:    []Line{{Number: 1, Text: "- a"}, {Number: 2, Text: "---"}, {Number: 3}, {Number: 4, Text: "    ```"}, {Number: 5, Text: "中"}},
			},
			"ATX 見出しは項目を閉じる": {
				content: "- a\n# h\n\n    ```\n中",
				want:    []Line{{Number: 1, Text: "- a"}, {Number: 2, Text: "# h"}, {Number: 3}, {Number: 4, Text: "    ```"}, {Number: 5, Text: "中"}},
			},
			"引用は項目を閉じる": {
				content: "- a\n> q\n\n    ```\n中",
				want:    []Line{{Number: 1, Text: "- a"}, {Number: 2, Text: "> q"}, {Number: 3}, {Number: 4, Text: "    ```"}, {Number: 5, Text: "中"}},
			},
			"種類1-6 の HTML ブロックは項目を閉じる": {
				content: "- a\n<div>\n\n    ```\n中",
				want:    []Line{{Number: 1, Text: "- a"}, {Number: 2, Text: "<div>"}, {Number: 3}, {Number: 4, Text: "    ```"}, {Number: 5, Text: "中"}},
			},
			"種類7 の HTML は遅延した続きの行で、項目の中のフェンスを読む": {
				content: "- a\n<b>x\n    ```\n    y\n    ```",
				want:    []Line{{Number: 1, Text: "- a"}, {Number: 2, Text: "<b>x"}},
			},
			"setext の下線は段落を終え、後ろの 2. は項目を開く": {
				content: "本文\n===\n2. b\n\n     ```\n中",
				want:    []Line{{Number: 1, Text: "本文"}, {Number: 2, Text: "==="}, {Number: 3, Text: "2. b"}, {Number: 4}, {Number: 6, Text: "中"}},
			},
			"印だけの項目は中身の行が続けば空行の後も続く": {
				content: "-\n  foo\n\n     ```\n中",
				want:    []Line{{Number: 1, Text: "-"}, {Number: 2, Text: "  foo"}, {Number: 3}, {Number: 5, Text: "中"}},
			},
			"段落の続きの字下げした行と遅延した行の後も項目は続く": {
				content: "- a\n      x\nlazy\n  ```\n中\n外",
				want:    []Line{{Number: 1, Text: "- a"}, {Number: 2, Text: "      x"}, {Number: 3, Text: "lazy"}, {Number: 5, Text: "中"}, {Number: 6, Text: "外"}},
			},
			"印の後ろの ``` は項目の中のフェンスを開く": {
				content: "- ```\n  中\n  ```\n外",
				want:    []Line{{Number: 4, Text: "外"}},
			},
			"1行に入れ子の印が並べば、内側の項目の中身の桁から数える": {
				content: "- - a\n\n    ```\n    中\n    ```\n外",
				want:    []Line{{Number: 1, Text: "- - a"}, {Number: 2}, {Number: 6, Text: "外"}},
			},
			"項目の外へ出る 2. は段落の中でも項目を開く": {
				content: "- a\n2. b\n\n      ```\n中",
				want:    []Line{{Number: 1, Text: "- a"}, {Number: 2, Text: "2. b"}, {Number: 3}, {Number: 5, Text: "中"}},
			},
			"項目の外の ``` は項目の中のフェンスを閉じ、新しいフェンスを開く": {
				content: "- a\n  ```\n  x\n```\n中\n```\n外",
				want:    []Line{{Number: 1, Text: "- a"}, {Number: 7, Text: "外"}},
			},
			"タブの後ろの箇条書きは中身の桁を4にする": {
				content: "-\ta\n\n\t```\n\t中\n\t```\n外",
				want:    []Line{{Number: 1, Text: "-\ta"}, {Number: 2}, {Number: 6, Text: "外"}},
			},
			"タブの後ろの番号付きは中身の桁を4にする": {
				content: "1.\ta\n\n\t```\n\t中\n\t```\n外",
				want:    []Line{{Number: 1, Text: "1.\ta"}, {Number: 2}, {Number: 6, Text: "外"}},
			},
			"中身の桁より1つ浅い行はフェンスを閉じ、その行を返す": {
				content: "- a\n  ```\n  中\n b\n後ろ",
				want:    []Line{{Number: 1, Text: "- a"}, {Number: 4, Text: " b"}, {Number: 5, Text: "後ろ"}},
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

func TestBlocks_Next(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		fence     string
		text      string
		wantFence string
		wantIn    bool
	}{
		"外の本文は外のまま":                {"", "本文", "", false},
		"外の区切りで開く":                 {"", "```go", "```", true},
		"字下げした区切りでも開く":             {"", "  ~~~", "~~~", true},
		"中の本文は中のまま":                {"```", "<!-- 本文", "```", true},
		"同じ文字種で同じ長さなら閉じる":          {"```", "```", "", true},
		"開きより長い閉じでも閉じる":            {"```", "`````", "", true},
		"開きより短い区切りでは閉じない":          {"````", "```", "````", true},
		"文字種が違えば閉じない":              {"```", "~~~", "```", true},
		"info string を持つ区切りでは閉じない": {"```", "```go", "```", true},
		"後ろの空白は閉じを妨げない":            {"```", "```   ", "", true},
		"後ろの CR は閉じを妨げない":          {"```", "```\r", "", true},
		"空白4つで字下げした区切りでは開かない":      {"", "    ```", "", false},
		"空白4つで字下げした区切りでは閉じない":      {"```", "    ```", "```", true},
		"空白3つで字下げした区切りでは閉じる":       {"```", "   ```", "", true},
		"タブで字下げした区切りでは開かない":        {"", "\t```", "", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b := Blocks{fence: c.fence}
			in := b.Next(c.text)
			assert.Equal(t, c.wantFence, b.fence)
			assert.Equal(t, c.wantIn, in)
		})
	}

	t.Run("1行に2万個並ぶ印も、行の長さに比例する手間ですべて項目として開く", func(t *testing.T) {
		t.Parallel()
		var b Blocks
		start := time.Now()
		in := b.Next(strings.Repeat("- ", 20000) + "x")
		assert.Less(t, time.Since(start), time.Second)
		assert.False(t, in)
		assert.Len(t, b.items, 20000)
		assert.True(t, b.paragraph)
	})

	for _, depth := range []int{33, 100} {
		t.Run(fmt.Sprintf("1行に %d 個並ぶ印の後ろの ``` はフェンスを開く", depth), func(t *testing.T) {
			t.Parallel()
			indent := strings.Repeat(" ", 2*depth)
			content := strings.Repeat("- ", depth) + "```\n" + indent + "中\n" + indent + "```\n外"
			assert.Equal(t, []Line{{Number: 4, Text: "外"}}, Lines([]byte(content)))
		})
	}
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
			"3つのバッククォートは区切り":     {line: "```", wantMarker: "```", wantInfo: "", wantOK: true},
			"info string を分けて返す": {line: "```go", wantMarker: "```", wantInfo: "go", wantOK: true},
			"4つ以上も区切り":           {line: "````", wantMarker: "````", wantInfo: "", wantOK: true},
			"チルダも区切り":            {line: "~~~", wantMarker: "~~~", wantInfo: "", wantOK: true},
			"先頭の空白は無視する":         {line: "  ```", wantMarker: "```", wantInfo: "", wantOK: true},
			"空白3つの字下げまでは区切り":     {line: "   ```", wantMarker: "```", wantInfo: "", wantOK: true},
			"空白4つの字下げは区切りでない":    {line: "    ```", wantMarker: "", wantInfo: "", wantOK: false},
			"タブの字下げは区切りでない":      {line: "\t```", wantMarker: "", wantInfo: "", wantOK: false},
			"空白の後のタブの字下げは区切りでない": {line: "   \t~~~", wantMarker: "", wantInfo: "", wantOK: false},
			"info string にバッククォートを含むバッククォートの行は区切りでない": {line: "```x``` 本文", wantMarker: "", wantInfo: "", wantOK: false},
			"チルダの区切りの info string はバッククォートを含んでよい":     {line: "~~~ `x`", wantMarker: "~~~", wantInfo: " `x`", wantOK: true},
			"2つでは区切りにならない":                            {line: "``x``", wantMarker: "", wantInfo: "", wantOK: false},
			"行の途中のバッククォートは区切りでない":                     {line: "text ```", wantMarker: "", wantInfo: "", wantOK: false},
			"空行は区切りでない":                               {line: "", wantMarker: "", wantInfo: "", wantOK: false},
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

func Test_lineView_thematicBreakAt(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		line string
		p    int
		want bool
	}{
		"3つの - は区切り線":          {"---", 0, true},
		"空白を挟んでもよい":            {"* * *", 0, true},
		"位置より後ろだけを見る":          {"x ___", 2, true},
		"2つでは区切り線でない":          {"- -", 0, false},
		"後ろに別の文字があれば区切り線でない":   {"- - - x", 0, false},
		"別の区切りの文字が混じれば区切り線でない": {"- * -", 0, false},
		"区切りの文字で始まらない":         {"a---", 0, false},
		"行末の位置":                {"---", 3, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			v := lineView{line: c.line}
			assert.Equal(t, c.want, v.thematicBreakAt(c.p))
		})
	}
}

func Test_listMarker(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		body      string
		at        int
		wantCol   int
		wantEmpty bool
		wantOK    bool
	}{
		"箇条書きは印と空白1つの後ろ":     {"- a", 0, 2, false, true},
		"印の桁を足す":             {"- a", 3, 5, false, true},
		"空白4つまでは中身の桁に含める":    {"-    a", 0, 5, false, true},
		"空白5つ以上は印の1つ後ろの桁":    {"-     a", 0, 2, false, true},
		"番号の桁数を印の幅に含める":      {"10. a", 0, 4, false, true},
		"閉じ括弧の番号":            {"1) a", 0, 3, false, true},
		"印だけの行は空の項目":         {"*", 0, 2, true, true},
		"印の後ろが空白だけでも空の項目":    {"+   ", 0, 2, true, true},
		"印の直後に空白が無ければ項目ではない": {"-a", 0, 0, false, false},
		"10桁の番号は項目ではない":      {"1234567890. a", 0, 0, false, false},
		"3桁の番号":              {"123. a", 0, 5, false, true},
		"9桁の番号":              {"123456789. a", 0, 11, false, true},
		"印で始まらない":            {"a", 0, 0, false, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			col, empty, ok := listMarker(c.body, c.at)
			assert.Equal(t, c.wantOK, ok)
			assert.Equal(t, c.wantCol, col)
			assert.Equal(t, c.wantEmpty, empty)
		})
	}
}

func Test_canInterrupt(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		body  string
		empty bool
		want  bool
	}{
		"箇条書きは遮る":          {"- a", false, true},
		"1 で始まる番号は遮る":      {"1. a", false, true},
		"0 を前に置いた 1 も遮る":   {"01. a", false, true},
		"1 以外で始まる番号は遮らない":  {"2. a", false, false},
		"空の項目は遮らない":        {"-", true, false},
		"10 で始まる番号は遮らない":   {"10. a", false, false},
		"0 を2つ前に置いた 1 も遮る": {"001. a", false, true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, canInterrupt(c.body, c.empty))
		})
	}
}

func Test_expandTabs(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		text string
		want string
	}{
		"タブが無ければそのまま":      {"a b", "a b"},
		"行頭のタブは4桁":         {"\tx", "    x"},
		"空白の後のタブは次の4の倍数まで": {"  \tx", "    x"},
		"桁4のタブは桁8まで":       {"-   \tx", "-       x"},
		"続くタブは4桁ずつ":        {"\t\tx", "        x"},
		"文字の間のタブも次の4の倍数まで": {"a\tb\tc", "a   b   c"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, expandTabs(c.text))
		})
	}
}

func Test_indentOf(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		s    string
		want int
	}{
		"字下げ無し":   {"a", 0},
		"空白を数える":  {"   a", 3},
		"空白だけの行":  {"  ", 2},
		"タブは数えない": {"\ta", 0},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, indentOf(c.s))
		})
	}
}

func TestVisibleLines(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			content string
			want    []Line
		}{
			"空の入力では1行の空文字を返す": {
				content: "",
				want:    []Line{{Number: 1}},
			},
			"複数行のコメントの間も項目は続き、後ろのフェンスを項目の中で読む": {
				content: "- a\n  <!--\n  x\n  -->\n\n  ```\n  中\n  ```\n外",
				want:    []Line{{Number: 1, Text: "- a"}, {Number: 2, Text: "  "}, {Number: 3}, {Number: 4}, {Number: 5}, {Number: 9, Text: "外"}},
			},
			"コメントもフェンスも無ければ全行を返す": {
				content: "一\n二",
				want:    []Line{{Number: 1, Text: "一"}, {Number: 2, Text: "二"}},
			},
			"1行のコメントは空の行になる": {
				content: "a\n<!-- ## 見出し -->\nb",
				want:    []Line{{Number: 1, Text: "a"}, {Number: 2, Text: ""}, {Number: 3, Text: "b"}},
			},
			"複数行のコメントの行はすべて空の行になる": {
				content: "a\n<!--\n## 見出し\n-->\nb",
				want:    []Line{{Number: 1, Text: "a"}, {Number: 2}, {Number: 3}, {Number: 4}, {Number: 5, Text: "b"}},
			},
			"同じ行で開いて閉じたコメントは、前後の文字を残す": {
				content: "前 <!-- x --> 後",
				want:    []Line{{Number: 1, Text: "前  後"}},
			},
			"閉じた後ろの文字を残す": {
				content: "<!-- a\nb --> c",
				want:    []Line{{Number: 1}, {Number: 2, Text: " c"}},
			},
			"inline code の中の <!-- はコメントを開かない": {
				content: "`<!--` は使わない\n## 見出し",
				want:    []Line{{Number: 1, Text: "`<!--` は使わない"}, {Number: 2, Text: "## 見出し"}},
			},
			"フェンスの中の <!-- はコメントを開かず、フェンスの行は返さない": {
				content: "```\n<!--\n```\n## 見出し",
				want:    []Line{{Number: 4, Text: "## 見出し"}},
			},
			"コメントの中のフェンスの区切りはフェンスを開かない": {
				content: "<!--\n```\n-->\n## 見出し",
				want:    []Line{{Number: 1}, {Number: 2}, {Number: 3}, {Number: 4, Text: "## 見出し"}},
			},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				got, err := VisibleLines([]byte(tt.content))
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			content string
			want    []Line
			wantAt  string
		}{
			"閉じないコメントはエラーにし、開いた行を名指す": {
				content: "a\n<!-- b\n## 見出し",
				want:    []Line{{Number: 1, Text: "a"}, {Number: 2}, {Number: 3}},
				wantAt:  "2 行目",
			},
			"同じ行で閉じた後に開き直して閉じなければエラーにする": {
				content: "<!-- a --> b <!-- c\nd",
				want:    []Line{{Number: 1, Text: " b "}, {Number: 2}},
				wantAt:  "1 行目",
			},
			"コメントの中で閉じて開き直せば、開き直した行を名指す": {
				content: "<!-- a\nb --> c <!-- d\ne",
				want:    []Line{{Number: 1}, {Number: 2, Text: " c "}, {Number: 3}},
				wantAt:  "2 行目",
			},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				got, err := VisibleLines([]byte(tt.content))
				require.ErrorIs(t, err, ErrUnclosedComment)
				assert.Contains(t, err.Error(), tt.wantAt)
				assert.Equal(t, tt.want, got)
			})
		}
	})
}

func TestScanner_Next(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		lines []string
		want  []Scanned
	}{
		"外の本文はコメントでもフェンスでもない": {
			[]string{"本文"},
			[]Scanned{{Visible: "本文"}},
		},
		"フェンスの行は Fence で、文字を返さない": {
			[]string{"```", "<!--", "```"},
			[]Scanned{{Fence: true}, {Fence: true}, {Fence: true}},
		},
		"行の頭でコメントが開いていれば Comment": {
			[]string{"<!-- a", "b", "c -->d"},
			[]Scanned{{}, {Comment: true}, {Comment: true, Visible: "d"}},
		},
		"コメントの中の区切りはフェンスを開かず、閉じた後の区切りは開く": {
			[]string{"<!--", "```", "-->", "```"},
			[]Scanned{{}, {Comment: true}, {Comment: true}, {Fence: true}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var s Scanner
			got := make([]Scanned, 0, len(c.lines))
			for i, text := range c.lines {
				got = append(got, s.Next(Line{Number: i + 1, Text: text}))
			}
			assert.Equal(t, c.want, got)
		})
	}
}

func TestScanner_Unclosed(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		lines []Line
		want  int
	}{
		"何も読まなければ 0":         {nil, 0},
		"閉じれば 0":             {[]Line{{1, "<!-- a"}, {2, "b -->"}}, 0},
		"開いた行の番号を返す":         {[]Line{{1, "a"}, {7, "<!-- b"}, {8, "c"}}, 7},
		"閉じて開き直せば、開き直した行を返す": {[]Line{{1, "<!-- a"}, {2, "b --> <!-- c"}}, 2},
		"フェンスの中の開きは数えない":     {[]Line{{1, "```"}, {2, "<!--"}}, 0},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var s Scanner
			for _, l := range c.lines {
				s.Next(l)
			}
			assert.Equal(t, c.want, s.Unclosed())
		})
	}
}

func Test_stripComments(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		text      string
		open      bool
		want      string
		stillOpen bool
	}{
		"コメントが無ければそのまま":                        {"a", false, "a", false},
		"1行のコメントは空になる":                         {"<!-- a -->", false, "", false},
		"行末のコメントを外す":                           {"- A <!-- note -->", false, "- A ", false},
		"開いたままの行は空になる":                         {"## x", true, "", true},
		"閉じた後ろの文字は残す":                          {"b --> c", true, " c", false},
		"1行に2つのコメント":                           {"a<!-- x -->b<!-- y -->c", false, "abc", false},
		"inline code の中の開きは開かない":               {"`<!--` a", false, "`<!--` a", false},
		"閉じない inline code の後ろは開く":              {"`a <!-- b", false, "`a ", true},
		"閉じなければ開いたままを返す":                       {"a <!-- b", false, "a ", true},
		"長さの違う並びを読み飛ばして inline code を閉じる":      {"`a``b<!-- c`", false, "`a``b<!-- c`", false},
		"長さの違う閉じは inline code を閉じない":           {"`a``<!-- b", false, "`a``", true},
		"2つ以上のバッククォートの inline code の中の開きは開かない": {"``a`<!--``", false, "``a`<!--``", false},
		"閉じた同じ行で開き直す":                          {"<!-- a --> b <!-- c", false, " b ", true},
		"コメントでない < の後ろの開きは開く":                  {"a <div> <!-- x", false, "a <div> ", true},
		"<! で始まるがコメントでないものは文字":                 {"a <div> <!x> b", false, "a <div> <!x> b", false},
		"バックスラッシュで逃がした < は開かない":                {"\\<!-- x", false, "\\<!-- x", false},
		"逃がしたバックスラッシュの後ろの < は開く":               {"\\\\<!-- x", false, "\\\\", true},
		"逃がしたバッククォートは inline code を開かない":       {"\\`<!-- x`", false, "\\`", true},
		"<!--> は空のコメント":                        {"a<!-->b", false, "ab", false},
		"<!---> は空のコメント":                       {"a<!--->b", false, "ab", false},
		"<!----> は閉じたコメント":                     {"a<!---->b", false, "ab", false},
		"行末のバックスラッシュは文字":                       {"a\\", false, "a\\", false},
		"バックスラッシュだけの行":                         {"\\", false, "\\", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, stillOpen := stripComments(c.text, c.open)
			assert.Equal(t, c.want, got)
			assert.Equal(t, c.stillOpen, stillOpen)
		})
	}
}

func Test_cutEmptyComment(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		rest string
		want string
		ok   bool
	}{
		"> で閉じる":          {">b", "b", true},
		"-> で閉じる":         {"->b", "b", true},
		"--> は空のコメントではない": {"-->b", "", false},
		"本文が続く":           {" a -->", "", false},
		"空":               {"", "", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, ok := cutEmptyComment(c.rest)
			assert.Equal(t, c.want, got)
			assert.Equal(t, c.ok, ok)
		})
	}
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

func TestCodeSpan(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		s      string
		n      int
		closed bool
	}{
		"同じ長さの閉じまでを返す":      {"`a` b", 3, true},
		"長さの違う並びを読み飛ばして閉じる": {"`a``b` c", 6, true},
		"2つの並びは2つの並びで閉じる":   {"``a`b`` c", 7, true},
		"閉じが無ければ開きの並びだけを返す": {"`a``b", 1, false},
		"バッククォートで始まらなければ0":  {"a`b`", 0, false},
		"空": {"", 0, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			n, closed := CodeSpan(c.s)
			assert.Equal(t, c.n, n)
			assert.Equal(t, c.closed, closed)
		})
	}
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
