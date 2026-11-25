// Package mdscan は、Markdown をコードフェンスの外だけ走査し、inline code span を取り出します。
//
// 名指しした先が実在するかを検査する道具は、**例示と参照を区別できなければ成立しません**。
// フェンスの中に書かれた `make no-such-target` は使い方の例であって参照ではなく、そこを対象に
// 含めると、正しい文書が恒久的に落ちます。区別をフェンスの位置で機械的に付けるのがここです。
//
// フェンスを**組む**側は lib/mdfence が持ちます。こちらは**読む**側で、両者は同じ CommonMark の
// 規則（開きと同じ文字種で、開き以上の長さ）の裏表です。
package mdscan

import "strings"

// Line は、コードフェンスの外にある行1つです。Number は1始まりで、その行を開くための番号です。
type Line struct {
	Number int
	Text   string
}

// Lines は、content のうちコードフェンスの外にある行を返します。
//
// フェンスの開始行・内部・終了行のいずれも返しません。開始行を返さないのは、info string
// （```go の go）が参照の形をしていても、それが言語名であって参照ではないためです。
func Lines(content []byte) []Line {
	var out []Line
	var fence Fence
	for i, text := range strings.Split(string(content), "\n") {
		var in bool
		if fence, in = fence.Next(text); !in {
			out = append(out, Line{Number: i + 1, Text: text})
		}
	}
	return out
}

// Fence は、そこまでの行を読んだ時点で開いているフェンスのマーカそのもの（``` や ~~~~）です。空ならフェンスの
// 外です。フェンスの外かどうかを別の状態（HTML コメントなど）と合わせて1行ずつ決める走査は、Lines ではなくこれを
// 使います。
type Fence string

// Next は、text の1行を読んだ後の状態と、text がフェンスの開始行・内部・終了行のどれかかを返します。
func (f Fence) Next(text string) (next Fence, in bool) {
	marker, info, ok := fenceMarker(text)
	switch {
	case f != "":
		// 閉じは、同じ文字種で、開き以上の長さで、後ろに info string を持たないもの。
		// 長さを「以上」で見るのは、``` の例を ```` で囲む入れ子が成立するためです。
		if ok && marker[0] == f[0] && len(marker) >= len(f) && strings.TrimSpace(info) == "" {
			return "", true
		}
		return f, true
	case ok:
		return Fence(marker), true
	default:
		return "", false
	}
}

// fenceMarker は、行がフェンスの区切りなら、そのマーカと後続の info string を返します。
func fenceMarker(line string) (marker, info string, ok bool) {
	trimmed := strings.TrimLeft(line, " \t")
	for _, c := range []byte{'`', '~'} {
		n := 0
		for n < len(trimmed) && trimmed[n] == c {
			n++
		}
		if n >= 3 {
			return trimmed[:n], trimmed[n:], true
		}
	}
	return "", "", false
}

// InlineCodes は、行に含まれる inline code span の中身を、現れた順に返します。
//
// 開きと閉じの長さが**同じ**ものだけを対にします（CommonMark）。これにより、中身に
// バッククォートを含む span（“ `foo` “ のような形）を1つとして取り出せます。閉じが
// 無いバッククォートの連なりはただの文字であり、span を作りません。
func InlineCodes(line string) []string {
	var out []string
	for i := 0; i < len(line); {
		if line[i] != '`' {
			i++
			continue
		}
		open := runLen(line, i)
		if closeAt, ok := findClose(line, i+open, open); ok {
			out = append(out, strings.TrimSpace(line[i+open:closeAt]))
			i = closeAt + open
			continue
		}
		i += open
	}
	return out
}

// CodeSpan は、s の先頭のバッククォートの並びが開く inline code span の、閉じの並びまでを含めた長さを返します。
// 閉じの規則は InlineCodes と同じで、長さの違う並びは読み飛ばします。閉じが無ければ、開きの並びの長さと false を
// 返します（その並びはただの文字です）。s がバッククォートで始まらなければ 0 と false です。
func CodeSpan(s string) (n int, closed bool) {
	open := runLen(s, 0)
	if open == 0 {
		return 0, false
	}
	if closeAt, ok := findClose(s, open, open); ok {
		return closeAt + open, true
	}
	return open, false
}

// findClose は、from 以降で、ちょうど want 個のバッククォートが並ぶ位置を返します。
func findClose(line string, from, want int) (int, bool) {
	for j := from; j < len(line); {
		if line[j] != '`' {
			j++
			continue
		}
		n := runLen(line, j)
		if n == want {
			return j, true
		}
		j += n
	}
	return 0, false
}

// runLen は、位置 i から続くバッククォートの個数を返します。
func runLen(line string, i int) int {
	n := 0
	for i+n < len(line) && line[i+n] == '`' {
		n++
	}
	return n
}
