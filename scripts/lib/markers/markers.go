// Package markers は、ファイルの中に置いた印を頭に、その区間を取り除く。
//
// 印はコメントの中に置く（`//`、`#`、`<!--` のあと）。名前空間ごとに次の形を持つ。
//
//	<ns>:begin … <ns>:end           印の行を含めて区間を消す。入れ子は深さで数える
//	<ns>:line                       その行を消す
//	<ns>:replace-begin … <ns>:replace-with … <ns>:replace-end
//	                                replace-with より前を消し、後ろの行を戻す
//
// replace-with の後ろの行は `= ` を付けたコメントで書く（`# = x`、`// = x`、`<!-- = x -->`）。
// 取り除くと、字下げを残してコメントと `= ` を外した行になる。replace-with は省いてよく、
// そのときは区間を消すだけになる。replace の区間は入れ子にできない。
//
// 名前空間に続く語が上のどれでもない印はエラーにする。綴りを誤った印は、黙って残ると
// 取り除いたはずの記述が残る。
package markers

import (
	"regexp"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var (
	// ErrUnknownMarker は、名前空間に続く語が既知の形でない印があることを表す。
	ErrUnknownMarker = xerrors.New("解釈できない印があります")
	// ErrUnbalanced は、begin と end、または replace の印の対応が取れていないことを表す。
	ErrUnbalanced = xerrors.New("印の対応が取れていません")
	// ErrNestedReplace は、replace の区間の中に replace-begin があることを表す。
	ErrNestedReplace = xerrors.New("replace の区間は入れ子にできません")
	// ErrBadReplacement は、replace-with の後ろの行が `= ` の形のコメントでないことを表す。
	ErrBadReplacement = xerrors.New("replace-with の後ろの行が `= ` の形ではありません")
)

// stashed は、replace-with の後ろの1行。字下げ、コメント、`= ` の順に並ぶ。
var stashed = regexp.MustCompile(`^([ \t]*)(?:(?://|#)[ \t]*=[ \t]?(.*)|<!--[ \t]*=[ \t]?(.*?)[ \t]*-->)$`)

// Markers は、1つの名前空間の印を扱う。
type Markers struct {
	ns string
	re *regexp.Regexp
	// loose は、大文字小文字やコメント記号の違いを問わずに名前空間を探す。re が拾わずに loose が
	// 拾う行は、綴りの揺れた印である。
	loose *regexp.Regexp
}

// New は、名前空間 ns の印を扱う Markers を返します。
func New(ns string) Markers {
	return Markers{
		ns:    ns,
		re:    regexp.MustCompile(`(?://|#|<!--)[ \t]*` + regexp.QuoteMeta(ns) + `:([A-Za-z0-9_-]*)`),
		loose: regexp.MustCompile(`(?i)` + regexp.QuoteMeta(ns) + `[ \t]*:[a-z]`),
	}
}

// Count は、content にある印の数を返します。解釈できない印があればエラーを返します。
func (m Markers) Count(content string) (int, error) {
	n := 0
	for i, line := range strings.Split(content, "\n") {
		_, ok, err := m.kind(line)
		if err != nil {
			return 0, xerrors.Wrapf(err, "%d 行目", i+1)
		}
		if ok {
			n++
		}
	}
	return n, nil
}

// Strip は、content から印の区間を取り除いた内容を返します。
//
// 印の対応が取れない、解釈できない印がある、replace-with の後ろの行が `= ` の形でない、の
// いずれかならエラーを返し、内容は返しません。エラーには行番号を付けます。
func (m Markers) Strip(content string) (string, error) {
	lines := strings.Split(content, "\n")
	// CRLF のファイルは行末の \r を外して解釈し、書き戻すときに付け直す。
	crlf := strings.Contains(content, "\r\n")
	if crlf {
		for i := range lines {
			lines[i] = strings.TrimSuffix(lines[i], "\r")
		}
	}
	out := make([]string, 0, len(lines))
	// seam は、直前に行を取り除いたことを表す。取り除いた跡に空行が2つ並ぶのを避ける。
	seam := false
	keep := func(line string) {
		if seam && strings.TrimSpace(line) == "" && len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
			return
		}
		seam = false
		out = append(out, line)
	}
	depth := 0
	// replace の区間の状態。0 は外、1 は replace-with より前、2 は後ろ。
	replace := 0
	replaceStart := 0
	blockStart := 0
	for i, line := range lines {
		no := i + 1
		kind, ok, err := m.kind(line)
		if err != nil {
			return "", xerrors.Wrapf(err, "%d 行目", no)
		}
		if replace != 0 {
			switch {
			case ok && kind == "replace-begin":
				return "", xerrors.Wrapf(ErrNestedReplace, "%d 行目", no)
			case ok && kind == "replace-with":
				if replace == 2 {
					return "", xerrors.Wrapf(ErrUnbalanced, "%d 行目: replace-with が2回あります", no)
				}
				replace = 2
			case ok && kind == "replace-end":
				replace = 0
			case ok:
				return "", xerrors.Wrapf(ErrUnbalanced, "%d 行目: replace の区間の中に %s があります", no, kind)
			case replace == 2:
				restored, err := restore(line)
				if err != nil {
					return "", xerrors.Wrapf(err, "%d 行目", no)
				}
				if depth == 0 {
					keep(restored)
				}
			default:
				seam = true
			}
			continue
		}
		switch {
		case ok && kind == "begin":
			seam = true
			if depth == 0 {
				blockStart = no
			}
			depth++
		case ok && kind == "end":
			if depth == 0 {
				return "", xerrors.Wrapf(ErrUnbalanced, "%d 行目: begin の無い end です", no)
			}
			depth--
		case ok && kind == "line":
			seam = true
		case ok && kind == "replace-begin":
			seam = true
			replace = 1
			replaceStart = no
		case ok:
			return "", xerrors.Wrapf(ErrUnbalanced, "%d 行目: replace-begin の無い %s です", no, kind)
		case depth == 0:
			keep(line)
		default:
			seam = true
		}
	}
	if replace != 0 {
		return "", xerrors.Wrapf(ErrUnbalanced, "%d 行目の replace-begin が閉じていません", replaceStart)
	}
	if depth != 0 {
		return "", xerrors.Wrapf(ErrUnbalanced, "%d 行目の begin が閉じていません", blockStart)
	}
	if crlf {
		return strings.Join(out, "\r\n"), nil
	}
	return strings.Join(out, "\n"), nil
}

// kind は、line にある印の語を返します。印が無ければ ok は false。
func (m Markers) kind(line string) (kind string, ok bool, err error) {
	matches := m.re.FindAllStringSubmatch(line, -1)
	if len(matches) == 0 {
		if m.loose.MatchString(line) {
			return "", false, xerrors.Wrapf(ErrUnknownMarker, "%s: の印の綴りが揺れています（小文字で、//・#・<!-- の直後に置く）", m.ns)
		}
		return "", false, nil
	}
	if len(matches) > 1 {
		return "", false, xerrors.Wrapf(ErrUnknownMarker, "1行に印が %d 個あります", len(matches))
	}
	switch k := matches[0][1]; k {
	case "begin", "end", "line", "replace-begin", "replace-with", "replace-end":
		return k, true, nil
	default:
		return "", false, xerrors.Wrapf(ErrUnknownMarker, "%s:%s", m.ns, k)
	}
}

// restore は、replace-with の後ろの1行からコメントと `= ` を外します。
func restore(line string) (string, error) {
	g := stashed.FindStringSubmatch(line)
	if g == nil {
		return "", ErrBadReplacement
	}
	if g[3] != "" || strings.HasPrefix(strings.TrimSpace(line), "<!--") {
		return g[1] + g[3], nil
	}
	return g[1] + g[2], nil
}
