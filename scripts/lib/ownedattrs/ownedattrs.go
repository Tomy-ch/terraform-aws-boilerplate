// Package ownedattrs は、ADR-0601 決定20 の一覧（Desired State から除く、発火側が所有する属性）を、
// resource type と属性の組として読みます。
//
// 読み手は drift の判定（scripts/tf-local の ci-drift）で、plan の resource_drift から外す組をここから得ます。
// 一覧の写しを持たず、ADR の表を正本として読みます。表が無い・0行・形が崩れているときは、外す組を推測せずに
// 失敗します（ADR-0702 決定13・14）。
package ownedattrs

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/mdscan"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// File は一覧を持つ ADR の、リポジトリの直下からのパスです。
const File = "docs/adr/0601-change-delivery-path.md"

const headerRow = "| resource type | 属性 | 発火側が行う操作 |"

var (
	separatorRow = regexp.MustCompile(`^\|(?:[ \t]*:?-{3,}:?[ \t]*\|){3}$`)
	typeName     = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)+$`)
	attrName     = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

	// ErrRead は File を読めないことを表します。
	ErrRead = xerrors.New("ADR-0601 決定20 の一覧を読めません")
	// ErrNoTable は一覧の表がちょうど1つではないことを表します。
	ErrNoTable = xerrors.New("ADR-0601 決定20 の一覧の表がちょうど1つではありません")
	// ErrEmpty は一覧の表が1行も持たないことを表します。
	ErrEmpty = xerrors.New("ADR-0601 決定20 の一覧の表に行がありません")
	// ErrMalformed は一覧の表の形を読めないことを表します。
	ErrMalformed = xerrors.New("ADR-0601 決定20 の一覧の表の形を読めません")
)

// Pair は一覧の1行の resource type と属性です。
type Pair struct {
	Type string
	Attr string
}

// Parse は ADR の本文 content から一覧の組を、書かれた順に返します。
func Parse(content []byte) ([]Pair, error) {
	lines, err := mdscan.VisibleLines(content)
	if err != nil {
		return nil, xerrors.Wrap(ErrMalformed, err.Error())
	}
	at := -1
	for i, l := range lines {
		if strings.TrimSpace(l.Text) != headerRow {
			continue
		}
		if at >= 0 {
			return nil, xerrors.Wrapf(ErrNoTable, "%d 行目と %d 行目", lines[at].Number, l.Number)
		}
		at = i
	}
	if at < 0 {
		return nil, ErrNoTable
	}
	if at+1 >= len(lines) || !separatorRow.MatchString(strings.TrimSpace(lines[at+1].Text)) {
		return nil, xerrors.Wrapf(ErrMalformed, "%d 行目: 見出しの次の行が区切りの行ではありません", lines[at].Number)
	}
	var out []Pair
	for i, l := range lines[at+2:] {
		text := strings.TrimSpace(l.Text)
		if text == "" {
			break
		}
		// 空行を挟まずに表の後へ続く行は、表の行として読めなかった行である。
		if !strings.HasPrefix(text, "|") {
			return nil, xerrors.Wrapf(ErrMalformed, "%d 行目: 表の行ではありません", l.Number)
		}
		p, err := parseRow(text)
		if err != nil {
			return nil, xerrors.Wrapf(err, "%d 行目", lines[at+2+i].Number)
		}
		if slices.Contains(out, p) {
			return nil, xerrors.Wrapf(ErrMalformed, "%d 行目: %s の %s が重複しています", l.Number, p.Type, p.Attr)
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, ErrEmpty
	}
	return out, nil
}

// parseRow は表の1行 `| `<type>` | `<属性>` | <操作> |` を読みます。
func parseRow(text string) (Pair, error) {
	cells := strings.Split(strings.TrimSuffix(strings.TrimPrefix(text, "|"), "|"), "|")
	if len(cells) != 3 {
		return Pair{}, xerrors.Wrapf(ErrMalformed, "列が %d 個です（3個を期待）", len(cells))
	}
	typ, err := codeCell(cells[0], typeName)
	if err != nil {
		return Pair{}, xerrors.Wrap(err, "resource type")
	}
	attr, err := codeCell(cells[1], attrName)
	if err != nil {
		return Pair{}, xerrors.Wrap(err, "属性")
	}
	// `all` は属性の全体を表し、ADR-0601 決定20 が禁じる。外す組として受けると、その type の変化をすべて外す。
	if attr == "all" {
		return Pair{}, xerrors.Wrap(ErrMalformed, "属性 all は一覧に置けません")
	}
	return Pair{Type: typ, Attr: attr}, nil
}

// codeCell は、セルがちょうど1つの inline code だけで成り、その中身が re の形であるときに中身を返します。
func codeCell(cell string, re *regexp.Regexp) (string, error) {
	cell = strings.TrimSpace(cell)
	codes := mdscan.InlineCodes(cell)
	if len(codes) != 1 || cell != "`"+codes[0]+"`" || !re.MatchString(codes[0]) {
		return "", xerrors.Wrap(ErrMalformed, fmt.Sprintf("%q は1つの inline code の名前ではありません", cell))
	}
	return codes[0], nil
}

// Read はリポジトリ repo の [File] を読んで [Parse] します。
func Read(repo string) ([]Pair, error) {
	r, err := os.OpenRoot(repo)
	if err != nil {
		return nil, xerrors.Wrap(ErrRead, err.Error())
	}
	defer func() { _ = r.Close() }()
	data, err := r.ReadFile(File)
	if err != nil {
		return nil, xerrors.Wrap(ErrRead, err.Error())
	}
	pairs, err := Parse(data)
	if err != nil {
		return nil, xerrors.Wrap(err, File)
	}
	return pairs, nil
}

// Set は組の集合です。
type Set map[Pair]bool

// NewSet は pairs の集合を返します。
func NewSet(pairs []Pair) Set {
	s := Set{}
	for _, p := range pairs {
		s[p] = true
	}
	return s
}

// Has は、resource type typ の属性 attr が集合に在るかを返します。
func (s Set) Has(typ, attr string) bool {
	return s[Pair{Type: typ, Attr: attr}]
}
