// Package mdscan は、Markdown をコードフェンスと HTML コメントの外だけ走査し、inline code span を取り出します。
//
// 名指しした先が実在するかを検査する道具は、**例示と参照を区別できなければ成立しません**。
// フェンスの中に書かれた `make no-such-target` は使い方の例であって参照ではなく、そこを対象に
// 含めると、正しい文書が恒久的に落ちます。区別をフェンスの位置で機械的に付けるのがここです。
//
// フェンスを**組む**側は lib/mdfence が持ちます。こちらは**読む**側で、両者は同じ CommonMark の
// 規則（開きと同じ文字種で、開き以上の長さ）の裏表です。
//
// HTML コメントの外の行を返すのは VisibleLines と Scanner だけです。Lines はコメントの中も読みます。
package mdscan

import (
	"regexp"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	commentOpen  = "<!--"
	commentClose = "-->"
	// maxIndent は、フェンスの区切りが持てる字下げの空白の数です。4つ目からは字下げしたコードブロックです。
	maxIndent = 3
)

// ErrUnclosedComment は、HTML コメントが末尾まで閉じないことを表します。
var ErrUnclosedComment = xerrors.New("HTML コメントが閉じていません")

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
	var b Blocks
	for i, text := range strings.Split(string(content), "\n") {
		if !b.Next(text) {
			out = append(out, Line{Number: i + 1, Text: text})
		}
	}
	return out
}

// Blocks は、そこまでの行を読んだ時点のフェンスと、それを囲みうる一覧の項目の状態です。ゼロ値は文書の頭です。
// フェンスの区切りの字下げは、いちばん内側の項目の中身の桁から数えます（CommonMark）。引用（`>`）は容器として
// 読まず、`>` で始まる行はフェンスの区切りになりません。
type Blocks struct {
	items     []listItem
	fence     string
	fenceBase int
	paragraph bool
}

// listItem は開いている一覧の項目です。col は中身の桁、empty は項目が印だけの行で開き、まだ中身の行が無いかです。
type listItem struct {
	col   int
	empty bool
}

// Next は、text の1行を読み、text がフェンスの開始行・内部・終了行のどれかかを返します。
func (b *Blocks) Next(text string) bool {
	line := expandTabs(text)
	blank := strings.TrimSpace(line) == ""
	ind := indentOf(line)
	if b.fence != "" {
		if blank || ind >= b.fenceBase {
			// 長さを「以上」で見るのは、``` の例を ```` で囲む入れ子が成立するためです。
			if marker, info, ok := fenceMarker(line[min(b.fenceBase, ind):]); ok && marker[0] == b.fence[0] &&
				len(marker) >= len(b.fence) && strings.TrimSpace(info) == "" {
				b.fence = ""
			}
			return true
		}
		// 囲む項目が続かない行は、項目とともにフェンスを閉じる。
		b.fence = ""
	}
	if blank {
		if n := len(b.items); n > 0 && b.items[n-1].empty {
			b.items = b.items[:n-1]
		}
		b.paragraph = false
		return false
	}

	matched := 0
	for matched < len(b.items) && ind >= b.items[matched].col {
		b.items[matched].empty = false
		matched++
	}
	lazy := b.paragraph && matched < len(b.items)
	base := 0
	if matched > 0 {
		base = b.items[matched-1].col
	}
	for {
		rest := line[base:]
		rel := indentOf(rest)
		body := rest[rel:]
		allMatched := matched == len(b.items)
		switch {
		case rel > maxIndent:
			if !b.paragraph || (!allMatched && !lazy) {
				b.items, b.paragraph = b.items[:matched], false
			}
			return false
		case b.paragraph && allMatched && setextUnderline.MatchString(body):
			b.paragraph = false
			return false
		}
		if marker, _, ok := fenceMarker(rest); ok {
			b.items, b.paragraph = b.items[:matched], false
			b.fence, b.fenceBase = marker, base
			return true
		}
		if thematicBreak.MatchString(body) || interrupter.MatchString(body) {
			b.items, b.paragraph = b.items[:matched], false
			return false
		}
		if col, empty, ok := listMarker(body, base+rel); ok && (!b.paragraph || !allMatched || canInterrupt(body, empty)) {
			b.items = append(b.items[:matched], listItem{col: col, empty: empty})
			matched, base, lazy, b.paragraph = matched+1, col, false, false
			if empty {
				return false
			}
			continue
		}
		if !lazy {
			b.items = b.items[:matched]
		}
		b.paragraph = true
		return false
	}
}

var (
	listStart       = regexp.MustCompile(`^([-+*]|([0-9]{1,9})[.)])( *)(.*)$`)
	thematicBreak   = regexp.MustCompile(`^((\*[ \t]*){3,}|(-[ \t]*){3,}|(_[ \t]*){3,})$`)
	setextUnderline = regexp.MustCompile(`^(=+|-+)[ \t]*$`)
	// interrupter は、段落を遮り、遅延した続きの行にならない行の頭（ATX 見出し・引用・HTML ブロック）です。
	interrupter = regexp.MustCompile(`^(#{1,6}([ \t]|$)|>|<)`)
)

// listMarker は、body（字下げを除いた行）が一覧の項目を開くなら、中身の桁と、印の後ろが空かを返します。at は印の桁
// です。印の後ろの空白が5つ以上なら、中身は印の1つ後ろの桁からの字下げしたコードブロックです（CommonMark）。
func listMarker(body string, at int) (col int, empty, ok bool) {
	m := listStart.FindStringSubmatch(body)
	if m == nil {
		return 0, false, false
	}
	width, spaces, content := len(m[1]), len(m[3]), m[4]
	switch {
	case strings.TrimSpace(content) == "":
		return at + width + 1, true, true
	case spaces == 0:
		return 0, false, false
	case spaces > maxIndent+1:
		return at + width + 1, false, true
	}
	return at + width + spaces, false, true
}

// canInterrupt は、項目が段落を遮れるかを返します。空の項目と、1 以外で始まる番号付きの項目は遮りません。
func canInterrupt(body string, empty bool) bool {
	if empty {
		return false
	}
	m := listStart.FindStringSubmatch(body)
	return m[2] == "" || strings.TrimLeft(m[2], "0") == "1"
}

// expandTabs は、タブを4の倍数の桁まで空白で埋めます。
func expandTabs(text string) string {
	if !strings.Contains(text, "\t") {
		return text
	}
	var sb strings.Builder
	for i := 0; i < len(text); i++ {
		if text[i] != '\t' {
			sb.WriteByte(text[i])
			continue
		}
		sb.WriteString(strings.Repeat(" ", 4-sb.Len()%4))
	}
	return sb.String()
}

// indentOf は、行頭の空白の数を返します。
func indentOf(s string) int {
	return len(s) - len(strings.TrimLeft(s, " "))
}

// fenceMarker は、行がフェンスの区切りなら、そのマーカと後続の info string を返します。字下げが空白4つ以上
// （タブはタブ位置まで進めて数えるので、行頭の空白にタブを含めば4つ以上）の行と、info string にバッククォートを
// 含むバッククォートの行は区切りではありません（CommonMark）。
func fenceMarker(line string) (marker, info string, ok bool) {
	indent := 0
	for indent < len(line) && line[indent] == ' ' {
		indent++
	}
	if indent > maxIndent || strings.HasPrefix(line[indent:], "\t") {
		return "", "", false
	}
	trimmed := line[indent:]
	for _, c := range []byte{'`', '~'} {
		n := 0
		for n < len(trimmed) && trimmed[n] == c {
			n++
		}
		if n < 3 {
			continue
		}
		if c == '`' && strings.ContainsRune(trimmed[n:], '`') {
			return "", "", false
		}
		return trimmed[:n], trimmed[n:], true
	}
	return "", "", false
}

// VisibleLines は、content のうちコードフェンスの外にある行を、HTML コメントの部分を除いた文字で返します。
// コメントの中に収まる行は空の文字として返し、行の番号を保ちます。コメントが末尾まで閉じなければ、
// ErrUnclosedComment を、開いた行の番号を添えて返します。そのときも行は返し、開いた行より後ろはすべて
// コメントとして読んだものです。
func VisibleLines(content []byte) ([]Line, error) {
	var out []Line
	var s Scanner
	for i, text := range strings.Split(string(content), "\n") {
		l := Line{Number: i + 1, Text: text}
		if r := s.Next(l); !r.Fence {
			out = append(out, Line{Number: l.Number, Text: r.Visible})
		}
	}
	if at := s.Unclosed(); at != 0 {
		return out, xerrors.Wrapf(ErrUnclosedComment, "%d 行目で開いたコメント", at)
	}
	return out, nil
}

// Scanner は、フェンスと HTML コメントの状態を1行ずつ一緒に追います。CommonMark のとおり、フェンスの中の
// `<!--` はコメントを開かず、コメントの中のフェンスの区切りはフェンスを開きません。front matter のように
// 走査しない行は、Next に渡さずに飛ばします。
type Scanner struct {
	blocks   Blocks
	openedAt int
}

// Scanned は、Scanner.Next が読んだ1行です。
type Scanned struct {
	// Fence は、行がフェンスの開始行・内部・終了行のどれかかです。
	Fence bool
	// Comment は、行の頭で HTML コメントが開いていたかです。
	Comment bool
	// Visible は、行から HTML コメントの部分を除いた文字です。Fence なら空です。
	Visible string
}

// Next は、l の1行を読み、状態を進めます。
func (s *Scanner) Next(l Line) Scanned {
	if s.openedAt == 0 {
		if s.blocks.Next(l.Text) {
			return Scanned{Fence: true}
		}
	}
	inComment := s.openedAt != 0
	visible, open := StripComments(l.Text, inComment)
	switch {
	case !open:
		s.openedAt = 0
	case !inComment || strings.Contains(l.Text, commentClose):
		s.openedAt = l.Number
	}
	return Scanned{Comment: inComment, Visible: visible}
}

// Unclosed は、そこまでに読んだ行で開いたまま閉じていない HTML コメントを開いた行の番号を返します。閉じていれば
// 0 です。
func (s *Scanner) Unclosed() int {
	return s.openedAt
}

// StripComments は、1行から HTML コメントの部分を除いた文字と、行末でコメントが開いたままかを返します。open は、
// 行の頭でコメントが開いているかです。inline code の中の `<!--` と、バックスラッシュで逃がした `<` はコメントを
// 開きません。`<!-->` と `<!--->` は空のコメントです。
func StripComments(text string, open bool) (visible string, stillOpen bool) {
	var b strings.Builder
	for text != "" {
		if open {
			_, after, found := strings.Cut(text, commentClose)
			if !found {
				break
			}
			open, text = false, after
			continue
		}
		if text[0] == '`' {
			n, _ := CodeSpan(text)
			b.WriteString(text[:n])
			text = text[n:]
			continue
		}
		if text[0] == '\\' && len(text) > 1 {
			b.WriteString(text[:2])
			text = text[2:]
			continue
		}
		if rest, ok := strings.CutPrefix(text, commentOpen); ok {
			if empty, ok := cutEmptyComment(rest); ok {
				text = empty
				continue
			}
			open, text = true, rest
			continue
		}
		next := strings.IndexAny(text[1:], "`<\\")
		if next < 0 {
			b.WriteString(text)
			break
		}
		b.WriteString(text[:next+1])
		text = text[next+1:]
	}
	return b.String(), open
}

// cutEmptyComment は、`<!--` の直後の rest が `>` か `->` で始まる（`<!-->` か `<!--->` の）とき、その後ろを返します。
func cutEmptyComment(rest string) (string, bool) {
	for _, end := range []string{">", "->"} {
		if after, ok := strings.CutPrefix(rest, end); ok {
			return after, true
		}
	}
	return "", false
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

func runLen(line string, i int) int {
	n := 0
	for i+n < len(line) && line[i+n] == '`' {
		n++
	}
	return n
}
