// ref-record-lint は、各ユースケースと各一般 module の README が、公式実装の参照記録を持つことを検査します。
//
// **ADR-0206 が自らの検証方法として挙げた項目を、そのまま実行するのがこのツールです。** 参照記録は散文であり、
// 節が消えても、採らなかった知見の理由が抜けても、何も赤くなりません。
//
// 対象は usecases/ と modules/ の直下のディレクトリで、その README.md が次を満たすことを見ます。
//
//  1. `## 公式実装の参照` の見出し（字下げは3つまで、後ろは行末・空白・`（` のどれか）を、コードフェンスと HTML
//     コメントの外にちょうど1つ持つ。閉じない HTML コメントと、読まない形（HTML ブロックと setext 見出し）は違反です
//  2. 節が `参照日: YYYY-MM-DD` の行を持ち、日付として読め、日付の後ろが行末・`（`・`、`・空白のどれか
//  3. 節が `AWS Provider` に続く version（`v6.70.0` / `6.70.0`）を持つ
//  4. 節が `採用した知見:` の行をちょうど1つ持ち、本文が空でない項目が1件以上続く
//  5. 節が `採用しなかった知見と理由:` の行をちょうど1つ持ち、`<知見>: <理由>` の両側が空でない項目が1件以上続く
//
// `参照日:` の行と2つのラベルの行は、字下げせず行頭から書き、一覧の項目にしません。`- 参照日:` や字下げした行は、
// その行として数えません。
//
// 記録の中身が正しいこと、公式実装と一致することは見ません（ADR-0206 の検証方法のレビュー）。
//
// 対象の集合は、ディレクトリの列挙と git の追跡ファイルの2つの経路で得て突き合わせます。見出しの数も、
// フェンスと HTML コメントを読む経路と生の行の経路で数えて突き合わせ、フェンスかコメントの中に見出しの形の行が
// 在れば食い違いとして扱います（ADR-0702 決定15）。CRLF の改行は LF として読みます。
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
	toolName         = "ref-record-lint"
	readme           = "README.md"
	headingText      = "公式実装の参照"
	heading          = "## " + headingText
	datePrefix       = "参照日: "
	adoptedLabel     = "採用した知見:"
	rejectedLabel    = "採用しなかった知見と理由:"
	itemPrefix       = "- "
	reasonSep        = ": "
	dateLayout       = "2006-01-02"
	dateLayoutSize   = len(dateLayout)
	dateShape        = "YYYY-MM-DD"
	frontMatterFence = "---"
)

// 読まない形の違反。走査がこれらを読めば、後ろの行の意味が変わる。
const (
	msgHTMLBlock = "ref-record-lint は HTML ブロック（`<!--` のコメント以外で `<` から始まる行）を読みません。別の書き方にします"
	msgSetext    = "ref-record-lint は setext 見出し（段落の直後の `=` か `-` だけの行）を読みません。`#` の見出しにするか、前に空行を置きます"
)

// scanRoots は、対象のディレクトリを直下に持つ root。ADR-0206 決定8 が記録の置き場とする2つ。
var scanRoots = []string{"usecases", "modules"}

var (
	providerVersion = regexp.MustCompile(`AWS Provider v?[0-9]+\.[0-9]+\.[0-9]+`)
	anyHeading      = regexp.MustCompile(`^ {0,3}(#{1,6})([ \t]|$)`)
	htmlBlockStart  = regexp.MustCompile(`^ {0,3}<`)
	setextUnderline = regexp.MustCompile(`^ {0,3}(=+|-+)[ \t]*$`)
	listItem        = regexp.MustCompile(`^ {0,3}([-+*]|[0-9]{1,9}[.)])([ \t]|$)`)
	thematicBreak   = regexp.MustCompile(`^ {0,3}((\*[ \t]*){3,}|(-[ \t]*){3,}|(_[ \t]*){3,})$`)
	commentStart    = regexp.MustCompile(`^ {0,3}<!--`)
	blockQuote      = regexp.MustCompile(`^ {0,3}>`)
	indentedCode    = regexp.MustCompile(`^( {4}| {0,3}\t)`)
)

// headingTails と dateTails は、見出しと日付の直後に続いてよいもの。行末も許します。
var (
	headingTails = []string{" ", "\t", "（"}
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

// checkReadme は、README の参照記録の節を検査します。見出しの形をした生の行の数と、scanReadme が見出しとして
// 読んだ数が一致しなければ、読まなかった行とその理由を名指して errCountMismatch を返します。この食い違いは
// ほかのどの違反より先に返すので、閉じないコメントが見出しを隠したときも、その見出しの行を名指す食い違いに
// なります。次に、読まない形と閉じないコメントの違反を、在ればそれだけ返します。
func checkReadme(path string, content []byte) ([]lintreport.Finding, error) {
	content = bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	sc := scanReadme(content)
	raw := 0
	for _, l := range rawLines(content) {
		if isHeading(l.Text) {
			raw++
		}
	}
	if raw != len(sc.headings) {
		where := make([]string, 0, len(sc.hidden))
		for _, h := range sc.hidden {
			where = append(where, fmt.Sprintf("%d 行目（%s）", h.Line, h.Reason))
		}
		return nil, xerrors.Wrapf(errCountMismatch, "%s: 見出し %q が生の行に %d 件、見出しとして読める位置に %d 件。読まなかった行: %s",
			path, heading, raw, len(sc.headings), strings.Join(where, "、"))
	}

	var unreadable []lintreport.Finding
	for _, f := range sc.unsupported {
		unreadable = append(unreadable, lintreport.Finding{File: path, Line: f.Line, Message: f.Message})
	}
	if sc.openedAt != 0 {
		unreadable = append(unreadable, lintreport.Finding{File: path, Line: sc.openedAt, Message: "HTML コメントが閉じていません。後ろの行をすべてコメントとして読みます"})
	}
	if len(unreadable) > 0 {
		return unreadable, nil
	}

	lines, at := sc.lines, sc.headings
	switch {
	case len(at) == 0:
		return []lintreport.Finding{{File: path, Line: 1, Message: fmt.Sprintf("%q の見出しがありません", heading)}}, nil
	case len(at) > 1:
		return []lintreport.Finding{{File: path, Line: lines[at[1]].Number, Message: fmt.Sprintf("%q の見出しが %d 件あります。1つにまとめます", heading, len(at))}}, nil
	}
	return checkSection(path, lines[at[0]].Number, section(lines, at[0])), nil
}

// 見出しの形をした行を、見出しとして読まなかった理由。
const (
	reasonFence       = "フェンス"
	reasonComment     = "HTML コメント"
	reasonFrontMatter = "front matter"
)

// hiddenLine は、見出しの形をしているが、フェンスか HTML コメントの中に在るので見出しとして読まなかった行です。
type hiddenLine struct {
	Line   int
	Reason string
}

// scanLine は、フェンスの外の1行です。Text は HTML コメントの部分を除いた文字で、Level は元の行が見出しならその段
// （1-6）、見出しでなければ 0 です。
type scanLine struct {
	Number int
	Text   string
	Level  int
}

// scanned は scanReadme の結果です。lines はフェンスの外の行を、行の番号を保って持ちます。headings は参照記録の
// 見出しの lines での添字です。unsupported は読まない形（HTML ブロックと setext 見出し）の行の違反で、File を
// 持ちません。openedAt は末尾まで閉じなかったコメントを開いた行の番号で、閉じていれば 0 です。
type scanned struct {
	lines       []scanLine
	headings    []int
	hidden      []hiddenLine
	unsupported []lintreport.Finding
	openedAt    int
}

// scanReadme は、フェンスと HTML コメントの状態を mdscan.Scanner の1度の走査で一緒に追います。見出しは元の行が
// 見出しの形をしているときだけ数え（headingLevel）、`<!-- -->## …` のように行頭がコメントの行と、コメントの中で
// 始まる行は、参照記録の見出しとしても、節と一覧の終わりとしても見出しではありません。フェンスとコメントの外の
// HTML ブロックの開始行と setext 見出しの下線は、読み違えずに読む方法を持たないので unsupported に挙げます。
func scanReadme(content []byte) scanned {
	var sc scanned
	var s mdscan.Scanner
	// paragraph は直前の行が段落の行か、inlineComment は開いているコメントが段落の中で開いたか（行頭で開いた
	// コメントは HTML ブロックで、閉じた後ろに段落は続かない）。
	paragraph, inlineComment := false, false
	texts := strings.Split(string(content), "\n")
	frontEnd := frontMatterEnd(texts)
	for i, text := range texts {
		number := i + 1
		if i <= frontEnd && frontEnd > 0 {
			if isHeading(text) {
				sc.hidden = append(sc.hidden, hiddenLine{Line: number, Reason: reasonFrontMatter})
			}
			sc.lines = append(sc.lines, scanLine{Number: number, Text: text})
			continue
		}
		r := s.Next(mdscan.Line{Number: number, Text: text})
		if r.Fence {
			paragraph = false
			if isHeading(text) {
				sc.hidden = append(sc.hidden, hiddenLine{Line: number, Reason: reasonFence})
			}
			continue
		}
		inComment := r.Comment
		level := 0
		if !inComment {
			level = headingLevel(text)
			switch {
			case isHTMLBlockStart(text):
				sc.unsupported = append(sc.unsupported, lintreport.Finding{Line: number, Message: msgHTMLBlock})
			case paragraph && isSetextUnderline(text):
				sc.unsupported = append(sc.unsupported, lintreport.Finding{Line: number, Message: msgSetext})
			}
		}
		switch {
		case !inComment:
			paragraph = isParagraphLine(text)
			inlineComment = paragraph
		case !inlineComment:
			paragraph = false
		}
		if isHeading(text) {
			if inComment {
				sc.hidden = append(sc.hidden, hiddenLine{Line: number, Reason: reasonComment})
			} else {
				sc.headings = append(sc.headings, len(sc.lines))
			}
		}
		sc.lines = append(sc.lines, scanLine{Number: number, Text: r.Visible, Level: level})
	}
	sc.openedAt = s.Unclosed()
	return sc
}

// headingLevel は、text が ATX 見出しならその段（1-6）を、見出しでなければ 0 を返します。
func headingLevel(text string) int {
	m := anyHeading.FindStringSubmatch(text)
	if m == nil {
		return 0
	}
	return len(m[1])
}

// frontMatterEnd は、texts の1行目が `---` で、後ろに閉じの `---` の行が在れば、その添字を返します。無ければ 0 です。
func frontMatterEnd(texts []string) int {
	if len(texts) == 0 || texts[0] != frontMatterFence {
		return 0
	}
	for i := 1; i < len(texts); i++ {
		if texts[i] == frontMatterFence {
			return i
		}
	}
	return 0
}

// isHTMLBlockStart は、text が `<!--` のコメント以外の `<` で始まり、HTML ブロック（CommonMark の種類1・3-7）か
// autolink を開きうるかを返します。
func isHTMLBlockStart(text string) bool {
	return htmlBlockStart.MatchString(text) && !commentStart.MatchString(text)
}

// isSetextUnderline は、text が、直前の段落の行を setext 見出しにする下線の形かを返します。
func isSetextUnderline(text string) bool {
	return setextUnderline.MatchString(text)
}

// isParagraphLine は、text が段落の行として読まれ、次の行が下線なら setext 見出しになるかを返します。空行・見出し・
// 一覧の項目・区切り線・引用・4つ以上の字下げ・HTML ブロックとコメントで始まる行は段落の行ではありません。
func isParagraphLine(text string) bool {
	return strings.TrimSpace(text) != "" && headingLevel(text) == 0 && !listItem.MatchString(text) &&
		!thematicBreak.MatchString(text) && !blockQuote.MatchString(text) && !indentedCode.MatchString(text) &&
		!isHTMLBlockStart(text) && !commentStart.MatchString(text)
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

// isHeading は、text が参照記録の見出し（2段目、字下げは3つまで）かを返します。`## 公式実装の参照メモ` のように
// 語が続くものは見出しではありません。
func isHeading(text string) bool {
	if headingLevel(text) != 2 {
		return false
	}
	rest := strings.TrimLeft(strings.TrimLeft(text, " ")[len("##"):], " \t")
	rest, ok := strings.CutPrefix(rest, headingText)
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

// section は、lines[start] の見出しの次の行から、次の1段目か2段目の見出しの手前までを返します。
func section(lines []scanLine, start int) []scanLine {
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if lv := lines[i].Level; lv == 1 || lv == 2 {
			end = i
			break
		}
	}
	return lines[start+1 : end]
}

func checkSection(path string, headingLine int, body []scanLine) []lintreport.Finding {
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
func checkDate(body []scanLine) (line int, msg string, ok bool) {
	var found []scanLine
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

func hasProviderVersion(body []scanLine) bool {
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
func checkList(body []scanLine, label string, needReason bool) []lintreport.Finding {
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
		if isLabel(text, adoptedLabel) || isLabel(text, rejectedLabel) || l.Level > 0 {
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
