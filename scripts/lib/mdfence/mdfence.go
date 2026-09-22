// Package mdfence は、Markdown のコードフェンスを、囲む本文から長さを決めて組みます。
//
// フェンス長を固定にすると、本文にそれ以上のバッククォートが並んだとき本文側がフェンスを
// 閉じ、外の Markdown へ抜けられます。値の中身を決めるのは pull request なので、長さは
// 値の側から取ります。規則の所有は .github/workflows/README.md です。
//
// 同じ計算は .github/actions/upsert-pr-comment の JavaScript にも在り、両者が食い違わない
// ことを検査する機構はありません。片方だけ直した日に気づく手段が無いので、どちらかを触るなら
// もう片方も開くこと。
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
