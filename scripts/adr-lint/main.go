// adr-lint は ADR の構造を検査します。
//
// **ADR-0001 が自らの検証方法として挙げた5項目を、そのまま実行するのがこのツールです。**
// 規範を定めた ADR が自分の検査手段を持たない状態は、ADR-0012 決定3（検証できない決定は
// 決定として不完全）に自ら反します。
//
//  1. ファイル名が NNNN-kebab-case-title.md に適合すること
//  2. 同一 scope 内で番号が重複しないこと
//  3. 先頭メタデータに Status / Date / Scope が存在すること
//  4. Status が `Superseded by` の場合、参照先 ADR が存在すること
//  5. root ADR 本文が modules/<use-case>/ 配下の path へ規範的に依存していないこと
//  6. 本文が参照する ADR-NNNN がすべて実在すること
//
// 6 は、番号が identity ではなく順序になったことから要る（ADR-0024 決定5-9）。
// 番号は削除に伴って詰められるため、**参照が黙って別の決定を指す**経路が開く。
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
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lintreport"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	defaultRoot  = "docs/adr"
	indexFile    = "README.md"
	templateFile = "template.md"
)

var (
	// ADR のファイル名。番号は4桁、続きは kebab-case。
	// 大文字とアンダースコアを弾くのは、同じ決定が2つの綴りで置かれるのを防ぐため。
	fileName = regexp.MustCompile(`^(\d{4})-([a-z0-9]+(?:-[a-z0-9]+)*)\.md$`)
	// 先頭メタデータ。値の形は問わず、存在だけを見る。
	statusLine = regexp.MustCompile(`(?m)^- Status:[ \t]*(\S.*)$`)
	dateLine   = regexp.MustCompile(`(?m)^- Date:[ \t]*(\S.*)$`)
	scopeLine  = regexp.MustCompile(`(?m)^- Scope:[ \t]*(\S.*)$`)
	// Superseded by <scope>/ADR-NNNN。scope を伴う形を正とする（ADR-0001 決定11）。
	supersededBy = regexp.MustCompile(`^Superseded by\s+([A-Za-z0-9_-]+)/ADR-(\d{4})\s*$`)
	// 索引の行。| [NNNN](ファイル名) | タイトル | Status |
	indexRow = regexp.MustCompile(`^\|\s*\[(\d{4})\]\(([^)]+)\)\s*\|`)
	// 本文中の ADR 参照。番号が動き得る以上、実在を機械で見るほかない。
	//
	// scope を伴う参照（`<scope>/ADR-NNNN`）は除く。他 scope の ADR はこのディレクトリに無く、
	// 見に行けば必ず落ちる検査になる。identity が `<scope>/<slug>` である以上（ADR-0024 決定4）、
	// scope を書いた参照はこちらの番号体系の外を指している。
	adrRef = regexp.MustCompile(`(^|[^/\w])ADR-(\d{4})`)
	// 本文が modules/<use-case>/ 配下の path を名指ししている箇所。
	// root ADR は特定ユースケースの実装詳細へ依存してはならない（ADR-0001 決定15）。
	useCasePath = regexp.MustCompile("`?modules/(?:<use-case>|[a-z0-9-]+)/[a-zA-Z0-9_./*-]+`?")
	// ただし <use-case> という**プレースホルダ**を含む形は、特定のユースケースを名指ししていない。
	placeholder = regexp.MustCompile(`modules/<use-case>/`)
)

// 検査対象を1件も持たないまま成功で終えないための番兵。
var errNoADR = xerrors.New("ADR が1件も見つかりません")

func main() {
	log.SetFlags(0)
	if err := run(os.Args[1:], os.Stdout); err != nil {
		if xerrors.Is(err, errNoADR) {
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
	findings = append(findings, checkMetadata(docs)...)
	findings = append(findings, checkSupersede(docs, *root)...)
	findings = append(findings, checkUseCaseDependency(docs)...)
	findings = append(findings, checkCrossReferences(docs)...)

	indexFindings, err := checkIndex(*root, docs)
	if err != nil {
		return err
	}
	findings = append(findings, indexFindings...)

	if len(findings) > 0 {
		sortFindings(findings)
		fmt.Fprintf(out, "❌ adr-lint: %d 件の不整合\n\n", len(findings))
		fmt.Fprintln(out, lintreport.Format(findings))
		return xerrors.Newf("%d 件の不整合", len(findings))
	}

	fmt.Fprintf(out, "✅ adr-lint: ADR %d 件の構造を確認しました\n", len(docs))
	return nil
}

// doc は検査対象の ADR 1件です。
type doc struct {
	path   string // root からの相対パス
	name   string // ファイル名
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
// 番号は採番後に再利用しません（ADR-0001 決定4）。欠番は許容します。
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
// 依存の向きは root ADR → use-case ADR の一方向に限ります（ADR-0001 決定13-15）。
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
// 番号は identity ではなく順序であり、削除に伴って詰められます（ADR-0024 決定5-6）。
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

func sortFindings(f []lintreport.Finding) {
	sort.SliceStable(f, func(i, j int) bool {
		if f[i].File != f[j].File {
			return f[i].File < f[j].File
		}
		return f[i].Line < f[j].Line
	})
}
