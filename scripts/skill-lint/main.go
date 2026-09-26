// skill-lint は、スキルが名指しした先が実在することを検査します。
//
// スキルは**検査を持たない散文**です。名指しした make target が消えても、パスが動いても、
// 何も赤くなりません。嘘をついたスキルは、それを読んだ人が叩くまで嘘だと分からず、叩いた人は
// 「この手順は古い」ではなく「自分の環境が壊れている」と読みます。
//
// 見るもの:
//
//  1. `make <target>` の <target> が実在すること
//  2. `/<skill>` が .claude/skills/ に実在すること
//  3. パスが実在すること
//
// 3 は**先頭セグメントがリポジトリ直下に実在するものだけ**を対象にします。まだ無い領域を
// 名指しした記述はずれではなく予告であり、その領域が作られた日に検査が自動で始まります。
//
// 例示と参照はフェンスの位置で分けます（lib/mdscan）。フェンスの外の inline code span は
// 実在するものを名指しする、という契約です。**行単位で検査を黙らせる口は持ちません** ——
// 通らない行を通すためのマーカは溜まり、対象が消えても残るためです。存在しないものを
// 述べたい文は、コードスパンに入れずに書きます。
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lintreport"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/mdscan"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	toolName  = "skill-lint"
	skillsDir = ".claude/skills"
	// 綴りは実ファイルと厳密に一致させること。macOS のファイルシステムは大文字小文字を
	// 区別しないので、取り違えても手元では開けてしまい、Linux の CI でだけ落ちる。
	makefile   = "Makefile"
	skillDoc   = ".md"
	scratchDir = "tmp"
)

var (
	// make の起動。`make foo` / `make foo bar` / `make -s foo` を拾う。
	makeInvocation = regexp.MustCompile(`^make(\s|$)`)
	// make へ渡してよい形。外れた語が出たときの扱いは makeTargets を見よ。
	makeArgument = regexp.MustCompile(`^[A-Za-z0-9_%.<>{},*/-]+$`)
	// スキルの参照。`/impl-review` の形。
	skillRef = regexp.MustCompile(`^/([a-z][a-z0-9-]*)$`)
	// makefile の include 行。
	includeLine = regexp.MustCompile(`(?m)^include\s+(\S+)\s*$`)
	// .PHONY の宣言。`##` 以降はヘルプの注記で、ターゲット名ではない。
	phonyLine = regexp.MustCompile(`^\.PHONY:\s*(.+)$`)
	// 規則の行。`target: prereq` の形。変数代入（`X := v` / `X = v`）はここへ来ない。
	ruleLine = regexp.MustCompile(`^([A-Za-z0-9_%.+/ -]+):(.*)$`)
	// パスに見えて参照ではないもの。空白とシェル / URL の約物を含む span は文章か command である。
	notAPath = regexp.MustCompile("[\\s$\\\\#?!\"'()|`:;@]")
	// Go の再帰指定（`scripts/...` の形）。パスではなく範囲の指定である。
	elision = regexp.MustCompile(`\.\.\.`)
	// `pkg/foo.Bar` —— パスではなく、パッケージと公開シンボルの組。
	packageSymbol = regexp.MustCompile(`^(.*)\.[A-Z][A-Za-z0-9_]*$`)
	// glob とプレースホルダ。含むものは、実体ではなく形を指している。
	wildcard = regexp.MustCompile(`[*<]`)
)

// 番兵（ADR-0702 決定13）。「違反が無かった」と「何も見なかった」を区別するため、
// 違反（1）とは別の終了コードで返す。
var (
	errNoSkillDoc   = xerrors.New("スキルの文書が1件も見つかりません")
	errNoMakeTarget = xerrors.New("make のターゲットを1件も読めません")
	errNoReference  = xerrors.New("文書から参照を1件も読めません")
)

func main() {
	log.SetFlags(0)
	if err := run(os.Args[1:], os.Stdout); err != nil {
		if xerrors.Is(err, errNoSkillDoc) || xerrors.Is(err, errNoMakeTarget) || xerrors.Is(err, errNoReference) {
			log.Printf("❌ %s: %v", toolName, err)
			os.Exit(2)
		}
		log.Fatalf("❌ %s: %v", toolName, err)
	}
}

func run(args []string, out io.Writer) error {
	fs := flag.NewFlagSet(toolName, flag.ContinueOnError)
	fs.SetOutput(out)
	root := fs.String("root", ".", "リポジトリのルート")
	if err := fs.Parse(args); err != nil {
		return xerrors.Wrap(err, "引数の解釈")
	}

	targets, err := collectMakeTargets(*root)
	if err != nil {
		return err
	}
	if len(targets.exact) == 0 && len(targets.patterns) == 0 {
		return errNoMakeTarget
	}

	skills, err := collectSkills(*root)
	if err != nil {
		return err
	}

	docs, err := collectDocs(*root)
	if err != nil {
		return err
	}
	if len(docs) == 0 {
		return errNoSkillDoc
	}

	roots, err := rootEntries(*root)
	if err != nil {
		return err
	}

	var findings []lintreport.Finding
	refs := 0
	for _, doc := range docs {
		content, err := os.ReadFile(filepath.Clean(filepath.Join(*root, doc)))
		if err != nil {
			return xerrors.Wrapf(err, "%s の読み取り", doc)
		}
		f, n := checkDoc(doc, content, env{root: *root, roots: roots, targets: targets, skills: skills})
		findings = append(findings, f...)
		refs += n
	}

	// 対象の文書が在るのに参照が1件も取れないなら、壊れているのは抽出の側である。
	// ここを塞がないと、span を1つも取り出せなくなった日に「参照 0 件」で緑を返す。
	if refs == 0 {
		return errNoReference
	}

	if len(findings) > 0 {
		lintreport.Sort(findings)
		fmt.Fprintf(out, "❌ %s: %d 件の不整合\n\n", toolName, len(findings))
		fmt.Fprintln(out, lintreport.Format(findings))
		return xerrors.Newf("%d 件の不整合", len(findings))
	}

	fmt.Fprintf(out, "✅ %s: 文書 %d 件の参照 %d 件が実在することを確認しました\n", toolName, len(docs), refs)
	return nil
}

// env は1つの文書を検査するのに要る、リポジトリ側の実体です。
type env struct {
	root    string
	roots   map[string]bool
	targets targetSet
	skills  map[string]bool
}

// checkDoc は文書1件を検査し、違反と、実在を確かめた参照の件数を返します。
//
// 0 件が意味するもの（抽出の破損）とその扱いは、run の errNoReference の分岐が持ちます。
func checkDoc(doc string, content []byte, e env) ([]lintreport.Finding, int) {
	var findings []lintreport.Finding
	refs := 0
	dir := filepath.Dir(doc)

	for _, line := range mdscan.Lines(content) {
		for _, span := range mdscan.InlineCodes(line.Text) {
			switch {
			case makeInvocation.MatchString(span):
				for _, target := range makeTargets(span) {
					refs++
					if !e.targets.has(target) {
						findings = append(findings, lintreport.Finding{
							File: doc, Line: line.Number,
							Message: "存在しない make ターゲットを参照しています: `make " + target + "`",
						})
					}
				}
			case skillRef.MatchString(span):
				refs++
				if name := skillRef.FindStringSubmatch(span)[1]; !e.skills[name] {
					findings = append(findings, lintreport.Finding{
						File: doc, Line: line.Number,
						Message: "存在しないスキルを参照しています: `" + span + "`",
					})
				}
			default:
				path, ok := asRepoPath(span, e.roots, dir, e.root)
				if !ok {
					continue
				}
				refs++
				if !pathExists(e.root, dir, path) {
					findings = append(findings, lintreport.Finding{
						File: doc, Line: line.Number,
						Message: "存在しないパスを参照しています: `" + span + "`",
					})
				}
			}
		}
	}
	return findings, refs
}

// makeTargets は `make ...` の span からターゲット名を取り出します。
//
// make へ渡せない形の語が出た時点で**打ち切ります**。`make test 2>&1` の `2>&1` や
// `make DB=local migrate` の `DB=local` は、その後ろにターゲットが続くとは限らず、
// 読み違えて存在しない名前を報告するより、読まないほうが害が小さい。
func makeTargets(span string) []string {
	var targets []string
	for _, token := range strings.Fields(span)[1:] {
		if strings.HasPrefix(token, "-") {
			continue
		}
		if !makeArgument.MatchString(token) {
			break
		}
		targets = append(targets, token)
	}
	return targets
}

// targetSet は make が解決できるターゲットの集合です。
type targetSet struct {
	exact    map[string]bool
	patterns []*regexp.Regexp
}

func (t targetSet) has(name string) bool {
	if t.exact[name] {
		return true
	}
	for _, p := range t.patterns {
		if p.MatchString(name) {
			return true
		}
	}
	// 文書の側がプレースホルダで書いている場合、指しているのは特定のターゲットではなく形である。
	// その形に当たる実在のターゲットが1つでもあれば、記述は正しい。
	if wildcard.MatchString(name) {
		re := placeholderPattern(name)
		for exact := range t.exact {
			if re.MatchString(exact) {
				return true
			}
		}
	}
	return false
}

// collectMakeTargets は makefile と、そこが include する .mk からターゲット名を集めます。
//
// **`make help` の一覧を正としない。** あれは `.PHONY: <name> ##` という注記を持つものだけを
// 並べるため、注記を持たない help 自身が一覧に出ない。include を辿って宣言そのものを読む。
func collectMakeTargets(root string) (targetSet, error) {
	set := targetSet{exact: map[string]bool{}}

	content, err := os.ReadFile(filepath.Clean(filepath.Join(root, makefile)))
	if err != nil {
		return set, xerrors.Wrapf(err, "%s の読み取り", makefile)
	}
	files := []string{makefile}
	for _, m := range includeLine.FindAllStringSubmatch(string(content), -1) {
		files = append(files, m[1])
	}

	for _, f := range files {
		c, err := os.ReadFile(filepath.Clean(filepath.Join(root, f)))
		if err != nil {
			return set, xerrors.Wrapf(err, "%s の読み取り", f)
		}
		for _, name := range parseTargets(string(c)) {
			addTarget(&set, name)
		}
	}
	return set, nil
}

// parseTargets は makefile 断片1つからターゲット名を取り出します。
func parseTargets(content string) []string {
	var names []string
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "\t") {
			continue // レシピ行
		}
		if m := phonyLine.FindStringSubmatch(line); m != nil {
			decl, _, _ := strings.Cut(m[1], "##")
			names = append(names, strings.Fields(decl)...)
			continue
		}
		m := ruleLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		// 代入は規則ではない。先頭の `:` を落としてから見るのは、`X ::= v` が m[2] に `:= v` を
		// 残すためで、`=` だけを見ると VAR がターゲットとして集まる。`a:: b` は二重コロンの
		// 規則なので、落とした後に `=` が来ない形として残る。
		if strings.HasPrefix(strings.TrimLeft(m[2], ":"), "=") {
			continue
		}
		names = append(names, strings.Fields(m[1])...)
	}
	return names
}

// addTarget は集合へ1件加えます。`%` を含むものは接尾規則なので、当たる名前の形として持ちます。
func addTarget(set *targetSet, name string) {
	if name == "" || strings.HasPrefix(name, ".") {
		return // .PHONY / .DEFAULT_GOAL などの特殊ターゲット
	}
	if strings.Contains(name, "%") {
		parts := strings.Split(name, "%")
		for i, p := range parts {
			parts[i] = regexp.QuoteMeta(p)
		}
		set.patterns = append(set.patterns, regexp.MustCompile("^"+strings.Join(parts, ".+")+"$"))
		return
	}
	set.exact[name] = true
}

// placeholderPattern は、文書が書いたプレースホルダを、実在のターゲットに当てる形へ変えます。
func placeholderPattern(text string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(text); {
		switch text[i] {
		case '*':
			b.WriteString(".+")
			i++
		case '<':
			if j := strings.IndexByte(text[i:], '>'); j >= 0 {
				b.WriteString(".+")
				i += j + 1
				continue
			}
			b.WriteString(regexp.QuoteMeta(string(text[i])))
			i++
		default:
			b.WriteString(regexp.QuoteMeta(string(text[i])))
			i++
		}
	}
	b.WriteString("$")
	return regexp.MustCompile(b.String())
}

// collectSkills は .claude/skills/ 直下のディレクトリ名を集めます。
func collectSkills(root string) (map[string]bool, error) {
	entries, err := os.ReadDir(filepath.Join(root, skillsDir))
	if err != nil {
		return nil, xerrors.Wrapf(err, "%s の読み取り", skillsDir)
	}
	skills := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() {
			skills[e.Name()] = true
		}
	}
	return skills, nil
}

// collectDocs は検査対象の Markdown を、root からの相対パスで集めます。
//
// 相対で返すのは、違反の報告が読む人の作業ディレクトリに依らないためです。
func collectDocs(root string) ([]string, error) {
	dir := filepath.Join(root, skillsDir)
	var docs []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, skillDoc) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		docs = append(docs, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, xerrors.Wrapf(err, "%s の走査", skillsDir)
	}
	sort.Strings(docs)
	return docs, nil
}

// rootEntries は、パス参照の先頭セグメントとして認める名前を集めます。
//
// tmp/ を外すのは、gitignore された作業場所であり、在ることを前提にできないためです。
func rootEntries(root string) (map[string]bool, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, xerrors.Wrapf(err, "%s の読み取り", root)
	}
	roots := map[string]bool{}
	for _, e := range entries {
		if e.Name() != scratchDir && e.Name() != ".git" {
			roots[e.Name()] = true
		}
	}
	return roots, nil
}

// asRepoPath は、span がこのリポジトリのパスとして検査できる形かを判定します。
//
// 門を厳しくしているのは、**パスに見える文字列の大半がパスではない**からです。コマンド行、
// URL、Go の import path、`pkg.Symbol` の組 —— これらを対象に含めると、正しい記述が落ちます。
func asRepoPath(span string, roots map[string]bool, fromDir, root string) (string, bool) {
	text := strings.TrimPrefix(strings.TrimSpace(span), "./")
	if !strings.Contains(text, "/") {
		return "", false
	}
	if notAPath.MatchString(text) || elision.MatchString(text) {
		return "", false
	}
	isDir := strings.HasSuffix(text, "/")
	text = strings.TrimSuffix(text, "/")
	if text == "" {
		return "", false
	}
	// **`..` はここで弾かない。** 列挙の展開は後段で起きるため、`{a,..}` のように括弧の中へ
	// 隠されたものは、この時点では生のセグメント `{a,..}` にしか見えない。外へ出るかどうかは、
	// 展開し終えた候補に対して resolves が判定する。

	head, _, _ := strings.Cut(text, "/")
	if !roots[head] && !existsIn(filepath.Join(root, fromDir), head) {
		return "", false
	}
	// 拡張子を持たないファイル参照は、Go の import path や package path と区別できない。
	if !isDir && !strings.Contains(filepath.Base(text), ".") {
		return "", false
	}
	// `scripts/lib/xerrors.Wrap` —— 前半が実在するパスなら、これはシンボルの名指しである。
	if m := packageSymbol.FindStringSubmatch(text); m != nil && pathExists(root, fromDir, m[1]) {
		return "", false
	}
	return text, true
}

func existsIn(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

// pathExists は、候補がリポジトリのルートからでも、参照元の隣からでも解決するかを返します。
//
// `{a,b}` は列挙であり、**すべてが実在して初めて記述が正しい**。片方しか無い状態を通すと、
// 「3つを走査する」と書かれた手順が2つしか走査しない状態で緑になります。
func pathExists(root, fromDir, candidate string) bool {
	candidates, ok := expandBraces(candidate)
	if !ok {
		return false
	}
	for _, c := range candidates {
		if !resolves(root, fromDir, c) {
			return false
		}
	}
	return true
}

// maxBraceCandidates は `{a,b}` の展開を打ち切る上限です。
//
// 展開数は群の数に対して指数で増えます（`{a,b,c,d,e,f}` を9つ並べた135文字で1千万件）。
// 文書へ1行足すだけでゲートを潰せてしまうので、上限を超えたものは**検査できないもの**として
// 違反にします。黙って諦めると、そこだけ検査が消える。
const maxBraceCandidates = 64

// maxBraceGroups は、1つの候補が持てる `{` の総数の上限です。
//
// 候補数の上限だけでは足りません —— `{{{…a…}}}` は候補を1件しか生まないので上限に触れない
// 一方、1組剥がすたびに文字列全体を組み直すため、`{` の数に対して**二乗**の時間と記憶域を
// 使います。組み直しの費用は入れ子か並列かを区別しないので、数えるのは深さではなく総数です
// （実測で、入れ子 40,000 段が 3.9 秒・1.6GB、並列 32,000 群が 1.0 秒）。
const maxBraceGroups = 32

// expandBraces は `{a,b}` の列挙を、それぞれの候補へ展開します。入れ子も展開します。
// 展開数が上限を超えた場合は false を返します。
func expandBraces(text string) ([]string, bool) {
	if strings.Count(text, "{") > maxBraceGroups {
		return nil, false
	}
	begin := strings.IndexByte(text, '{')
	if begin < 0 {
		return []string{text}, true
	}
	end := matchingBrace(text, begin)
	if end < 0 {
		return []string{text}, true
	}

	var out []string
	for _, alt := range splitAlternatives(text[begin+1 : end]) {
		if alt == "" {
			continue // 空の選択肢は候補を1つも増やさない
		}
		expanded, ok := expandBraces(text[:begin] + alt + text[end+1:])
		if !ok {
			return nil, false
		}
		out = append(out, expanded...)
		if len(out) > maxBraceCandidates {
			return nil, false
		}
	}
	return out, true
}

// splitAlternatives は列挙の中身を、**最も外側の**カンマで分けます。
//
// 素朴に分けると入れ子の中のカンマでも割れます —— `b,{c,d}` が `b` / `{c` / `d}` の3つになり、
// 括弧の片割れを抱えた候補が生まれます。
func splitAlternatives(inner string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(inner); i++ {
		switch inner[i] {
		case '{':
			depth++
		case '}':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, inner[start:i])
				start = i + 1
			}
		}
	}
	return append(out, inner[start:])
}

// matchingBrace は begin の `{` に対応する `}` の位置を返します。無ければ -1 を返します。
//
// 最初の `}` を採ると入れ子で壊れます —— `{a,{b,c}}` の外側が内側の閉じ括弧で切れ、
// `a}` のような候補が混ざり、実在する参照が「存在しない」と報告されます。
func matchingBrace(text string, begin int) int {
	depth := 0
	for i := begin; i < len(text); i++ {
		switch text[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// resolves は、候補1つが実在するかを返します。
//
// glob とプレースホルダを含むものは、実体ではなく形を指しています。展開して総当たりする代わりに、
// **ワイルドカードより前の実在するディレクトリ**を見ます。形が指す先がまだ無いことは許されるが、
// 形が置かれる場所が無いことは誤りである、という線引きです。
func resolves(root, fromDir, candidate string) bool {
	if wildcard.MatchString(candidate) {
		parent := literalParent(candidate)
		if parent == "" {
			return true // 先頭セグメントからワイルドカードなら、確かめられる部分が無い
		}
		candidate = parent
	}
	for _, base := range []string{root, filepath.Join(root, fromDir)} {
		target := filepath.Join(base, filepath.FromSlash(candidate))
		// **リポジトリの外は見ない。** ここが最後の関門である —— `..` は列挙の中へ隠せるので、
		// 文字列を見て弾く門はすり抜けられる。Stat する直前に、解決した先が root の下へ
		// 収まっていることを確かめる。存在の有無だけでも、文書へ1行足せば外を覗けてしまう。
		if escapes(root, target) {
			continue
		}
		if _, err := os.Stat(target); err != nil {
			continue
		}
		// **字面が内側でも、実体は外側であり得る。** os.Stat は symlink を辿るのに対し
		// escapes は文字列しか見ないので、リポジトリの中から外を指す symlink は2つの間を
		// すり抜ける。辿った先に対してもう一度確かめる。
		if escapesReal(root, target) {
			continue
		}
		return true
	}
	return false
}

// escapes は、target が root の外へ出ているかを返します。
func escapes(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return true
	}
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// escapesReal は、symlink を辿った実体が root の外にあるかを返します。
//
// root 自身も symlink の下に在り得る（macOS の `/var` は `/private/var` を指す）ので、
// 両側を同じ規則で解決してから比べます。解決できないものは外として扱います —— 判定が
// できないときに通す門は、門を置かないことと変わりません。
func escapesReal(root, target string) bool {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return true
	}
	realTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return true
	}
	return escapes(realRoot, realTarget)
}

// literalParent は、最初にワイルドカードを含むセグメントより前の部分を返します。
func literalParent(candidate string) string {
	segments := strings.Split(candidate, "/")
	for i, s := range segments {
		if wildcard.MatchString(s) {
			return strings.Join(segments[:i], "/")
		}
	}
	return candidate
}
