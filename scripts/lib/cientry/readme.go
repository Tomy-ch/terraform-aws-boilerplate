package cientry

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// ReadmeRolesFile は CI の role の表を持つ use-case README の、リポジトリの直下からのパスです。
const ReadmeRolesFile = "usecases/deployment-identity/README.md"

var (
	// ErrReadmeParse は README の role の表を、決まった形として読めない場合のエラー。
	ErrReadmeParse = xerrors.New("README の role の表を解釈できません")
	// ErrReadmeRole は README の role の表が登録簿と一致しない場合のエラー。
	ErrReadmeRole = xerrors.New("README の role の表が登録簿と一致しません")
)

var (
	// readmeHeadRe は role の表の見出しの行。先頭の2つの列で見分ける。
	readmeHeadRe = regexp.MustCompile(`^\|\s*役割\s*\|\s*role 名\s*\|`)
	readmeSepRe  = regexp.MustCompile(`^\|(?:\s*:?-+:?\s*\|)+$`)
)

// parseReadmeRoles は Markdown のソース src の、先頭の2つの列が `役割` と `role 名` の表から、役割の列の role を
// 書かれた順で返します。
//
// 見出しの行はちょうど1つで、その次の行は区切りの行に限ります。表の行は区切りの行に続く `|` で始まる行で、
// `|` で終わり、列の数が見出しと同じものに限ります。役割の列は role の形で、重複してはいけません。これ以外の形と
// 0行は [ErrReadmeParse] です。読めた行数は、区切りの行から最初の空行までの行の数と突き合わせます（ADR-0702
// 決定15）。
func parseReadmeRoles(src []byte) ([]string, error) {
	lines := strings.Split(string(src), "\n")
	var heads []int
	for i, l := range lines {
		if readmeHeadRe.MatchString(l) {
			heads = append(heads, i)
		}
	}
	if len(heads) != 1 {
		return nil, xerrors.Wrap(ErrReadmeParse, fmt.Sprintf("role の表の見出しの行が %d 行です（1行を期待）", len(heads)))
	}
	h := heads[0]
	cells := func(l string) []string {
		l = strings.TrimRight(l, " \t")
		if !strings.HasPrefix(l, "|") || !strings.HasSuffix(l, "|") || len(l) < 2 {
			return nil
		}
		return strings.Split(l[1:len(l)-1], "|")
	}
	cols := len(cells(lines[h]))
	if cols == 0 {
		return nil, xerrors.Wrap(ErrReadmeParse, fmt.Sprintf("%d 行目: 見出しの行が | で終わりません", h+1))
	}
	if h+1 >= len(lines) || !readmeSepRe.MatchString(strings.TrimRight(lines[h+1], " \t")) {
		return nil, xerrors.Wrap(ErrReadmeParse, fmt.Sprintf("%d 行目: 見出しの次が区切りの行ではありません", h+2))
	}
	var roles []string
	for i := h + 2; i < len(lines) && strings.HasPrefix(lines[i], "|"); i++ {
		row := cells(lines[i])
		if len(row) != cols {
			return nil, xerrors.Wrap(ErrReadmeParse, fmt.Sprintf("%d 行目: | で終わらないか、列の数が見出しの %d 個と違います", i+1, cols))
		}
		role := strings.TrimSpace(row[0])
		if !roleRe.MatchString(role) {
			return nil, xerrors.Wrap(ErrReadmeParse, fmt.Sprintf("%d 行目: role %q を読めません", i+1, role))
		}
		if slices.Contains(roles, role) {
			return nil, xerrors.Wrap(ErrReadmeParse, fmt.Sprintf("%d 行目: role %s が重複しています", i+1, role))
		}
		roles = append(roles, role)
	}
	if len(roles) == 0 {
		return nil, xerrors.Wrap(ErrReadmeParse, fmt.Sprintf("%d 行目: role の表が0行です", h+1))
	}
	n := 0
	for _, l := range lines[h+2:] {
		if strings.TrimSpace(l) == "" {
			break
		}
		n++
	}
	if n != len(roles) {
		return nil, xerrors.Wrap(ErrReadmeParse, fmt.Sprintf("区切りの行から空行までの行 %d 個 / 読めた role %d 個", n, len(roles)))
	}
	return roles, nil
}

// checkReadmeRoles は README の role の表の role の集合 got が、登録簿の role の集合と過不足なく一致することを
// 見ます。一致しなければ [ErrReadmeRole] を返し、登録簿にだけ在る role と got にだけ在る role を分けて名指します。
func (r registry) checkReadmeRoles(got []string) error {
	want := r.roles()
	if len(want) == 0 {
		return xerrors.Wrap(errRegistry, "種別が1件もありません")
	}
	if len(got) == 0 {
		return xerrors.Wrap(ErrReadmeRole, "README の role の表が0行です")
	}
	var reasons []string
	if missing := slices.DeleteFunc(slices.Clone(want), func(role string) bool { return slices.Contains(got, role) }); len(missing) > 0 {
		reasons = append(reasons, "登録簿の role "+strings.Join(missing, ", ")+" が README の role の表にありません")
	}
	if extra := slices.DeleteFunc(slices.Clone(got), func(role string) bool { return slices.Contains(want, role) }); len(extra) > 0 {
		reasons = append(reasons, "README の role の表の role "+strings.Join(extra, ", ")+" は登録簿にありません")
	}
	if len(reasons) > 0 {
		return xerrors.Wrap(ErrReadmeRole, strings.Join(reasons, "。"))
	}
	return nil
}
