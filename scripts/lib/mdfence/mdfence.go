// Package mdfence は、Markdown のコードフェンスを、囲む本文から長さを決めて組みます。
//
// フェンス長を固定にすると、本文にそれ以上のバッククォートが並んだとき本文側がフェンスを
// 閉じ、外の Markdown へ抜けられます。値の中身を決めるのは pull request なので、長さは
// 値の側から取ります。規則の所有は .github/workflows/README.md で、複製された実装が
// 互いに食い違うことは make pr-comment-fence-lint が検査します。
package mdfence

import (
	"fmt"
	"strings"
)

// minLen は CommonMark が定めるフェンスの下限です。
const minLen = 3

// For は、text を囲むのに足りる長さのフェンスを返します。
func For(text string) string {
	longest, run := 0, 0
	for _, r := range text {
		if r == '`' {
			run++
			if run > longest {
				longest = run
			}

			continue
		}
		run = 0
	}

	return strings.Repeat("`", max(minLen, longest+1))
}

// Section は、見出しと件数を付けた text フェンスを b へ書きます。
func Section(b *strings.Builder, heading string, lines []string) {
	body := strings.Join(lines, "\n")
	fence := For(body)
	fmt.Fprintf(b, "## %s (%d)\n\n%stext\n%s\n%s\n\n", heading, len(lines), fence, body, fence)
}
