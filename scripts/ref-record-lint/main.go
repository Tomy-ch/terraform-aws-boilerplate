// ref-record-lint は、各ユースケースと各一般 module の README が、公式実装の参照記録を持つことを検査します。
//
// **ADR-0206 が自らの検証方法として挙げた項目を、そのまま実行するのがこのツールです。** 参照記録は散文であり、
// 節が消えても、採らなかった知見の理由が抜けても、何も赤くなりません。
//
// 対象は usecases/ と modules/ の直下のディレクトリで、その README.md が次を満たすことを見ます。
//
//  1. `## 公式実装の参照` の見出し（後ろは行末・空白・`（` のどれか）を、コードフェンスと HTML コメントの外に
//     ちょうど1つ持つ。閉じない HTML コメントは違反です
//  2. 節が `参照日: YYYY-MM-DD` の行を持ち、日付として読め、日付の後ろが行末・`（`・`、`・空白のどれか
//  3. 節が `AWS Provider` に続く version（`v6.70.0` / `6.70.0`）を持つ
//  4. 節が `採用した知見:` の行をちょうど1つ持ち、本文が空でない項目が1件以上続く
//  5. 節が `採用しなかった知見と理由:` の行をちょうど1つ持ち、`<知見>: <理由>` の両側が空でない項目が1件以上続く
//
// 記録の中身が正しいこと、公式実装と一致することは見ません（ADR-0206 の検証方法のレビュー）。
//
// 対象の集合は、ディレクトリの列挙と git の追跡ファイルの2つの経路で得て突き合わせます。見出しの数も、
// フェンスを読む経路（lib/mdscan）と生の行の経路で数えて突き合わせます（ADR-0702 決定15）。CRLF の改行は LF として
// 読みます。
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/gitenv"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lintreport"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/mdscan"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	toolName       = "ref-record-lint"
	readme         = "README.md"
	heading        = "## 公式実装の参照"
	datePrefix     = "参照日: "
	adoptedLabel   = "採用した知見:"
	rejectedLabel  = "採用しなかった知見と理由:"
	itemPrefix     = "- "
	reasonSep      = ": "
	dateLayout     = "2006-01-02"
	dateLayoutSize = len(dateLayout)
	dateShape      = "YYYY-MM-DD"
	commentOpen    = "<!--"
	commentClose   = "-->"
)

// scanRoots は、対象のディレクトリを直下に持つ root。ADR-0206 決定8 が記録の置き場とする2つ。
var scanRoots = []string{"usecases", "modules"}

var (
	providerVersion = regexp.MustCompile(`AWS Provider v?[0-9]+\.[0-9]+\.[0-9]+`)
	anyHeading      = regexp.MustCompile(`^#{1,6}( |$)`)
)

// headingTails と dateTails は、見出しと日付の直後に続いてよいもの。行末も許します。
var (
	headingTails = []string{" ", "（"}
	dateTails    = []string{"（", "、", " "}
)

var (
	errUsage         = xerrors.New("引数が正しくありません")
	errUnreadable    = xerrors.New("検査対象を読み取れません")
	errNoRoot        = xerrors.New("走査の root がありません")
	errNoTargets     = xerrors.New("走査の root に対象のディレクトリが0件です")
	errCountMismatch = xerrors.New("2つの経路で得た件数が一致しません")
	errViolations    = xerrors.New("違反がある")
)

func main() {
	log.SetFlags(0)
	if err := run(os.Args[1:], os.Stdout, func(dir string) ([]string, error) { return gitFiles(dir, gitEnv) }); err != nil {
		log.Printf("❌ %s: %v", toolName, err)
		os.Exit(exitCode(err))
	}
}

func gitEnv() ([]string, error) { return gitenv.Env(os.Environ()) }

func exitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case xerrors.Is(err, errViolations):
		return 1
	default:
		return 2
	}
}

// listFunc は、dir のリポジトリの追跡ファイルを、dir からの相対パスで返します。テストは差し替えます。
type listFunc func(dir string) ([]string, error)

func run(args []string, out io.Writer, lister listFunc) error {
	fset := flag.NewFlagSet(toolName, flag.ContinueOnError)
	fset.SetOutput(out)
	root := fset.String("root", ".", "リポジトリのルート")
	if err := fset.Parse(args); err != nil {
		return xerrors.Wrapf(errUsage, "%v", err)
	}
	if fset.NArg() > 0 {
		return xerrors.Wrapf(errUsage, "余分な引数 %q", fset.Args())
	}

	tracked, err := lister(*root)
	if err != nil {
		return xerrors.Wrapf(errUnreadable, "追跡ファイルの一覧: %v", err)
	}

	var findings []lintreport.Finding
	counts := make([]string, 0, len(scanRoots))
	for _, scanRoot := range scanRoots {
		dirs, err := listTargets(*root, scanRoot)
		if err != nil {
			return err
		}
		if err := compareSets(scanRoot, dirs, trackedTargets(tracked, scanRoot)); err != nil {
			return err
		}
		for _, name := range dirs {
			f, err := checkTarget(*root, filepath.ToSlash(filepath.Join(scanRoot, name, readme)))
			if err != nil {
				return err
			}
			findings = append(findings, f...)
		}
		counts = append(counts, fmt.Sprintf("%s %d 件", scanRoot, len(dirs)))
	}

	if len(findings) > 0 {
		lintreport.Sort(findings)
		fmt.Fprintf(out, "❌ %s: %d 件の違反\n\n", toolName, len(findings))
		fmt.Fprintln(out, lintreport.Format(findings))
		fmt.Fprintf(out, "\n記録の項目は ADR-0206 決定8、検査する形は scripts/README.md の %s 行が持つ。\n", toolName)
		return xerrors.Wrapf(errViolations, "%d 件", len(findings))
	}

	fmt.Fprintf(out, "✅ %s: %sの README が参照記録を持ちます\n", toolName, strings.Join(counts, " / "))
	return nil
}

// gitFiles は、dir のリポジトリの追跡ファイルを、isolated が返す（外側のリポジトリから切り離した）env の git で
// 列挙します。
func gitFiles(dir string, isolated func() ([]string, error)) ([]string, error) {
	env, err := isolated()
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(context.Background(), "git", "-c", "core.quotepath=false", "ls-files", "-z")
	cmd.Dir, cmd.Env = dir, env
	raw, err := cmd.Output()
	if err != nil {
		return nil, xerrors.Wrapf(err, "git ls-files")
	}
	return splitNUL(string(raw)), nil
}

func splitNUL(s string) []string {
	var out []string
	for _, p := range strings.Split(s, "\x00") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// listTargets は、root/scanRoot の直下のディレクトリの名前を昇順で返します。ディレクトリでないもの（symlink を
// 含む）は、対象かどうかを決められないので errUnreadable にします。
func listTargets(root, scanRoot string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, scanRoot))
	if xerrors.Is(err, fs.ErrNotExist) {
		return nil, xerrors.Wrapf(errNoRoot, "%s", scanRoot)
	}
	if err != nil {
		return nil, xerrors.Wrapf(errUnreadable, "%s: %v", scanRoot, err)
	}
	var names []string
	for _, e := range entries {
		if e.Type()&fs.ModeType != fs.ModeDir {
			return nil, xerrors.Wrapf(errUnreadable, "%s/%s はディレクトリではありません", scanRoot, e.Name())
		}
		names = append(names, e.Name())
	}
	if len(names) == 0 {
		return nil, xerrors.Wrapf(errNoTargets, "%s", scanRoot)
	}
	sort.Strings(names)
	return names, nil
}

// trackedTargets は、追跡ファイルのうち scanRoot/<名前>/ の下に在るものから、<名前> の集合を昇順で返します。
func trackedTargets(tracked []string, scanRoot string) []string {
	seen := map[string]bool{}
	for _, p := range tracked {
		rest, ok := strings.CutPrefix(p, scanRoot+"/")
		if !ok {
			continue
		}
		if name, _, nested := strings.Cut(rest, "/"); nested && name != "" {
			seen[name] = true
		}
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// compareSets は、ディレクトリの列挙と追跡ファイルから得た対象の集合が一致しなければ、片側にだけ在るものを
// 名指して errCountMismatch を返します。
func compareSets(scanRoot string, dirs, tracked []string) error {
	onlyDir, onlyTracked := difference(dirs, tracked), difference(tracked, dirs)
	if len(onlyDir) == 0 && len(onlyTracked) == 0 {
		return nil
	}
	return xerrors.Wrapf(errCountMismatch, "%s: ディレクトリ %d 件・追跡 %d 件。ディレクトリにだけ在る %q、追跡にだけ在る %q",
		scanRoot, len(dirs), len(tracked), onlyDir, onlyTracked)
}

// difference は、a に在って b に無いものを a の順で返します。
func difference(a, b []string) []string {
	in := make(map[string]bool, len(b))
	for _, s := range b {
		in[s] = true
	}
	var out []string
	for _, s := range a {
		if !in[s] {
			out = append(out, s)
		}
	}
	return out
}

func checkTarget(root, path string) ([]lintreport.Finding, error) {
	full := filepath.Join(root, filepath.FromSlash(path))
	info, err := os.Lstat(full)
	if xerrors.Is(err, fs.ErrNotExist) {
		return []lintreport.Finding{{File: path, Line: 1, Message: "README.md がありません"}}, nil
	}
	if err != nil {
		return nil, xerrors.Wrapf(errUnreadable, "%s: %v", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, xerrors.Wrapf(errUnreadable, "%s は通常のファイルではありません", path)
	}
	content, err := os.ReadFile(filepath.Clean(full))
	if err != nil {
		return nil, xerrors.Wrapf(errUnreadable, "%s: %v", path, err)
	}
	return checkReadme(path, content)
}

// checkReadme は、README の参照記録の節を検査します。見出しの数が2つの経路で一致しなければ errCountMismatch を
// 返します。
func checkReadme(path string, content []byte) ([]lintreport.Finding, error) {
	content = bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	lines, openedAt := visibleLines(mdscan.Lines(content))
	var at []int
	for i, l := range lines {
		if isHeading(l.Text) {
			at = append(at, i)
		}
	}
	raw := 0
	rawVisible, _ := visibleLines(rawLines(content))
	for _, l := range rawVisible {
		if isHeading(l.Text) {
			raw++
		}
	}
	if raw != len(at) {
		return nil, xerrors.Wrapf(errCountMismatch, "%s: 見出し %q がフェンスの外に %d 件、全行に %d 件", path, heading, len(at), raw)
	}

	switch {
	case openedAt != 0:
		return []lintreport.Finding{{File: path, Line: openedAt, Message: "HTML コメントが閉じていません。後ろの行をすべてコメントとして読みます"}}, nil
	case len(at) == 0:
		return []lintreport.Finding{{File: path, Line: 1, Message: fmt.Sprintf("%q の見出しがありません", heading)}}, nil
	case len(at) > 1:
		return []lintreport.Finding{{File: path, Line: lines[at[1]].Number, Message: fmt.Sprintf("%q の見出しが %d 件あります。1つにまとめます", heading, len(at))}}, nil
	}
	return checkSection(path, lines[at[0]].Number, section(lines, at[0])), nil
}

// rawLines は、content の全行を、フェンスを区別せずに1始まりの番号で返します。
func rawLines(content []byte) []mdscan.Line {
	texts := strings.Split(string(content), "\n")
	out := make([]mdscan.Line, 0, len(texts))
	for i, text := range texts {
		out = append(out, mdscan.Line{Number: i + 1, Text: text})
	}
	return out
}

// isHeading は、text が参照記録の見出しかを返します。`## 公式実装の参照メモ` のように語が続くものは見出しではありません。
func isHeading(text string) bool {
	rest, ok := strings.CutPrefix(text, heading)
	return ok && hasTail(rest, headingTails)
}

// hasTail は、rest が空か、tails のどれかで始まるかを返します。
func hasTail(rest string, tails []string) bool {
	if rest == "" {
		return true
	}
	for _, tail := range tails {
		if strings.HasPrefix(rest, tail) {
			return true
		}
	}
	return false
}

// visibleLines は、各行から HTML コメントの部分を除いた文字を、行の番号を保って返します。コメントだけの行は空の行に
// なります。inline code の中の `<!--` はコメントを開きません。末尾までコメントが閉じなければ、openedAt はその
// コメントを開いた行の番号で、閉じていれば 0 です。
func visibleLines(lines []mdscan.Line) (out []mdscan.Line, openedAt int) {
	out = make([]mdscan.Line, 0, len(lines))
	for _, l := range lines {
		var visible strings.Builder
		text := l.Text
		for text != "" {
			if openedAt != 0 {
				_, after, found := strings.Cut(text, commentClose)
				if !found {
					break
				}
				openedAt, text = 0, after
				continue
			}
			if text[0] == '`' {
				n := backtickRun(text)
				if end := strings.Index(text[n:], text[:n]); end >= 0 && backtickRun(text[n+end:]) == n {
					visible.WriteString(text[:n+end+n])
					text = text[n+end+n:]
					continue
				}
				visible.WriteString(text[:n])
				text = text[n:]
				continue
			}
			if strings.HasPrefix(text, commentOpen) {
				openedAt, text = l.Number, text[len(commentOpen):]
				continue
			}
			next := strings.IndexAny(text[1:], "`<")
			if next < 0 {
				visible.WriteString(text)
				break
			}
			visible.WriteString(text[:next+1])
			text = text[next+1:]
		}
		out = append(out, mdscan.Line{Number: l.Number, Text: visible.String()})
	}
	return out, openedAt
}

// backtickRun は、s の先頭から続くバッククォートの個数を返します。
func backtickRun(s string) int {
	n := 0
	for n < len(s) && s[n] == '`' {
		n++
	}
	return n
}

// section は、lines[start] の見出しの次の行から、次の1段目か2段目の見出しの手前までを返します。
func section(lines []mdscan.Line, start int) []mdscan.Line {
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i].Text, "# ") || strings.HasPrefix(lines[i].Text, "## ") {
			end = i
			break
		}
	}
	return lines[start+1 : end]
}

func checkSection(path string, headingLine int, body []mdscan.Line) []lintreport.Finding {
	var findings []lintreport.Finding
	add := func(line int, msg string) {
		findings = append(findings, lintreport.Finding{File: path, Line: line, Message: msg})
	}

	if line, msg, ok := checkDate(body); !ok {
		if line == 0 {
			line = headingLine
		}
		add(line, msg)
	}
	if !hasProviderVersion(body) {
		add(headingLine, "節に AWS Provider の version（`AWS Provider v6.70.0` の形）がありません")
	}
	for _, label := range []string{adoptedLabel, rejectedLabel} {
		for _, f := range checkList(body, label, label == rejectedLabel) {
			if f.Line == 0 {
				f.Line = headingLine
			}
			add(f.Line, f.Message)
		}
	}
	return findings
}

// checkDate は、節の `参照日: ` の行が1つだけ在り、日付として読めるかを返します。違反のとき、行が無ければ
// line は 0 です。
func checkDate(body []mdscan.Line) (line int, msg string, ok bool) {
	var found []mdscan.Line
	for _, l := range body {
		if strings.HasPrefix(l.Text, datePrefix) {
			found = append(found, l)
		}
	}
	switch {
	case len(found) == 0:
		return 0, fmt.Sprintf("節に %q の行がありません", datePrefix+dateShape), false
	case len(found) > 1:
		return found[1].Number, fmt.Sprintf("%q の行が %d 件あります", strings.TrimSpace(datePrefix), len(found)), false
	}
	value := strings.TrimPrefix(found[0].Text, datePrefix)
	if len(value) < dateLayoutSize || !hasTail(value[dateLayoutSize:], dateTails) {
		return found[0].Number, fmt.Sprintf("参照日 %q を日付（%s）として読めません", value, dateShape), false
	}
	if _, err := time.Parse(dateLayout, value[:dateLayoutSize]); err != nil {
		return found[0].Number, fmt.Sprintf("参照日 %q を日付（%s）として読めません", value, dateShape), false
	}
	return found[0].Number, "", true
}

func hasProviderVersion(body []mdscan.Line) bool {
	for _, l := range body {
		if providerVersion.MatchString(l.Text) {
			return true
		}
	}
	return false
}

// checkList は、節の label の行がちょうど1つ在り、その後に項目が1件以上続くことを検査します。needReason なら、
// 項目が `<知見>: <理由>` の両側を空でなく持つことも見ます。label の行が無い違反の Line は 0 です。
//
// 一覧は label の行の次から、次の label の行か見出しの手前までです。その中で `- ` で始まる行が項目で、`-` だけの行は
// 本文が空の項目です。字下げした行は直前の項目の続き、空行は読み飛ばします。それ以外の行は、後ろに項目が続けば
// 違反で、続かなければ一覧の後の文です。
func checkList(body []mdscan.Line, label string, needReason bool) []lintreport.Finding {
	var at []int
	for i, l := range body {
		if isLabel(l.Text, label) {
			at = append(at, i)
		}
	}
	switch {
	case len(at) == 0:
		return []lintreport.Finding{{Message: fmt.Sprintf("節に %q の行がありません", label)}}
	case len(at) > 1:
		return []lintreport.Finding{{Line: body[at[1]].Number, Message: fmt.Sprintf("%q の行が %d 件あります", label, len(at))}}
	}

	var findings, prose []lintreport.Finding
	items := 0
	for _, l := range body[at[0]+1:] {
		text := l.Text
		if isLabel(text, adoptedLabel) || isLabel(text, rejectedLabel) || anyHeading.MatchString(text) {
			break
		}
		if strings.TrimSpace(text) == "" || strings.HasPrefix(text, " ") || strings.HasPrefix(text, "\t") {
			continue
		}
		item, ok := strings.CutPrefix(text, itemPrefix)
		if !ok {
			if strings.TrimRight(text, " \t") != strings.TrimSpace(itemPrefix) {
				prose = append(prose, lintreport.Finding{Line: l.Number, Message: fmt.Sprintf("%q の一覧の途中に項目でない行があります", label)})
				continue
			}
			item = ""
		}
		findings, prose = append(findings, prose...), nil
		items++
		if strings.TrimSpace(item) == "" {
			findings = append(findings, lintreport.Finding{Line: l.Number, Message: fmt.Sprintf("%q の項目の本文が空です", label)})
			continue
		}
		if needReason {
			knowledge, reason, found := strings.Cut(item, reasonSep)
			if !found || strings.TrimSpace(knowledge) == "" || strings.TrimSpace(reason) == "" {
				findings = append(findings, lintreport.Finding{Line: l.Number, Message: fmt.Sprintf("%q の項目が `<知見>: <理由>` の形ではありません", label)})
			}
		}
	}
	if items == 0 {
		findings = append(findings, lintreport.Finding{Line: body[at[0]].Number, Message: fmt.Sprintf("%q の後に項目がありません", label)})
	}
	return findings
}

func isLabel(text, label string) bool {
	return strings.TrimRight(text, " \t") == label
}
