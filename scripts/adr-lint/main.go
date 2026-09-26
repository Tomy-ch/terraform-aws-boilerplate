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
//  5. root ADR 本文が modules/<use-case>/ 配下の path へ規範的に依存していないこと
//  6. 本文が参照する ADR-NNNN がすべて実在すること
//  7. 見出しが名乗る番号が、ファイル名の番号と一致すること
//  8. docs/adr/ の**外**から ADR を指すリンクが、実在する ADR を指していること
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
// 「ADR の一覧が存在する唯一の場所」であり、そこがずれると読み手は決定へ到達できません。
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
)

var (
	// ADR のファイル名。番号は4桁、続きは kebab-case。
	// 大文字とアンダースコアを弾くのは、同じ決定が2つの綴りで置かれるのを防ぐため。
	fileName = regexp.MustCompile(`^(\d{4})-([a-z0-9]+(?:-[a-z0-9]+)*)\.md$`)
	// 先頭メタデータの行。checkMetadata が存在を見る。
	statusLine = regexp.MustCompile(`(?m)^- Status:[ \t]*(\S.*)$`)
	dateLine   = regexp.MustCompile(`(?m)^- Date:[ \t]*(\S.*)$`)
	scopeLine  = regexp.MustCompile(`(?m)^- Scope:[ \t]*(\S.*)$`)
	// Superseded by <scope>/ADR-NNNN。scope を伴う形を正とする（ADR-0001 決定3）。
	supersededBy = regexp.MustCompile(`^Superseded by\s+([A-Za-z0-9_-]+)/ADR-(\d{4})\s*$`)
	// 索引の行。| [NNNN](ファイル名) | タイトル | Status |
	indexRow = regexp.MustCompile(`^\|\s*\[(\d{4})\]\(([^)]+)\)\s*\|`)
	// 本文の H1。`# ADR-NNNN: <題>`
	headingLine = regexp.MustCompile(`(?m)^# ADR-(\d{4})\b`)
	// 本文中の ADR 参照。番号が動き得る以上、実在を機械で見るほかない。
	//
	// scope を伴う参照（`<scope>/ADR-NNNN`）は除く。他 scope の ADR はこのディレクトリに無く、
	// 見に行けば必ず落ちる検査になる。identity が `<scope>/<slug>` である以上（ADR-0001 決定4）、
	// scope を書いた参照はこちらの番号体系の外を指している。
	adrRef = regexp.MustCompile(`(^|[^/\w])ADR-(\d{4})`)
	// 本文が modules/<use-case>/ 配下の path を名指ししている箇所。
	// root ADR は特定ユースケースの実装詳細へ依存してはならない（ADR-0001 決定19）。
	useCasePath = regexp.MustCompile("`?modules/(?:<use-case>|[a-z0-9-]+)/[a-zA-Z0-9_./*-]+`?")
	// ただし <use-case> という**プレースホルダ**を含む形は、特定のユースケースを名指ししていない。
	placeholder = regexp.MustCompile(`modules/<use-case>/`)
	// Markdown のリンク。`[文言](指し先)` の形。指し先の後ろに title が付く形も許す。
	markdownLink = regexp.MustCompile(`\[([^\]]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
	// リンクの文言が番号を名乗っている形。`[0101]` と `[ADR-0101]` のどちらも使われている。
	linkTextNumber = regexp.MustCompile(`^(?:ADR-)?(\d{4})$`)
	// このリポジトリの作業ツリーの外を指すリンク。scheme 付きの URL と、サイトのルートからの
	// 絶対パスが該当する。**`/docs/adr/0001-x.md` は作業ツリーの docs/adr/ ではない** ——
	// 相対として解決すると同じ場所に見えるため、ここで分けないと外部の参照を自分の番号体系で裁く。
	externalLink = regexp.MustCompile(`^(?:[a-z][a-z0-9+.-]*:|/)`)
)

// 番兵（ADR-0702 決定13）。検査対象0件を成功として終えない。
var (
	errNoADR       = xerrors.New("ADR が1件も見つかりません")
	errNoMarkdown  = xerrors.New("ADR を指し得る Markdown が1件も見つかりません")
	errNoReference = xerrors.New("ADR を名指しした文書から、リンクを1件も取り出せません")
)

func main() {
	log.SetFlags(0)
	if err := run(os.Args[1:], os.Stdout); err != nil {
		if xerrors.Is(err, errNoADR) || xerrors.Is(err, errNoMarkdown) || xerrors.Is(err, errNoReference) {
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

	docs, findings, err := collect(*root)
	if err != nil {
		return err
	}
	// 「ADR が1件も無い」と「全てのファイル名が規約違反で検査対象から外れた」は別である。
	// 後者を番兵で返すと、直すべき違反が出力されないまま終わる。
	if len(docs) == 0 && len(findings) == 0 {
		return errNoADR
	}

	findings = append(findings, checkNumbers(docs, *root)...)
	findings = append(findings, checkHeading(docs)...)
	findings = append(findings, checkMetadata(docs)...)
	findings = append(findings, checkSupersede(docs, *root)...)
	findings = append(findings, checkUseCaseDependency(docs)...)
	findings = append(findings, checkCrossReferences(docs)...)

	indexFindings, err := checkIndex(*root, docs)
	if err != nil {
		return err
	}
	findings = append(findings, indexFindings...)

	pathFindings, refs, err := checkPathReferences(*repo, *root, docs)
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

	fmt.Fprintf(out, "✅ adr-lint: ADR %d 件の構造と、外からの参照 %d 件を確認しました\n", len(docs), refs)
	return nil
}

// doc は検査対象の ADR 1件です。
type doc struct {
	path   string // root からの相対パス
	name   string
	number int
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
		// 索引と雛形は ADR ではない。
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
		docs = append(docs, doc{path: path, name: name, number: num, source: string(raw)})
	}

	sort.Slice(docs, func(i, j int) bool { return docs[i].name < docs[j].name })
	return docs, findings, nil
}

// checkNumbers は同一 scope 内での番号の重複を検出します。
// 同一 scope 内で番号は重複させません。削除に伴う詰め直しは許容します（ADR-0001 決定5-7）。
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

// checkSupersede は Status が `Superseded by` の場合に、参照先 ADR の実在を検査します。
//
// 参照先が無いまま supersede を宣言すると、読み手は「置き換わった先」へ到達できません。
func checkSupersede(docs []doc, root string) []lintreport.Finding {
	exists := make(map[int]bool, len(docs))
	for _, d := range docs {
		exists[d.number] = true
	}

	var findings []lintreport.Finding
	for _, d := range docs {
		m := statusLine.FindStringSubmatch(d.source)
		if m == nil {
			continue // 不在は checkMetadata が報告済み
		}
		status := strings.TrimSpace(m[1])
		if !strings.HasPrefix(status, "Superseded by") {
			continue
		}

		sm := supersededBy.FindStringSubmatch(status)
		if sm == nil {
			findings = append(findings, lintreport.Finding{
				File: d.path, Line: 1,
				Message: fmt.Sprintf("Status の形式が `Superseded by <scope>/ADR-NNNN` ではありません（%s）", status),
			})
			continue
		}

		// 他 scope の ADR はこのディレクトリに無い。ここで実在を見るのは同一 scope のものだけとする。
		scope, numStr := sm[1], sm[2]
		if scope != "repository-wide" {
			continue
		}
		num, _ := strconv.Atoi(numStr)
		if !exists[num] {
			findings = append(findings, lintreport.Finding{
				File: d.path, Line: 1,
				Message: fmt.Sprintf("Superseded by の参照先 ADR-%s が %s に存在しません", numStr, root),
			})
		}
	}
	return findings
}

// checkUseCaseDependency は、root ADR 本文が特定ユースケースの実装詳細へ依存していないかを検査します。
//
// 依存の向きは root ADR → use-case ADR の一方向に限ります（ADR-0001 決定17-19）。
// `modules/<use-case>/` のようなプレースホルダは、特定のユースケースを名指ししていないため許容します。
func checkUseCaseDependency(docs []doc) []lintreport.Finding {
	var findings []lintreport.Finding
	for _, d := range docs {
		for i, line := range strings.Split(d.source, "\n") {
			for _, hit := range useCasePath.FindAllString(line, -1) {
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
		for i, line := range strings.Split(d.source, "\n") {
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

// checkIndex は索引が実ファイルと一致していることを検査します。
//
// 索引は「ADR の一覧が存在する唯一の場所」であり、そこがずれると読み手は決定へ到達できません。
func checkIndex(root string, docs []doc) ([]lintreport.Finding, error) {
	path := filepath.Join(root, indexFile)
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, xerrors.Wrapf(err, "%s の読み取り", path)
	}

	listed := make(map[int]string)
	for _, line := range strings.Split(string(raw), "\n") {
		if m := indexRow.FindStringSubmatch(line); m != nil {
			num, _ := strconv.Atoi(m[1])
			listed[num] = m[2]
		}
	}

	var findings []lintreport.Finding
	for _, d := range docs {
		target, ok := listed[d.number]
		if !ok {
			findings = append(findings, lintreport.Finding{
				File: path, Line: 1,
				Message: d.name + " が索引に載っていません",
			})
			continue
		}
		if target != d.name {
			findings = append(findings, lintreport.Finding{
				File: path, Line: 1,
				Message: fmt.Sprintf("索引の ADR-%04d が %s を指していますが、実ファイルは %s です", d.number, target, d.name),
			})
		}
		delete(listed, d.number)
	}

	numbers := make([]int, 0, len(listed))
	for n := range listed {
		numbers = append(numbers, n)
	}
	sort.Ints(numbers)
	for _, n := range numbers {
		findings = append(findings, lintreport.Finding{
			File: path, Line: 1,
			Message: fmt.Sprintf("索引の ADR-%04d（%s）に対応する実ファイルがありません", n, listed[n]),
		})
	}

	return findings, nil
}

// checkPathReferences は、docs/adr/ の**外**から ADR を指すリンクを検査し、違反と参照の件数を返します。
//
// 番号は identity ではなく順序であり、削除に伴って詰められます（ADR-0001 決定5-9）。詰めた瞬間、
// 外側のリンクは実在する**別の**決定を指します。指し先のファイル名が番号と slug の両方を持つため、
// 突き合わせれば必ず落ちます。
//
// 件数を返すのは、**0件のまま緑を返す経路を呼び出し側が塞げるようにする**ためです。走査の範囲が
// 壊れたとき、この検査は違反0件の合格として現れます。
func checkPathReferences(repo, root string, docs []doc) ([]lintreport.Finding, int, error) {
	index := make(map[int]string, len(docs))
	for _, d := range docs {
		index[d.number] = d.name
	}

	// root は「ファイルシステム上の位置」として渡ってくるが、リンクの指し先と突き合わせるには
	// repo からの相対で要る。両方を root 1つで兼ねると、repo とは別の場所を -root に渡したとき、
	// 突合が一度も成立しないまま緑になる。
	adrDir, err := filepath.Rel(repo, root)
	if err != nil {
		return nil, 0, xerrors.Wrapf(err, "%s から見た %s の位置", repo, root)
	}
	adrDir = filepath.ToSlash(adrDir)

	files, err := collectMarkdown(repo, adrDir)
	if err != nil {
		return nil, 0, err
	}
	// README.md すら見つからないなら、走査が対象を失っている。
	if len(files) == 0 {
		return nil, 0, errNoMarkdown
	}

	var findings []lintreport.Finding
	refs, mentions := 0, 0
	for _, file := range files {
		raw, err := os.ReadFile(filepath.Clean(filepath.Join(repo, filepath.FromSlash(file))))
		if err != nil {
			return nil, 0, xerrors.Wrapf(err, "%s の読み取り", file)
		}
		for _, line := range mdscan.Lines(raw) {
			if mentionsADR(line.Text, adrDir, index) {
				mentions++
			}
			for _, m := range markdownLink.FindAllStringSubmatch(line.Text, -1) {
				name, ok := adrTarget(m[2], path.Dir(file), adrDir)
				if !ok {
					continue
				}
				refs++
				findings = append(findings, matchReference(file, line.Number, m[1], name, index)...)
			}
		}
	}

	// **抽出の件数を、独立した経路で得た件数と突き合わせる**（ADR-0702 決定15）。実在する ADR の
	// ファイル名を含む行は在るのに、リンクを1件も取り出せないなら、壊れているのは解析の側である。
	// 突き合わせが無ければ、文書が解析の効かない書式（reference-style link など）へ寄った日に、
	// この検査は「外からの参照 0 件」で緑を返し続ける。
	//
	// 数えるのを「実在する ADR のファイル名」にしているのは、解析が**正しく退けた**もの
	// （外部 URL、索引、雛形）で食い違わせないためである。
	if mentions > 0 && refs == 0 {
		return nil, 0, errNoReference
	}
	return findings, refs, nil
}

// mentionsADR は、行が実在する ADR への道筋（`<adrDir>/<ファイル名>`）を含むかを返します。
//
// リンクの書式に依らない経路であることが要点で、ここが markdownLink と同じ形を見ると
// 突き合わせが成立しません。置き場所まで見るのは、同名のファイルが別のディレクトリに在る
// 場合に食い違わせないためです。
func mentionsADR(line, adrDir string, index map[int]string) bool {
	for _, name := range index {
		if strings.Contains(line, adrDir+"/"+name) {
			return true
		}
	}
	return false
}

// adrTarget は、リンクの指し先が root 配下の ADR なら、そのファイル名を返します。
//
// 外部 URL を外すのは、番号体系がこのリポジトリのものではないからです。他リポジトリの ADR を
// 指すリンクは正しく、ここで実在を問えば必ず落ちます。
func adrTarget(target, fromDir, root string) (string, bool) {
	if externalLink.MatchString(target) {
		return "", false
	}
	clean, _, _ := strings.Cut(target, "#")
	resolved := path.Clean(path.Join(fromDir, clean))
	if path.Dir(resolved) != root {
		return "", false
	}
	// 索引と雛形は root に在るが ADR ではない。検査 6 と同じ扱いにする。
	switch base := path.Base(resolved); base {
	case indexFile, templateFile:
		return "", false
	default:
		return base, true
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
