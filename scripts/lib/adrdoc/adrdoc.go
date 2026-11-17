// Package adrdoc は、ADR の置き場所と本文の読み方を持ちます。identity（`<scope>/<slug>`、ADR-0001 決定4）から
// ファイルを引き、Status と見出しを取り出します。
//
// 読み手は adr-lint と infra-code-gate の2つです。ファイル名と Status 行の形を各々が持つと、片方だけが
// 規約に追従したとき、もう片方は ADR を見失ったまま「参照先が無い」と報告します。
package adrdoc

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/mdscan"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	// RootScope は root scope の名前です。`Superseded by <scope>/...` の scope と同じ綴りです。
	RootScope = "repository-wide"
	// RootDir は root scope の ADR の置き場所です。
	RootDir = "docs/adr"
	// UseCasesDir は use-case ADR の置き場所 usecases/<use-case>/docs/adr/ の先頭です（ADR-0001 決定12）。
	UseCasesDir = "usecases"
	// UseCaseDir は、ユースケースのディレクトリの中の ADR の置き場所です。
	UseCaseDir = "docs/adr"
)

var (
	// FileName は ADR のファイル名です。大文字とアンダースコアを弾くのは、同じ決定が2つの綴りで置かれるのを防ぐため。
	FileName = regexp.MustCompile(`^(\d{4})-([a-z0-9]+(?:-[a-z0-9]+)*)\.md$`)
	// StatusLine は先頭メタデータの Status 行です。
	StatusLine = regexp.MustCompile(`(?m)^- Status:[ \t]*(\S.*)$`)

	identity    = regexp.MustCompile(`^([a-z0-9]+(?:-[a-z0-9]+)*)/([a-z0-9]+(?:-[a-z0-9]+)*)$`)
	headingLine = regexp.MustCompile(`^#{1,6}[ \t]+(.*?)[ \t]*#*[ \t]*$`)
	inlineCode  = regexp.MustCompile("`[^`]*`")
)

var (
	// ErrIdentity は、identity が `<scope>/<slug>` の形でないことを表します。
	ErrIdentity = xerrors.New("ADR の identity が `<scope>/<slug>` の形ではありません")
	// ErrNotFound は、identity に当たる ADR が無いことを表します。
	ErrNotFound = xerrors.New("identity に当たる ADR がありません")
	// ErrAmbiguous は、同じ slug の ADR が scope の中に2つ以上あることを表します。
	ErrAmbiguous = xerrors.New("identity に当たる ADR が2つ以上あります")
)

// Dir は scope の ADR の置き場所を、repo からの相対パスで返します。
func Dir(scope string) string {
	if scope == RootScope {
		return RootDir
	}
	return filepath.Join(UseCasesDir, scope, filepath.FromSlash(UseCaseDir))
}

// Find は identity に当たる ADR のパスを、repo からの相対パスで返します。
func Find(repo, id string) (string, error) {
	m := identity.FindStringSubmatch(id)
	if m == nil {
		return "", xerrors.Wrapf(ErrIdentity, "%q", id)
	}
	dir := Dir(m[1])
	entries, err := os.ReadDir(filepath.Join(repo, dir))
	if err != nil {
		if os.IsNotExist(err) {
			return "", xerrors.Wrapf(ErrNotFound, "%s（%s がありません）", id, dir)
		}
		return "", xerrors.Wrapf(err, "%s の読み取り", dir)
	}
	var found []string
	for _, e := range entries {
		if f := FileName.FindStringSubmatch(e.Name()); f != nil && !e.IsDir() && f[2] == m[2] {
			found = append(found, filepath.Join(dir, e.Name()))
		}
	}
	switch len(found) {
	case 0:
		return "", xerrors.Wrapf(ErrNotFound, "%s", id)
	case 1:
		return found[0], nil
	default:
		return "", xerrors.Wrapf(ErrAmbiguous, "%s（%s）", id, strings.Join(found, " / "))
	}
}

// Status は本文の Status 行の値を返します。行が無ければ ok は false です。
func Status(source string) (status string, ok bool) {
	m := StatusLine.FindStringSubmatch(source)
	if m == nil {
		return "", false
	}
	return strings.TrimSpace(m[1]), true
}

// Headings は、コードフェンスと HTML コメントの外にある ATX 見出しのテキストを、現れた順に返します。
// レベルは問いません。inline code の中の `<!--` はコメントを開きません。
func Headings(source []byte) []string {
	var out []string
	inComment := false
	for _, l := range mdscan.Lines(source) {
		var visible strings.Builder
		text := inlineCode.ReplaceAllString(l.Text, "")
		for text != "" {
			if inComment {
				end := strings.Index(text, "-->")
				if end < 0 {
					text = ""
					continue
				}
				inComment = false
				text = text[end+len("-->"):]
				continue
			}
			start := strings.Index(text, "<!--")
			if start < 0 {
				visible.WriteString(text)
				break
			}
			visible.WriteString(text[:start])
			inComment = true
			text = text[start+len("<!--"):]
		}
		if m := headingLine.FindStringSubmatch(visible.String()); m != nil {
			out = append(out, m[1])
		}
	}
	return out
}
