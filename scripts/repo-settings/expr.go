package main

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// Actions の式（docs.github.com の Expressions）を字句と構文として読み、ブランチの名前か ref を値に持つ値を、
// 式の中で作った文字列と比べる項を探す。文法は Expressions の演算子・関数・リテラルで閉じており、読めないものは
// errExpression で落とす（ADR-0702 決定14）。

type tokenKind int

const (
	tokIdent tokenKind = iota
	tokString
	tokNumber
	tokKeyword // true / false / null
	tokPunct
)

type exprToken struct {
	kind       tokenKind
	text       string // tokString は引用符を外した値。ほかは字面
	start, end int
}

var (
	exprNumber = regexp.MustCompile(`^-?(?:0[xX][0-9a-fA-F]+|[0-9]+(?:\.[0-9]*)?(?:[eE][-+]?[0-9]+)?)`)
	exprIdent  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*`)
	// exprPuncts は長いものを先に置く。`!=` を `!` と `=` に、`<=` を `<` と `=` に分けない。
	exprPuncts = []string{"==", "!=", "<=", ">=", "&&", "||", "<", ">", "!", "(", ")", "[", "]", ".", ",", "*"}
)

// lexExpr は、式の本文（`${{ }}` を外したもの）を字句の列にします。文字列リテラルは `'` で囲み、`'` を2つ重ねて
// `'` を表す。識別子は `-` を含んでよい。未知の文字と閉じない文字列リテラルは失敗する。
func lexExpr(src string) ([]exprToken, error) {
	var out []exprToken
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '\'':
			var b strings.Builder
			j := i + 1
			for {
				if j >= len(src) {
					return nil, xerrors.Wrapf(errExpression, "%d 文字目からの文字列リテラルが閉じていません", i+1)
				}
				if src[j] == '\'' {
					if j+1 < len(src) && src[j+1] == '\'' {
						b.WriteByte('\'')
						j += 2
						continue
					}
					break
				}
				b.WriteByte(src[j])
				j++
			}
			out = append(out, exprToken{kind: tokString, text: b.String(), start: i, end: j + 1})
			i = j + 1
		case exprNumber.MatchString(src[i:]):
			m := exprNumber.FindString(src[i:])
			out = append(out, exprToken{kind: tokNumber, text: m, start: i, end: i + len(m)})
			i += len(m)
		case exprIdent.MatchString(src[i:]):
			m := exprIdent.FindString(src[i:])
			kind := tokIdent
			switch strings.ToLower(m) {
			case "true", "false", "null":
				kind = tokKeyword
			}
			out = append(out, exprToken{kind: kind, text: m, start: i, end: i + len(m)})
			i += len(m)
		default:
			p := ""
			for _, q := range exprPuncts {
				if strings.HasPrefix(src[i:], q) {
					p = q
					break
				}
			}
			if p == "" {
				return nil, xerrors.Wrapf(errExpression, "%d 文字目の %q は式の字句ではありません", i+1, string(c))
			}
			out = append(out, exprToken{kind: tokPunct, text: p, start: i, end: i + len(p)})
			i += len(p)
		}
	}
	return out, nil
}

type nodeKind int

const (
	nodeLiteral nodeKind = iota
	nodeIdent
	nodeMember // x.name、x.*
	nodeIndex  // x[i]
	nodeCall
	nodeNot
	nodeBinary
)

// exprNode は式の構文木の節です。args は子で、nodeMember と nodeNot は [x]、nodeIndex は [x, i]、nodeCall は
// 引数、nodeBinary は [左, 右] である。
type exprNode struct {
	kind nodeKind
	// op は nodeBinary の演算子と、nodeCall の関数名（小文字）。
	op string
	// text は nodeIdent と nodeMember の名前と、nodeLiteral の値。
	text       string
	litKind    tokenKind
	args       []*exprNode
	start, end int
}

// exprFunctions は、式が呼べる関数（小文字）と引数の数の下限と上限（-1 は上限なし）。
var exprFunctions = map[string][2]int{
	"contains": {2, 2}, "startswith": {2, 2}, "endswith": {2, 2},
	"format": {1, -1}, "join": {1, 2}, "tojson": {1, 1}, "fromjson": {1, 1}, "hashfiles": {1, -1}, "case": {3, -1},
	"success": {0, 0}, "always": {0, 0}, "cancelled": {0, 0}, "failure": {0, 0},
}

// binaryPrec は二項演算子の結合の強さ。大きいほど強い。
var binaryPrec = map[string]int{"||": 1, "&&": 2, "==": 3, "!=": 3, "<": 4, "<=": 4, ">": 4, ">=": 4}

type exprParser struct {
	toks []exprToken
	pos  int
}

// parseExpr は、式の本文を構文木にします。空の式と、読み残した字句があるものは失敗する。
func parseExpr(src string) (*exprNode, error) {
	toks, err := lexExpr(src)
	if err != nil {
		return nil, err
	}
	if len(toks) == 0 {
		return nil, xerrors.Wrap(errExpression, "式が空です")
	}
	p := &exprParser{toks: toks}
	n, err := p.parseBinary(1)
	if err != nil {
		return nil, err
	}
	if p.pos < len(toks) {
		return nil, p.unexpected()
	}
	return n, nil
}

func (p *exprParser) peek(text string) bool {
	return p.pos < len(p.toks) && p.toks[p.pos].kind == tokPunct && p.toks[p.pos].text == text
}

func (p *exprParser) expect(text string) (exprToken, error) {
	if !p.peek(text) {
		return exprToken{}, p.unexpected()
	}
	t := p.toks[p.pos]
	p.pos++
	return t, nil
}

func (p *exprParser) unexpected() error {
	if p.pos >= len(p.toks) {
		return xerrors.Wrap(errExpression, "式が途中で終わっています")
	}
	t := p.toks[p.pos]
	return xerrors.Wrapf(errExpression, "%d 文字目の %q を読めません", t.start+1, t.text)
}

// parseBinary は、結合の強さが minPrec 以上の二項演算子の連なりを、左結合で読みます。
func (p *exprParser) parseBinary(minPrec int) (*exprNode, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.pos < len(p.toks) && p.toks[p.pos].kind == tokPunct {
		op := p.toks[p.pos].text
		prec, ok := binaryPrec[op]
		if !ok || prec < minPrec {
			break
		}
		p.pos++
		right, err := p.parseBinary(prec + 1)
		if err != nil {
			return nil, err
		}
		left = &exprNode{kind: nodeBinary, op: op, args: []*exprNode{left, right}, start: left.start, end: right.end}
	}
	return left, nil
}

func (p *exprParser) parseUnary() (*exprNode, error) {
	if p.peek("!") {
		t := p.toks[p.pos]
		p.pos++
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &exprNode{kind: nodeNot, args: []*exprNode{x}, start: t.start, end: x.end}, nil
	}
	return p.parsePostfix()
}

// parsePostfix は、値に続く `.名前` / `.*` / `[式]` を読みます。
func (p *exprParser) parsePostfix() (*exprNode, error) {
	x, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for {
		switch {
		case p.peek("."):
			p.pos++
			if p.pos >= len(p.toks) {
				return nil, p.unexpected()
			}
			t := p.toks[p.pos]
			// `.` の後ろの名前は context の property であり、true / null も名前として読む。
			if t.kind != tokIdent && t.kind != tokKeyword && (t.kind != tokPunct || t.text != "*") {
				return nil, p.unexpected()
			}
			p.pos++
			x = &exprNode{kind: nodeMember, text: t.text, args: []*exprNode{x}, start: x.start, end: t.end}
		case p.peek("["):
			p.pos++
			i, err := p.parseBinary(1)
			if err != nil {
				return nil, err
			}
			t, err := p.expect("]")
			if err != nil {
				return nil, err
			}
			x = &exprNode{kind: nodeIndex, args: []*exprNode{x, i}, start: x.start, end: t.end}
		default:
			return x, nil
		}
	}
}

// parsePrimary は、リテラル、context の名前、関数の呼び出し、括弧の式を読みます。関数は [exprFunctions] に
// 在るものだけを、その引数の数で受け付ける。
func (p *exprParser) parsePrimary() (*exprNode, error) {
	if p.pos >= len(p.toks) {
		return nil, p.unexpected()
	}
	t := p.toks[p.pos]
	switch t.kind {
	case tokString, tokNumber, tokKeyword:
		p.pos++
		return &exprNode{kind: nodeLiteral, text: t.text, litKind: t.kind, start: t.start, end: t.end}, nil
	case tokIdent:
		p.pos++
		if !p.peek("(") {
			return &exprNode{kind: nodeIdent, text: t.text, start: t.start, end: t.end}, nil
		}
		name := strings.ToLower(t.text)
		arity, ok := exprFunctions[name]
		if !ok {
			return nil, xerrors.Wrapf(errExpression, "%d 文字目の関数 %s は Actions の式にありません", t.start+1, t.text)
		}
		p.pos++
		var args []*exprNode
		if !p.peek(")") {
			for {
				a, err := p.parseBinary(1)
				if err != nil {
					return nil, err
				}
				args = append(args, a)
				if !p.peek(",") {
					break
				}
				p.pos++
			}
		}
		end, err := p.expect(")")
		if err != nil {
			return nil, err
		}
		if len(args) < arity[0] || (arity[1] >= 0 && len(args) > arity[1]) || (name == "case" && len(args)%2 == 0) {
			return nil, xerrors.Wrapf(errExpression, "%d 文字目の %s の引数の数（%d）が合いません", t.start+1, t.text, len(args))
		}
		return &exprNode{kind: nodeCall, op: name, args: args, start: t.start, end: end.end}, nil
	case tokPunct:
		if t.text == "(" {
			p.pos++
			x, err := p.parseBinary(1)
			if err != nil {
				return nil, err
			}
			end, err := p.expect(")")
			if err != nil {
				return nil, err
			}
			// 字面で返す項が括弧の片側だけを含まないよう、括弧を節の範囲に含める。
			x.start, x.end = t.start, end.end
			return x, nil
		}
	}
	return nil, p.unexpected()
}

// refSegment は、ブランチの名前か ref を値に持つ property の名前を、単語を `_` で区切った小文字で表す。
// ref_type・ref_protected・branches_url のように名前の末尾が違うものは当たらない。
var refSegment = regexp.MustCompile(`^(?:(?:[a-z0-9]+_)*(?:ref|branch|branches)|ref_name|target_commitish)$`)

var (
	// camelBoundary は、camelCase の単語の境目（小文字か数字の直後の大文字）。
	camelBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)
	// acronymBoundary は、大文字の連なりの後ろの単語の境目（HEADRef の D と R の間）。
	acronymBoundary = regexp.MustCompile(`([A-Z]+)([A-Z][a-z])`)
)

// isRefSegment は、区間の名前が [refSegment] に当たるかを判定します。camelCase の境目と `-` を `_` へ寄せ、
// 大文字小文字を区別せずに比べる（targetBranch・target-branch・TARGET_BRANCH は同じ名前）。
func isRefSegment(name string) bool {
	snake := camelBoundary.ReplaceAllString(acronymBoundary.ReplaceAllString(name, "${1}_${2}"), "${1}_${2}")
	return refSegment.MatchString(strings.ReplaceAll(strings.ToLower(snake), "-", "_"))
}

// isRefRoot は、ref を値に持つ値を探す context の根（github と inputs）かを判定します。
func isRefRoot(name string) bool {
	return strings.EqualFold(name, "github") || strings.EqualFold(name, "inputs")
}

// contextPath は、n が context の根から `.名前` と添字だけで続くパスなら、その根と区間を返します。区間は名前、
// 文字列リテラルの添字はその値、数の添字と `.*` は空文字列である。リテラルでない添字を含むと ok は false。
func contextPath(n *exprNode) (root string, segs []string, ok bool) {
	for {
		switch n.kind {
		case nodeIdent:
			for l, r := 0, len(segs)-1; l < r; l, r = l+1, r-1 {
				segs[l], segs[r] = segs[r], segs[l]
			}
			return n.text, segs, true
		case nodeMember:
			if n.text == "*" {
				segs = append(segs, "")
			} else {
				segs = append(segs, n.text)
			}
		case nodeIndex:
			i := n.args[1]
			if i.kind != nodeLiteral || (i.litKind != tokString && i.litKind != tokNumber) {
				return "", nil, false
			}
			if i.litKind == tokString {
				segs = append(segs, i.text)
			} else {
				segs = append(segs, "")
			}
		default:
			return "", nil, false
		}
		n = n.args[0]
	}
}

// refPath は、根 root から区間 segs で続くパスが、ブランチの名前か ref を値に持つかを判定します。区間のどれかが
// [isRefSegment] に当たれば、その下を含めて ref とする（github.action_ref は実行中の action の版であり除く）。
// base.label / head.label で終わるパスも ref とする。
// context そのもの（github と inputs）と github.event は ref を含む値として扱う。serialized は toJSON の
// 引数の中であることで、そのとき github と inputs の下のパスはすべて、ref を含みうる値として扱う。
func refPath(root string, segs []string, serialized bool) bool {
	if !isRefRoot(root) {
		return false
	}
	if serialized || len(segs) == 0 {
		return true
	}
	// pull_request の base / head の label は `<owner>:<branch>` である。
	if n := len(segs); n >= 2 && strings.EqualFold(segs[n-1], "label") && (strings.EqualFold(segs[n-2], "base") || strings.EqualFold(segs[n-2], "head")) {
		return true
	}
	if strings.EqualFold(root, "github") && len(segs) == 1 && strings.EqualFold(segs[0], "event") {
		return true
	}
	for i, s := range segs {
		if i == 0 && strings.EqualFold(root, "github") && strings.EqualFold(s, "action_ref") {
			continue
		}
		if isRefSegment(s) {
			return true
		}
	}
	return false
}

// refDerived は、n の値がブランチの名前か ref に由来しうるかを判定します。[refPath] に当たるパスと、それを
// format / join / toJSON / fromJSON の引数に含む呼び出し、それを結果の引数に持つ case、それをオペランドに持つ
// `&&` / `||`（オペランドの値を返す）である。
func refDerived(n *exprNode, serialized bool) bool {
	switch n.kind {
	case nodeIdent, nodeMember, nodeIndex:
		// パスでない値（呼び出しから続く区間、リテラルでない添字）は、その手前の値で決める。名前の節は常にパスである。
		if root, segs, ok := contextPath(n); ok {
			return refPath(root, segs, serialized)
		}
		return refDerived(n.args[0], serialized)
	case nodeCall:
		switch n.op {
		case "format", "join", "fromjson", "tojson":
			for _, a := range n.args {
				if refDerived(a, serialized || n.op == "tojson") {
					return true
				}
			}
		case "case":
			for _, a := range caseResults(n) {
				if refDerived(a, serialized) {
					return true
				}
			}
		}
	case nodeBinary:
		if n.op == "&&" || n.op == "||" {
			return refDerived(n.args[0], serialized) || refDerived(n.args[1], serialized)
		}
	}
	return false
}

// constString は、n の値が式の中で作った空でない文字列でありうるかを判定します。空でない文字列リテラルと、
// 引数がリテラルとその呼び出しだけで文字列リテラルを含む format / join / toJSON / fromJSON、それを結果の引数に
// 持つ case、それをオペランドに持つ `&&` / `||`、それから `.名前` かリテラルの添字で続く値である。空文字列は
// ブランチの名前になりえないので含めない。
func constString(n *exprNode) bool {
	switch n.kind {
	case nodeLiteral:
		return n.litKind == tokString && n.text != ""
	case nodeMember:
		return constString(n.args[0])
	case nodeIndex:
		return n.args[1].kind == nodeLiteral && constString(n.args[0])
	case nodeCall:
		switch n.op {
		case "format", "join", "fromjson", "tojson":
			return literalOnly(n) && hasStringLiteral(n)
		case "case":
			for _, a := range caseResults(n) {
				if constString(a) {
					return true
				}
			}
		}
	case nodeBinary:
		if n.op == "&&" || n.op == "||" {
			return constString(n.args[0]) || constString(n.args[1])
		}
	}
	return false
}

// caseResults は、case(述語, 値, ..., 既定値) の結果になる引数を返します。
func caseResults(n *exprNode) []*exprNode {
	out := make([]*exprNode, 0, len(n.args)/2+1)
	for i := 1; i < len(n.args); i += 2 {
		out = append(out, n.args[i])
	}
	return append(out, n.args[len(n.args)-1])
}

// literalOnly は、n の値がリテラルと、引数がリテラルだけの format / join / toJSON / fromJSON から成るかを
// 判定します。
func literalOnly(n *exprNode) bool {
	switch n.kind {
	case nodeLiteral:
		return true
	case nodeCall:
		switch n.op {
		case "format", "join", "fromjson", "tojson":
			for _, a := range n.args {
				if !literalOnly(a) {
					return false
				}
			}
			return true
		}
	}
	return false
}

// hasStringLiteral は、n が空でない文字列リテラルを含むかを判定します。空文字列リテラルだけから作った値は
// 空文字列と同じく、ブランチの名前と比べる定数に数えない。
func hasStringLiteral(n *exprNode) bool {
	if n.kind == nodeLiteral {
		return n.litKind == tokString && n.text != ""
	}
	for _, a := range n.args {
		if hasStringLiteral(a) {
			return true
		}
	}
	return false
}

// exprCounts は、式の中の、根が github か inputs の context の名前、`.名前` / `.*`、添字、比較の演算子、
// startsWith / endsWith / contains の呼び出しの件数。
type exprCounts struct {
	roots, members, indexes, compares, predicates int
}

// walkExpr は、構文木のすべての節を辿り、比較の演算子と startsWith / endsWith / contains の呼び出しのうち、
// 片側が [refDerived] でもう片側が [constString] であるものと、[constString] の値を [refDerived] の添字で引く
// 項（fromJSON の表を ref で引くのは比較と同じ）を、src の中の字面で返します。辿った節の件数も返す。根が
// github か inputs のパスの添字がリテラルでなければ失敗する —— 添字の先が ref かを決められない。
func walkExpr(root *exprNode, src string) ([]string, exprCounts, error) {
	var (
		found []string
		c     exprCounts
		visit func(n *exprNode) error
	)
	pair := func(n, a, b *exprNode) {
		if (refDerived(a, false) && constString(b)) || (refDerived(b, false) && constString(a)) {
			found = append(found, src[n.start:n.end])
		}
	}
	visit = func(n *exprNode) error {
		switch n.kind {
		case nodeIdent:
			if isRefRoot(n.text) {
				c.roots++
			}
		case nodeMember:
			c.members++
		case nodeIndex:
			c.indexes++
			base := n.args[0]
			for base.kind == nodeMember || base.kind == nodeIndex {
				base = base.args[0]
			}
			i := n.args[1]
			if base.kind == nodeIdent && isRefRoot(base.text) && (i.kind != nodeLiteral || (i.litKind != tokString && i.litKind != tokNumber)) {
				return xerrors.Wrapf(errExpression, "%s の添字がリテラルではありません。添字の先が ref かを決められません", src[n.start:n.end])
			}
			if constString(n.args[0]) && refDerived(i, false) {
				found = append(found, src[n.start:n.end])
			}
		case nodeCall:
			if n.op == "contains" || n.op == "startswith" || n.op == "endswith" {
				c.predicates++
				pair(n, n.args[0], n.args[1])
			}
		case nodeBinary:
			if binaryPrec[n.op] >= binaryPrec["=="] {
				c.compares++
				pair(n, n.args[0], n.args[1])
			}
		}
		for _, a := range n.args {
			if err := visit(a); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(root); err != nil {
		return nil, exprCounts{}, err
	}
	return found, c, nil
}

var (
	literalRe    = regexp.MustCompile(`'(?:[^']|'')*'`)
	rootWordRe   = regexp.MustCompile(`(?i)github|inputs`)
	memberRe     = regexp.MustCompile(`\.\s*[A-Za-z_*]`)
	compareRe    = regexp.MustCompile(`==|!=|<=|>=|<|>`)
	predicateRe  = regexp.MustCompile(`(?i)\b(?:startsWith|endsWith|contains)\s*\(`)
	identCharRe  = regexp.MustCompile(`[A-Za-z0-9_-]`)
	exprOpenWrap = "${{"
)

// literalCounts は、[walkExpr] と同じ [exprCounts] を、構文木を経ずに字面から数えます（ADR-0702 決定15）。
// 守るのは字句解析と木の走査が節を落とさないことで、ref を見分ける規則そのものの誤りは守らない。
func literalCounts(src string) exprCounts {
	s := literalRe.ReplaceAllString(src, "''")
	var c exprCounts
	for _, m := range rootWordRe.FindAllStringIndex(s, -1) {
		before := strings.TrimRight(s[:m[0]], " \t\r\n")
		if before != "" && (identCharRe.MatchString(before[len(before)-1:]) || strings.HasSuffix(before, ".")) {
			continue
		}
		if m[1] < len(s) && identCharRe.MatchString(s[m[1]:m[1]+1]) {
			continue
		}
		c.roots++
	}
	c.members = len(memberRe.FindAllString(s, -1))
	c.indexes = strings.Count(s, "[")
	c.compares = len(compareRe.FindAllString(s, -1))
	c.predicates = len(predicateRe.FindAllString(s, -1))
	return c
}

// countMismatch は、構文木を辿った件数 read と字面から数えた件数 literal が食い違えば失敗します。
func countMismatch(read, literal exprCounts) error {
	if read == literal {
		return nil
	}
	f := func(c exprCounts) string {
		return fmt.Sprintf("（根 %d・区間 %d・添字 %d・比較 %d・述語 %d）", c.roots, c.members, c.indexes, c.compares, c.predicates)
	}
	return xerrors.Wrapf(errExpression, "構文木から読んだ件数%sが、字面から数えた件数%sと食い違います。読めない書き方があります", f(read), f(literal))
}

// exprBody は、[expressions] が返す式の字面から、式の本文を取り出します。引用符の値は YAML の文字列として
// 読む。`${{ }}` で値の全体を包んだものは中身を、包みの無いもの（素の if: の値）はそのまま返す。包みの外に
// 字面が残る値（`${{ a }} == 'x'` や `a && ${{ b }}`）は、if: が文字列の補間として評価するので失敗する。
func exprBody(text string) (string, error) {
	t := strings.TrimSpace(text)
	if strings.HasPrefix(t, `"`) || strings.HasPrefix(t, "'") {
		// 文書の先頭のスカラーとして読むと、閉じた引用符の後ろの字面を黙って捨てる。キーの値として読ませる。
		var m map[string]string
		if err := yaml.Unmarshal([]byte("v: "+t), &m); err != nil {
			return "", xerrors.Wrapf(errExpression, "引用符の if: の値を YAML の文字列として読めません: %v", err)
		}
		t = strings.TrimSpace(m["v"])
	}
	if !strings.HasPrefix(t, exprOpenWrap) {
		if strings.Contains(t, exprOpenWrap) {
			return "", xerrors.Wrap(errExpression, "if: の値に、`${{ }}` の包みの外の字面が混ざっています")
		}
		return t, nil
	}
	spans := exprSpans(t)
	if len(spans) == 0 {
		return "", xerrors.Wrap(errExpression, "`${{` が閉じていません")
	}
	if spans[0] != t {
		return "", xerrors.Wrap(errExpression, "if: の値に、`${{ }}` の包みの外の字面が混ざっています")
	}
	return t[len(exprOpenWrap) : len(t)-2], nil
}

// exprSpans は、s の中の `${{ ... }}` を左から順に返します。式を閉じる `}}` は文字列リテラルの外のものに
// 限る。閉じない `${{` からは読まない。
func exprSpans(s string) []string {
	var out []string
	for i := 0; ; {
		j := strings.Index(s[i:], exprOpenWrap)
		if j < 0 {
			return out
		}
		start := i + j
		end := -1
		quoted := false
		for k := start + len(exprOpenWrap); k < len(s); k++ {
			if s[k] == '\'' {
				quoted = !quoted
			} else if !quoted && strings.HasPrefix(s[k:], "}}") {
				end = k
				break
			}
		}
		if end < 0 {
			return out
		}
		out = append(out, s[start:end+2])
		i = end + 2
	}
}

// refComparisons は、[expressions] が返す1つの式の字面を読み、ブランチの名前か ref を値に持つ値を、式の中で
// 作った文字列と比べる項を返します。式として読めないもの、構文木から読んだ件数が字面から数えた件数と
// 食い違うものは errExpression で失敗する。
func refComparisons(text string) ([]string, error) {
	body, err := exprBody(text)
	if err != nil {
		return nil, err
	}
	n, err := parseExpr(body)
	if err != nil {
		return nil, err
	}
	found, read, err := walkExpr(n, body)
	if err != nil {
		return nil, err
	}
	if err := countMismatch(read, literalCounts(body)); err != nil {
		return nil, err
	}
	return found, nil
}
