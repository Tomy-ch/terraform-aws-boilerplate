// adr-lint は ADR の構造を検査します。
//
// **ADR-0001 が自らの検証方法として挙げた項目を、そのまま実行するのがこのツールです。**
// 規範を定めた ADR が自分の検査手段を持たない状態は、ADR-0401 決定3（検証できない主張を
// 保証として扱わない）に自ら反します。
//
//  1. ファイル名が NNNN-kebab-case-title.md に適合すること
//  2. 同一 scope 内で番号が重複しないこと
//  3. 先頭メタデータに Status / Date / Scope が存在すること
//  4. Status が `Superseded by` の場合、参照先 ADR が存在すること
//  5. root ADR 本文が usecases/<use-case>/ そのもの、またはその配下の path へ規範的に依存していないこと
//  6. 本文が参照する、scope を伴わない ADR-NNNN がすべて同じ scope に実在すること
//  7. 見出しが名乗る番号が、ファイル名の番号と一致すること
//  8. docs/adr/ の**外**から ADR を指すリンクが、実在する ADR を指していること
//  9. 索引の Status 列が、本文の Status と一致すること
//  10. Status が ADR-0001 決定3 の値のいずれかであること
//  11. 本文が参照する、scope を伴う `<scope>/ADR-NNNN` が、その scope に実在すること
//  12. 同一 scope 内で slug が重複しないこと
//
// 6 と 7 は、番号が identity ではなく順序になったことから要る（ADR-0001 決定5-9）。
// 番号は削除に伴って詰められるため、**参照が黙って別の決定を指す**経路が開く。
//
// 6 だけでは塞がらない。詰め直しの取りこぼしは、指す先が**無い**形ではなく、実在する
// **別の**決定を指す形で現れるからで、そのとき誤記が唯一残っているのは見出しである。
// 7 が無ければ、2つのファイルが同じ番号を名乗っていても全件が緑で通る。
//
// 8 が 6 と別に要るのは、**参照の主要な書式が docs/adr/ の外に在る**からです。AGENTS.md や
// 各 README が ADR を指すときの形は `[0101](docs/adr/0101-architecture-principles.md)` であり、
// 6 の走査はそこへ届きません。番号を詰めた瞬間、外側のリンクは実在する**別の**決定を指します。
// リンクの文言（`[0101]`）と指し先の番号を突き合わせるのは、詰め直しの取りこぼしが
// 「指す先が無い」形ではなく「文言だけが古い」形で現れるためです。
//
// 併せて、索引（README.md）が実ファイルと食い違っていないことも見ます。索引は
// その scope の「ADR の一覧が存在する唯一の場所」であり、そこがずれると読み手は決定へ到達できません。
//
// 検査は scope ごとに掛けます。scope は -root の root scope と、-repo 配下の
// usecases/<use-case>/docs/adr/ を持つユースケースです（ADR-0001 決定12）。1-3・6・7・9・10・12 は
// scope の内側で完結し、5 は root scope だけに掛け、8 は use-case ADR の置き場所も「外」として読みます。
// 4 と 11 は宛先が別の scope に在りうるため、すべての scope を読み終えてから掛けます。
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lintreport"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/mdscan"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	defaultRepo  = "."
	defaultRoot  = "docs/adr"
	indexFile    = "README.md"
	templateFile = "template.md"
	// root scope の名前。`Superseded by <scope>/...` の scope と同じ綴りである。
	rootScope = "repository-wide"
	// use-case ADR の置き場所は usecases/<use-case>/docs/adr/（ADR-0001 決定12）。
	useCasesDir = "usecases"
	useCaseADR  = "docs/adr"
	scopeChars  = `[A-Za-z0-9_-]+`
)

var (
	// 大文字とアンダースコアを弾くのは、同じ決定が2つの綴りで置かれるのを防ぐため。
	fileName   = regexp.MustCompile(`^(\d{4})-([a-z0-9]+(?:-[a-z0-9]+)*)\.md$`)
	statusLine = regexp.MustCompile(`(?m)^- Status:[ \t]*(\S.*)$`)
	dateLine   = regexp.MustCompile(`(?m)^- Date:[ \t]*(\S.*)$`)
	scopeLine  = regexp.MustCompile(`(?m)^- Scope:[ \t]*(\S.*)$`)
	// 宛先は identity の `<scope>/<slug>` で書く（ADR-0001 決定3・4）。slug の形は fileName の第2群と同じ。
	supersededBy = regexp.MustCompile(`^Superseded by\s+(` + scopeChars + `)/([a-z0-9]+(?:-[a-z0-9]+)*)\s*$`)
	indexRow     = regexp.MustCompile(`^\|\s*\[(\d{4})\]\(([^)]+)\)\s*\|`)
	// Status は行の最後のセルとする。最後のセルならタイトルに `|` や `](` が入っても位置がずれない。
	// リンクのセルと Status の間にセルが1つも無い行は、Status 列を持たない。
	indexStatus = regexp.MustCompile(`^\|\s*\[\d{4}\]\([^)]+\)\s*\|.*\|[ \t]*([^|]*?)[ \t]*\|[ \t]*$`)
	headingLine = regexp.MustCompile(`(?m)^# ADR-(\d{4})\b`)
	// scope を伴う参照（`<scope>/ADR-NNNN`）は除く。宛先の番号体系が別の scope に在り、scopedRef が引く。
	adrRef = regexp.MustCompile(`(^|[^/\w])ADR-(\d{4})`)
	// adrRef と同じ出現に当たらない。scope の左に境界を置くのは、`repository-wide` を `wide` と取らないため。
	// `/` と `.` を境界に含めないのは、`docs/adr/ADR-0001` のような path の一部を参照と読まないため。
	scopedRef = regexp.MustCompile(`(?:^|[^A-Za-z0-9_/.-])(` + scopeChars + `)/ADR-(\d{4})`)
	// scope の名前に使える文字。scopedRef と supersededBy が名前をこの形で切り出すため、外れた名前の
	// ユースケースを指す参照は、どちらにも当たらないまま検査の外に残る。
	scopeName = regexp.MustCompile(`^` + scopeChars + `$`)
	// 先頭の境界は消費して捕獲群 1 だけを報告に使う。RE2 は後読みを持たず、`\b` は `-` と
	// 日本語の直後で立たない。`/` を境界に含めるのは、相対パスで書いた path を取り逃さないため。
	useCasePath    = regexp.MustCompile(`(?:^|[^A-Za-z0-9_-])(usecases/(?:<use-case>|[a-z0-9-]+)/[a-zA-Z0-9_./*-]*)`)
	placeholder    = regexp.MustCompile(`usecases/<use-case>/`)
	markdownLink   = regexp.MustCompile(`\[([^\]]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
	linkTextNumber = regexp.MustCompile(`^(?:ADR-)?(\d{4})$`)
	useCaseADRDir  = regexp.MustCompile(`^` + useCasesDir + `/[^/]+/` + useCaseADR + `$`)
	pathToken      = regexp.MustCompile(`[A-Za-z0-9_./-]+\.md`)
	// このリポジトリの作業ツリーの外を指すリンク。scheme 付きの URL と、サイトのルートからの
	// 絶対パスが該当する。**`/docs/adr/0001-x.md` は作業ツリーの docs/adr/ ではない** ——
	// 相対として解決すると同じ場所に見えるため、ここで分けないと外部の参照を自分の番号体系で裁く。
	externalLink = regexp.MustCompile(`^(?:[a-z][a-z0-9+.-]*:|/)`)
)

// 番兵（ADR-0702 決定13・15）。検査対象0件、および抽出の取りこぼしを、成功として終えない。
var (
	errNoADR       = xerrors.New("ADR が1件も見つかりません")
	errNoMarkdown  = xerrors.New("ADR を指し得る Markdown が1件も見つかりません")
	errNoReference = xerrors.New("ADR を名指しした文書から、リンクを1件も取り出せません")
	errNoScopedRef = xerrors.New("scope を伴う ADR 参照を含む行から、参照を1件も取り出せません")
)

// scope の発見で、置き場所を解釈できなかったときの失敗。どれも黙って検査の外へ倒さない（ADR-0702 決定14）。
var (
	errSymlink       = xerrors.New("シンボリックリンクは scope の置き場所として扱いません")
	errReservedScope = xerrors.New("root scope と同じ名前のユースケースは置けません")
	errNotDirectory  = xerrors.New("ADR の置き場所がディレクトリではありません")
	errScopeName     = xerrors.New("ユースケースの名前が scope として使えない文字を含みます")
)

func main() {
	log.SetFlags(0)
	if err := run(os.Args[1:], os.Stdout); err != nil {
		if xerrors.Is(err, errNoADR) || xerrors.Is(err, errNoMarkdown) || xerrors.Is(err, errNoReference) ||
			xerrors.Is(err, errNoScopedRef) {
			log.Printf("❌ adr-lint: %v", err)
			os.Exit(2)
		}
		log.Fatalf("❌ adr-lint: %v", err)
	}
}

func run(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("adr-lint", flag.ContinueOnError)
	fs.SetOutput(out)
	root := fs.String("root", defaultRoot, "ADR を探すディレクトリ")
	repo := fs.String("repo", defaultRepo, "ADR を指すリンクを探すリポジトリのルート")
	if err := fs.Parse(args); err != nil {
		return xerrors.Wrap(err, "引数の解釈")
	}

	scopes, skipped, err := discoverScopes(*repo, *root)
	if err != nil {
		return err
	}

	var findings []lintreport.Finding
	checked := make([]scopeDocs, 0, len(scopes))
	total := 0
	for _, s := range scopes {
		docs, scopeFindings, err := checkScope(s)
		if err != nil {
			return err
		}
		findings = append(findings, scopeFindings...)
		checked = append(checked, scopeDocs{scope: s, docs: docs})
		total += len(docs)
	}
	findings = append(findings, checkSupersede(checked)...)
	scopedFindings, scopedRefs, err := checkScopedReferences(checked)
	if err != nil {
		return err
	}
	findings = append(findings, scopedFindings...)

	pathFindings, refs, err := checkPathReferences(*repo, checked)
	if err != nil {
		return err
	}
	findings = append(findings, pathFindings...)

	if len(findings) > 0 {
		lintreport.Sort(findings)
		fmt.Fprintf(out, "❌ adr-lint: %d 件の不整合\n\n", len(findings))
		fmt.Fprintln(out, lintreport.Format(findings))
		return xerrors.Newf("%d 件の不整合", len(findings))
	}

	fmt.Fprintf(out, "✅ adr-lint: ADR %d 件（%d scope）の構造と、scope を伴う参照 %d 件と、外からの参照 %d 件を確認しました\n",
		total, len(scopes), scopedRefs, refs)
	if len(skipped) > 0 {
		fmt.Fprintf(out, "   docs/adr を持たないユースケース: %s\n", strings.Join(skipped, ", "))
	}
	return nil
}

// scope は番号体系を共有する ADR の置き場所です。番号は scope ごとに独立します（ADR-0001 決定11）。
type scope struct {
	name string // root scope は rootScope、use-case scope はユースケースの名前
	dir  string
}

type scopeDocs struct {
	scope scope
	docs  []doc
}

// discoverScopes は、root scope と、repo 配下で docs/adr/ を持つユースケースの scope を返します。
//
// use-case scope を引数ではなく発見で得るのは、置き場所を足しただけで検査対象に入るようにするためです。
// 呼び出し側が列挙すると、列挙し忘れた scope が黙って検査の外に残ります。
//
// docs/adr/ を持たないユースケースは scope にせず、その名前を返します。綴り違いの置き場所も同じ分岐に
// 落ちるため、名前を出さなければ黙って検査の外に残ります。
func discoverScopes(repo, root string) ([]scope, []string, error) {
	scopes := []scope{{name: rootScope, dir: root}}

	base := filepath.Join(repo, useCasesDir)
	entries, err := os.ReadDir(base)
	if xerrors.Is(err, os.ErrNotExist) {
		return scopes, nil, nil
	}
	if err != nil {
		return nil, nil, xerrors.Wrapf(err, "%s の読み取り", base)
	}

	var skipped []string
	for _, e := range entries {
		// DirEntry.IsDir はシンボリックリンクに false を返す。飛ばせば、そのユースケースが黙って外れる。
		if e.Type()&os.ModeSymlink != 0 {
			return nil, nil, xerrors.Wrapf(errSymlink, "%s", filepath.Join(base, e.Name()))
		}
		if !e.IsDir() {
			continue
		}
		// 同じ名前だと、checkScope と checkPathReferences がこの scope を root として扱う。
		if e.Name() == rootScope {
			return nil, nil, xerrors.Wrapf(errReservedScope, "%s", filepath.Join(base, e.Name()))
		}
		if !scopeName.MatchString(e.Name()) {
			return nil, nil, xerrors.Wrapf(errScopeName, "%s", filepath.Join(base, e.Name()))
		}
		dir := filepath.Join(base, e.Name(), filepath.FromSlash(useCaseADR))
		info, err := os.Lstat(dir)
		if xerrors.Is(err, os.ErrNotExist) {
			skipped = append(skipped, e.Name())
			continue
		}
		if err != nil {
			return nil, nil, xerrors.Wrapf(err, "%s の確認", dir)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, nil, xerrors.Wrapf(errSymlink, "%s", dir)
		}
		if !info.IsDir() {
			return nil, nil, xerrors.Wrapf(errNotDirectory, "%s", dir)
		}
		scopes = append(scopes, scope{name: e.Name(), dir: dir})
	}
	return scopes, skipped, nil
}

// checkScope は、1つの scope の ADR を読み出し、scope の内側で完結する検査を掛けます。
func checkScope(s scope) ([]doc, []lintreport.Finding, error) {
	docs, findings, err := collect(s.dir)
	if err != nil {
		return nil, nil, err
	}
	// 「ADR が1件も無い」と「全てのファイル名が規約違反で検査対象から外れた」は別である。
	// 後者を番兵で返すと、直すべき違反が出力されないまま終わる。
	if len(docs) == 0 && len(findings) == 0 {
		return nil, nil, xerrors.Wrapf(errNoADR, "%s", s.dir)
	}

	findings = append(findings, checkNumbers(docs, s.dir)...)
	findings = append(findings, checkHeading(docs)...)
	findings = append(findings, checkSlugs(docs, s.dir)...)
	findings = append(findings, checkMetadata(docs)...)
	findings = append(findings, checkStatusValue(docs)...)
	// 決定19 が縛るのは root ADR だけである。use-case ADR が自分の path を名指しするのは正しい。
	if s.name == rootScope {
		findings = append(findings, checkUseCaseDependency(docs)...)
	}
	findings = append(findings, checkCrossReferences(docs)...)

	indexFindings, err := checkIndex(s.dir, docs)
	if err != nil {
		return nil, nil, err
	}
	return docs, append(findings, indexFindings...), nil
}

// doc は検査対象の ADR 1件です。
type doc struct {
	path   string // root からの相対パス
	name   string
	number int
	slug   string // ファイル名から番号を除いた部分。identity は `<scope>/<slug>`（ADR-0001 決定4）
	source string
}

// collect は ADR を読み出します。ファイル名の規約違反は、番号を取れないため
// ここで違反として返し、以降の検査対象から外します。
func collect(root string) ([]doc, []lintreport.Finding, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, nil, xerrors.Wrapf(err, "%s の読み取り", root)
	}

	var docs []doc
	var findings []lintreport.Finding

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".md") {
			continue
		}
		if name == indexFile || name == templateFile {
			continue
		}

		path := filepath.Join(root, name)
		m := fileName.FindStringSubmatch(name)
		if m == nil {
			findings = append(findings, lintreport.Finding{
				File: path, Line: 1,
				Message: "ファイル名が `NNNN-kebab-case-title.md` に適合しません",
			})
			continue
		}

		raw, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return nil, nil, xerrors.Wrapf(err, "%s の読み取り", path)
		}
		num, _ := strconv.Atoi(m[1])
		docs = append(docs, doc{path: path, name: name, number: num, slug: m[2], source: string(raw)})
	}

	sort.Slice(docs, func(i, j int) bool { return docs[i].name < docs[j].name })
	return docs, findings, nil
}

// checkNumbers は同一 scope 内での番号の重複を検出します。削除に伴う詰め直しは許容します（ADR-0001 決定5-7）。
func checkNumbers(docs []doc, root string) []lintreport.Finding {
	seen := make(map[int][]string, len(docs))
	for _, d := range docs {
		seen[d.number] = append(seen[d.number], d.name)
	}

	var findings []lintreport.Finding
	numbers := make([]int, 0, len(seen))
	for n := range seen {
		numbers = append(numbers, n)
	}
	sort.Ints(numbers)

	for _, n := range numbers {
		names := seen[n]
		if len(names) <= 1 {
			continue
		}
		findings = append(findings, lintreport.Finding{
			File: root, Line: 1,
			Message: fmt.Sprintf("番号 %04d が重複しています（%s）", n, strings.Join(names, " / ")),
		})
	}
	return findings
}

// checkSlugs は同一 scope 内での slug の重複を検出します。
//
// identity は `<scope>/<slug>` であり（ADR-0001 決定4）、`Superseded by` は slug で宛先を引きます。
// 番号だけが違う2つのファイルが同じ slug を名乗ると、宛先がどちらの決定か決まりません。
func checkSlugs(docs []doc, root string) []lintreport.Finding {
	seen := make(map[string][]string, len(docs))
	for _, d := range docs {
		seen[d.slug] = append(seen[d.slug], d.name)
	}

	var findings []lintreport.Finding
	slugs := make([]string, 0, len(seen))
	for slug := range seen {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)

	for _, slug := range slugs {
		names := seen[slug]
		if len(names) <= 1 {
			continue
		}
		findings = append(findings, lintreport.Finding{
			File: root, Line: 1,
			Message: fmt.Sprintf("slug %s が重複しています（%s）", slug, strings.Join(names, " / ")),
		})
	}
	return findings
}

// checkHeading は、本文の H1 が名乗る番号がファイル名の番号と一致することを検査します。
//
// **ここは checkCrossReferences が構造的に見られない場所です。** あちらは自分自身への言及を
// 参照から除いており、見出しはその除外に当たります。番号を詰めたときに見出しの更新を落とすと、
// 2つのファイルが同じ番号を名乗る状態が、参照の実在検査を全件通過したまま残ります。
func checkHeading(docs []doc) []lintreport.Finding {
	var findings []lintreport.Finding
	for _, d := range docs {
		m := headingLine.FindStringSubmatch(d.source)
		if m == nil {
			findings = append(findings, lintreport.Finding{
				File: d.path, Line: 1,
				Message: "本文に `# ADR-NNNN: <題>` の見出しがありません",
			})
			continue
		}

		num, _ := strconv.Atoi(m[1])
		if num != d.number {
			findings = append(findings, lintreport.Finding{
				File: d.path, Line: 1,
				Message: fmt.Sprintf("見出しが ADR-%04d を名乗っていますが、ファイル名の番号は %04d です", num, d.number),
			})
		}
	}
	return findings
}

// checkMetadata は先頭メタデータの存在を検査します。値の形は問いません。
func checkMetadata(docs []doc) []lintreport.Finding {
	var findings []lintreport.Finding
	for _, d := range docs {
		for _, want := range []struct {
			key string
			re  *regexp.Regexp
		}{
			{"Status", statusLine},
			{"Date", dateLine},
			{"Scope", scopeLine},
		} {
			if !want.re.MatchString(d.source) {
				findings = append(findings, lintreport.Finding{
					File: d.path, Line: 1,
					Message: fmt.Sprintf("先頭メタデータに `%s` がありません", want.key),
				})
			}
		}
	}
	return findings
}

func statusOf(d doc) (string, bool) {
	m := statusLine.FindStringSubmatch(d.source)
	if m == nil {
		return "", false
	}
	return strings.TrimSpace(m[1]), true
}

// metadataLine は、先頭メタデータの Status 行が本文の何行目か（0 始まり）を返します。無ければ -1 を返します。
//
// statusOf と同じく最初の一致をメタデータとして扱います。本文に同じ形の行が続いても、それは参照を持つ本文です。
func metadataLine(source string) int {
	loc := statusLine.FindStringIndex(source)
	if loc == nil {
		return -1
	}
	return strings.Count(source[:loc[0]], "\n")
}

// checkStatusValue は、Status が ADR-0001 決定3 の値のいずれかであることを検査します。
//
// `Superseded by` で始まるものの宛先の形は checkSupersede が見ます。
func checkStatusValue(docs []doc) []lintreport.Finding {
	var findings []lintreport.Finding
	for _, d := range docs {
		status, ok := statusOf(d)
		if !ok {
			continue // 不在は checkMetadata が報告済み
		}
		switch {
		case status == "Proposed", status == "Accepted", status == "Deprecated":
		case strings.HasPrefix(status, "Superseded by"):
		default:
			findings = append(findings, lintreport.Finding{
				File: d.path, Line: 1,
				Message: fmt.Sprintf("Status が `Proposed` / `Accepted` / `Superseded by <scope>/<slug>` / `Deprecated` のいずれでもありません（%s）", status),
			})
		}
	}
	return findings
}

// checkSupersede は Status が `Superseded by` の場合に、宛先の ADR の実在を検査します。
//
// 参照先が無いまま supersede を宣言すると、読み手は「置き換わった先」へ到達できません。宛先は
// 別の scope に在りうるため（ADR-0001 決定23）、すべての scope を読み終えてから引きます。
func checkSupersede(scopes []scopeDocs) []lintreport.Finding {
	slugs := make(map[string]map[string]bool, len(scopes))
	for _, s := range scopes {
		set := make(map[string]bool, len(s.docs))
		for _, d := range s.docs {
			set[d.slug] = true
		}
		slugs[s.scope.name] = set
	}

	var findings []lintreport.Finding
	for _, s := range scopes {
		for _, d := range s.docs {
			status, ok := statusOf(d)
			if !ok || !strings.HasPrefix(status, "Superseded by") {
				continue
			}
			at := func(msg string) {
				findings = append(findings, lintreport.Finding{File: d.path, Line: 1, Message: msg})
			}

			sm := supersededBy.FindStringSubmatch(status)
			if sm == nil {
				at(fmt.Sprintf("Status の形式が `Superseded by <scope>/<slug>` ではありません（%s）", status))
				continue
			}
			target, slug := sm[1], sm[2]
			set, ok := slugs[target]
			switch {
			case !ok:
				at(fmt.Sprintf("Superseded by の宛先の scope %s が見つかりません", target))
			case target == s.scope.name && slug == d.slug:
				at("Superseded by が自分自身を指しています")
			case !set[slug]:
				at(fmt.Sprintf("Superseded by の参照先 %s/%s が存在しません", target, slug))
			}
		}
	}
	return findings
}

// checkUseCaseDependency は、root ADR 本文が特定ユースケースの実装詳細へ依存していないかを検査します。
//
// 依存の向きは root ADR → use-case ADR の一方向に限ります（ADR-0001 決定17-19）。
// `usecases/<use-case>/` のようなプレースホルダは、特定のユースケースを名指ししていないため許容します。
func checkUseCaseDependency(docs []doc) []lintreport.Finding {
	var findings []lintreport.Finding
	for _, d := range docs {
		for i, line := range strings.Split(d.source, "\n") {
			for _, m := range useCasePath.FindAllStringSubmatch(line, -1) {
				hit := m[1]
				if placeholder.MatchString(hit) {
					continue
				}
				findings = append(findings, lintreport.Finding{
					File: d.path, Line: i + 1,
					Message: fmt.Sprintf(
						"root ADR が特定ユースケースの path を名指ししています（%s）"+
							"。依存の向きは root → use-case の一方向に限ります", hit),
				})
			}
		}
	}
	return findings
}

// checkCrossReferences は、本文が参照する ADR-NNNN がすべて実在することを検査します。
//
// 番号は identity ではなく順序であり、削除に伴って詰められます（ADR-0001 決定5-6）。
// 詰め直しで参照の更新を落とすと、**参照が黙って別の決定を指します。** 実在の検査は
// 「指す先が無い」ことしか捕まえられませんが、詰め直しの取りこぼしはその形で現れます。
func checkCrossReferences(docs []doc) []lintreport.Finding {
	exists := make(map[int]bool, len(docs))
	for _, d := range docs {
		exists[d.number] = true
	}

	var findings []lintreport.Finding
	for _, d := range docs {
		status := metadataLine(d.source)
		for i, line := range strings.Split(d.source, "\n") {
			// Status 行の宛先は checkSupersede が見る。
			if i == status {
				continue
			}
			for _, m := range adrRef.FindAllStringSubmatch(line, -1) {
				num, _ := strconv.Atoi(m[2])
				// 自分自身への言及（見出しなど）は参照ではない。
				if num == d.number || exists[num] {
					continue
				}
				findings = append(findings, lintreport.Finding{
					File: d.path, Line: i + 1,
					Message: fmt.Sprintf("参照先の ADR-%s が存在しません", m[2]),
				})
			}
		}
	}
	return findings
}

// checkScopedReferences は、本文が参照する `<scope>/ADR-NNNN` が、その scope に実在することを検査し、
// 違反と参照の件数を返します。
//
// scope を伴わない参照は checkCrossReferences が自 scope で引きます。Status 行は checkSupersede が
// 見るので、ここでは読みません。
//
// **抽出を、正規表現を使わない経路と行ごとに突き合わせます**（ADR-0702 決定15）。scope を伴う
// 参照が0件なのは正常な状態なので、0件そのものは落としません。落とすのは、`/ADR-NNNN` を含む行から
// scopedRef が1件も取り出せないときです。木全体で突き合わせると、取り出せた参照が1件在るだけで
// 別の行の取りこぼしが隠れます。
func checkScopedReferences(scopes []scopeDocs) ([]lintreport.Finding, int, error) {
	numbers := make(map[string]map[int]bool, len(scopes))
	for _, s := range scopes {
		set := make(map[int]bool, len(s.docs))
		for _, d := range s.docs {
			set[d.number] = true
		}
		numbers[s.scope.name] = set
	}

	var findings []lintreport.Finding
	refs := 0
	for _, s := range scopes {
		for _, d := range s.docs {
			status := metadataLine(d.source)
			for i, line := range strings.Split(d.source, "\n") {
				if i == status {
					continue
				}
				matches := scopedRef.FindAllStringSubmatch(line, -1)
				if len(matches) == 0 && mentionsScopedADR(line) {
					return nil, 0, xerrors.Wrapf(errNoScopedRef, "%s:%d", d.path, i+1)
				}
				for _, m := range matches {
					refs++
					target, numStr := m[1], m[2]
					set, ok := numbers[target]
					num, _ := strconv.Atoi(numStr)
					var msg string
					switch {
					case !ok:
						msg = fmt.Sprintf("参照先 %s/ADR-%s の scope %s が見つかりません", target, numStr, target)
					case !set[num]:
						msg = fmt.Sprintf("参照先の %s/ADR-%s が存在しません", target, numStr)
					default:
						continue
					}
					findings = append(findings, lintreport.Finding{File: d.path, Line: i + 1, Message: msg})
				}
			}
		}
	}
	return findings, refs, nil
}

// mentionsScopedADR は、行が `<文字>/ADR-NNNN` の形を含むかを、正規表現を使わずに判定します。
//
// scopedRef と同じ形を見ると突き合わせが成立しないので、左の境界も scope の文字種も問いません。
func mentionsScopedADR(line string) bool {
	const marker = "/ADR-"
	for rest := line; ; {
		i := strings.Index(rest, marker)
		if i < 0 {
			return false
		}
		digits := rest[i+len(marker):]
		if i > 0 && len(digits) >= 4 && strings.Trim(digits[:4], "0123456789") == "" {
			return true
		}
		rest = rest[i+len(marker):]
	}
}

// checkIndex は索引が実ファイルと一致していることを検査します。
//
// 索引はその scope の「ADR の一覧が存在する唯一の場所」であり、そこがずれると読み手は決定へ到達できません。
// Status 列は、索引の行が指すファイルが実ファイルと一致するときだけ本文と比べます。番号の詰め直しの
// 途中では、行が別の ADR の Status を持っているためです。
func checkIndex(root string, docs []doc) ([]lintreport.Finding, error) {
	path := filepath.Join(root, indexFile)
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, xerrors.Wrapf(err, "%s の読み取り", path)
	}

	type entry struct {
		file     string
		status   string
		statusOK bool
		line     int
	}
	var findings []lintreport.Finding
	listed := make(map[int]entry)
	for i, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimRight(line, "\r")
		m := indexRow.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		num, _ := strconv.Atoi(m[1])
		if first, dup := listed[num]; dup {
			findings = append(findings, lintreport.Finding{
				File: path, Line: i + 1,
				Message: fmt.Sprintf("索引に ADR-%04d の行が重複しています（最初の行は %d 行目）", num, first.line),
			})
			continue
		}
		e := entry{file: m[2], line: i + 1}
		if sm := indexStatus.FindStringSubmatch(line); sm != nil && sm[1] != "" {
			e.status, e.statusOK = sm[1], true
		}
		listed[num] = e
	}

	for _, d := range docs {
		e, ok := listed[d.number]
		if !ok {
			findings = append(findings, lintreport.Finding{
				File: path, Line: 1,
				Message: d.name + " が索引に載っていません",
			})
			continue
		}
		delete(listed, d.number)
		at := func(msg string) {
			findings = append(findings, lintreport.Finding{File: path, Line: e.line, Message: msg})
		}
		if e.file != d.name {
			at(fmt.Sprintf("索引の ADR-%04d が %s を指していますが、実ファイルは %s です", d.number, e.file, d.name))
			continue
		}
		if !e.statusOK {
			at(fmt.Sprintf("索引の ADR-%04d の行から Status 列を読めません", d.number))
			continue
		}
		if status, ok := statusOf(d); ok && status != e.status {
			at(fmt.Sprintf("索引の ADR-%04d の Status は %q ですが、本文は %q です", d.number, e.status, status))
		}
	}

	numbers := make([]int, 0, len(listed))
	for n := range listed {
		numbers = append(numbers, n)
	}
	sort.Ints(numbers)
	for _, n := range numbers {
		findings = append(findings, lintreport.Finding{
			File: path, Line: listed[n].line,
			Message: fmt.Sprintf("索引の ADR-%04d（%s）に対応する実ファイルがありません", n, listed[n].file),
		})
	}

	return findings, nil
}

// checkPathReferences は、root の docs/adr/ の**外**から ADR を指すリンクを検査し、違反と参照の件数を返します。
//
// リンクは、指し先のディレクトリが属する scope の ADR と突き合わせます。
//
// use-case ADR の置き場所は「外」として読みます。use-case ADR から root ADR へのリンク（決定18）は、
// root の番号を詰めたときに壊れる主要な経路だからです。
//
// 件数を返すのは、**0件のまま緑を返す経路を呼び出し側が塞げるようにする**ためです。走査の範囲が
// 壊れたとき、この検査は違反0件の合格として現れます。
func checkPathReferences(repo string, scopes []scopeDocs) ([]lintreport.Finding, int, error) {
	// scope の置き場所は「ファイルシステム上の位置」として渡ってくるが、リンクの指し先と突き合わせるには
	// repo からの相対で要る。両方を1つで兼ねると、repo とは別の場所を -root に渡したとき、
	// 突合が一度も成立しないまま緑になる。
	indexes := make(map[string]map[int]string, len(scopes))
	dirs := make(map[string]bool, len(scopes))
	rootDir := ""
	for _, s := range scopes {
		dir, err := filepath.Rel(repo, s.scope.dir)
		if err != nil {
			return nil, 0, xerrors.Wrapf(err, "%s から見た %s の位置", repo, s.scope.dir)
		}
		dir = filepath.ToSlash(dir)
		index := make(map[int]string, len(s.docs))
		for _, d := range s.docs {
			index[d.number] = d.name
		}
		indexes[dir] = index
		dirs[dir] = true
		if s.scope.name == rootScope {
			rootDir = dir
		}
	}

	files, err := collectMarkdown(repo, rootDir)
	if err != nil {
		return nil, 0, err
	}
	if len(files) == 0 {
		return nil, 0, errNoMarkdown
	}

	var findings []lintreport.Finding
	refs := make(map[string]int, len(dirs))
	mentions := make(map[string]int, len(dirs))
	for _, file := range files {
		raw, err := os.ReadFile(filepath.Clean(filepath.Join(repo, filepath.FromSlash(file))))
		if err != nil {
			return nil, 0, xerrors.Wrapf(err, "%s の読み取り", file)
		}
		from := path.Dir(file)
		for _, line := range mdscan.Lines(raw) {
			for dir := range mentionedDirs(line.Text, from, indexes) {
				mentions[dir]++
			}
			for _, m := range markdownLink.FindAllStringSubmatch(line.Text, -1) {
				if unknown, ok := unknownScopeDir(m[2], from, dirs); ok {
					findings = append(findings, lintreport.Finding{
						File: file, Line: line.Number,
						Message: "どの scope にも当たらない ADR の置き場所を指しています: " + unknown,
					})
					continue
				}
				dir, name, ok := adrTarget(m[2], from, dirs)
				// 同じ置き場所の中のリンクは「外から」ではない。索引の行は checkIndex が、本文の番号は
				// checkCrossReferences が既に見ており、ここで数えると同じずれを二重に報告する。
				if !ok || dir == from {
					continue
				}
				refs[dir]++
				findings = append(findings, matchReference(file, line.Number, m[1], name, indexes[dir])...)
			}
		}
	}

	// **抽出の件数を、独立した経路で得た件数と突き合わせる**（ADR-0702 決定15）。実在する ADR の
	// ファイル名を含む行は在るのに、リンクを1件も取り出せないなら、壊れているのは解析の側である。
	// 突き合わせが無ければ、文書が解析の効かない書式（reference-style link など）へ寄った日に、
	// この検査は「外からの参照 0 件」で緑を返し続ける。
	//
	// 数えるのを「実在する ADR のファイル名」にしているのは、解析が**正しく退けた**もの
	// （外部 URL、索引、雛形）で食い違わせないためである。突き合わせを scope ごとに行うのは、
	// 合算すると、ある scope へのリンクが1件取れただけで別の scope の取りこぼしが隠れるためである。
	mentioned := make([]string, 0, len(mentions))
	for dir := range mentions {
		mentioned = append(mentioned, dir)
	}
	sort.Strings(mentioned)
	for _, dir := range mentioned {
		if refs[dir] == 0 {
			return nil, 0, xerrors.Wrapf(errNoReference, "%s", dir)
		}
	}
	total := 0
	for _, n := range refs {
		total += n
	}
	return findings, total, nil
}

// mentionedDirs は、行が実在する ADR への道筋を含む置き場所を返します。from 自身は外します。
//
// リンクの書式に依らない経路であることが要点で、ここが markdownLink と同じ形を見ると突き合わせが
// 成立しません。道筋は、行のある文書からの相対と、リポジトリのルートからの両方で解決します。
// 文字列の包含で見ると、`usecases/x/docs/adr/0001-a.md` が root の `docs/adr/0001-a.md` に当たり、
// 指し先の scope に無い ADR が別の scope の言及として数えられます。
func mentionedDirs(line, from string, indexes map[string]map[int]string) map[string]bool {
	hit := make(map[string]bool)
	for _, token := range pathToken.FindAllString(line, -1) {
		// adrTarget が外部として退ける形を数えると、取り出せない参照があるように見えて番兵が立つ。
		if strings.HasPrefix(token, "/") {
			continue
		}
		for _, base := range []string{from, "."} {
			resolved := path.Clean(path.Join(base, token))
			dir := path.Dir(resolved)
			index, ok := indexes[dir]
			if !ok || dir == from {
				continue
			}
			m := fileName.FindStringSubmatch(path.Base(resolved))
			if m == nil {
				continue
			}
			if num, _ := strconv.Atoi(m[1]); index[num] == path.Base(resolved) {
				hit[dir] = true
			}
		}
	}
	return hit
}

// unknownScopeDir は、リンクの指し先が usecases/<use-case>/docs/adr/ の形をしているのに、どの scope にも
// 当たらないとき、その置き場所を返します。
//
// adrTarget は scope に無い置き場所を ADR ではないものとして退けるので、ここで拾わなければ綴り違いの
// ユースケースを指すリンクは何とも突き合わされないまま通ります。
func unknownScopeDir(target, fromDir string, dirs map[string]bool) (string, bool) {
	if externalLink.MatchString(target) {
		return "", false
	}
	clean, _, _ := strings.Cut(target, "#")
	dir := path.Dir(path.Clean(path.Join(fromDir, clean)))
	if dirs[dir] || !useCaseADRDir.MatchString(dir) {
		return "", false
	}
	return dir, true
}

// adrTarget は、リンクの指し先が dirs のいずれかの直下の ADR なら、その置き場所とファイル名を返します。
//
// 外部 URL を外すのは、番号体系がこのリポジトリのものではないからです。他リポジトリの ADR を
// 指すリンクは正しく、ここで実在を問えば必ず落ちます。
func adrTarget(target, fromDir string, dirs map[string]bool) (string, string, bool) {
	if externalLink.MatchString(target) {
		return "", "", false
	}
	clean, _, _ := strings.Cut(target, "#")
	resolved := path.Clean(path.Join(fromDir, clean))
	dir := path.Dir(resolved)
	if !dirs[dir] {
		return "", "", false
	}
	// 索引と雛形は ADR の置き場所に在るが ADR ではない。collect と同じ扱いにする。
	switch base := path.Base(resolved); base {
	case indexFile, templateFile:
		return "", "", false
	default:
		return dir, base, true
	}
}

// matchReference は、リンクの文言と指し先を ADR の実体に突き合わせます。
func matchReference(file string, line int, text, name string, index map[int]string) []lintreport.Finding {
	at := func(msg string) lintreport.Finding {
		return lintreport.Finding{File: file, Line: line, Message: msg}
	}

	m := fileName.FindStringSubmatch(name)
	if m == nil {
		return []lintreport.Finding{at(name + " は ADR のファイル名の形ではありません")}
	}
	number, _ := strconv.Atoi(m[1])

	actual, ok := index[number]
	if !ok {
		return []lintreport.Finding{at(fmt.Sprintf("参照先の ADR-%04d が存在しません: %s", number, name))}
	}
	if actual != name {
		return []lintreport.Finding{at(fmt.Sprintf("参照先が実在しません: %s（ADR-%04d は %s）", name, number, actual))}
	}

	// 文言が番号を名乗っているなら、指し先の番号と一致していなければならない。詰め直しの
	// 取りこぼしは、指し先ではなく**文言だけが古い**形で現れる。
	if t := linkTextNumber.FindStringSubmatch(text); t != nil {
		if stated, _ := strconv.Atoi(t[1]); stated != number {
			return []lintreport.Finding{at(fmt.Sprintf("リンクの文言 [%s] が指し先 %s と食い違っています", text, name))}
		}
	}
	return nil
}

// collectMarkdown は、repo 配下の Markdown を repo からの相対パスで集めます。adrDir 配下は
// 検査 6 が既に見ているため外します。
func collectMarkdown(repo, adrDir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(repo, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(repo, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if skipDir(rel, adrDir) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(rel, ".md") {
			files = append(files, rel)
		}
		return nil
	})
	if err != nil {
		return nil, xerrors.Wrapf(err, "%s の走査", repo)
	}
	sort.Strings(files)
	return files, nil
}

// skipDir は、走査に入らないディレクトリを判定します。
//
// .claude/worktrees は**別の作業のためのチェックアウト**であり、このリポジトリの記述ではありません。
// そこを読むと、無関係な作業の状態でこの検査が落ちます。
func skipDir(rel, adrDir string) bool {
	switch rel {
	case ".", "":
		return false
	case adrDir, ".git", "tmp", "node_modules", ".claude/worktrees":
		return true
	}
	return strings.HasSuffix(rel, "/node_modules")
}
