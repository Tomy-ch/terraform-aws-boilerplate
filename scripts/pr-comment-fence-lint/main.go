// pr-comment-fence-lint は、Pull Request のコメント本文が Markdown を壊さないことを検査します。
//
// 解いている問題は2つあります。
//
//  1. **固定長のフェンス。** コメントが利用者の書いたファイルを引用するとき、フェンス長を固定
//     （3バッククォート）にすると、本文に3連バッククォートが含まれた時点でブロックが閉じ、
//     以降が bot 名義の生 Markdown になります。長さは本文の最長バッククォート連から決めます。
//
//  2. **inline code span への補間。** 同型の問題が長さ1のフェンスでも起きます。**パスは NUL と /
//     以外なら何でも持てる**ため、バッククォートを含むパスが span を閉じます。
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lintreport"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workflow"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	commentAction       = "./.github/actions/upsert-pr-comment"
	defaultWorkflowsDir = ".github/workflows"
)

var (
	fixedFence     = regexp.MustCompile("(?:^|[\\s'\"])(`{3,})(?:[A-Za-z0-9_-]*)['\"]?\\s*$")
	commentUse     = workflow.UsesActionPattern(commentAction, false)
	detailsSummary = regexp.MustCompile(`^[ \t]*details-summary:[ \t]*(.*)$`)
	stepBullet     = regexp.MustCompile(`^\s*-\s`)
	// バッククォート1個で開いて1個で閉じる span の中に、シェル変数展開か printf の変換指定がある形。
	interpolatedSpan = regexp.MustCompile("`[^`\n]*(?:\\$\\{|\\$[A-Za-z_]|%[-0-9.*]*[sb])[^`\n]*`")
)

var errNothingChecked = xerrors.New("検査対象の workflow が1件も見つかりません")

func main() {
	log.SetFlags(0)
	if err := run(os.Args[1:], os.Stdout); err != nil {
		if xerrors.Is(err, errNothingChecked) {
			log.Printf("❌ pr-comment-fence-lint: %v", err)
			os.Exit(2)
		}
		log.Fatalf("❌ pr-comment-fence-lint: %v", err)
	}
}

func run(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("pr-comment-fence-lint", flag.ContinueOnError)
	fs.SetOutput(out)
	dir := fs.String("workflows", defaultWorkflowsDir, "workflow を探すディレクトリ")
	if err := fs.Parse(args); err != nil {
		return xerrors.Wrap(err, "引数の解釈")
	}

	entries, err := os.ReadDir(*dir)
	if err != nil {
		return xerrors.Wrapf(err, "%s の読み取り", *dir)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}

	files := workflow.SelectFiles(names, *dir)
	if len(files) == 0 {
		return errNothingChecked
	}

	var findings []lintreport.Finding
	for _, path := range files {
		raw, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return xerrors.Wrapf(err, "%s の読み取り", path)
		}
		findings = append(findings, scanWorkflow(path, strings.Split(string(raw), "\n"))...)
	}

	if len(findings) > 0 {
		fmt.Fprintf(out, "❌ pr-comment-fence-lint: %d 件の違反\n\n", len(findings))
		fmt.Fprintln(out, lintreport.Format(findings))
		return xerrors.Newf("%d 件の違反", len(findings))
	}

	fmt.Fprintf(out, "✅ pr-comment-fence-lint: workflow %d 件を確認しました\n", len(files))
	return nil
}

// findFixedFences は、固定長のフェンスを出力している echo 行を探します。
//
// 変数でフェンスを組む行（`echo "${fence}text"`）は対象外です。本文の最長バッククォート連から
// 長さを決める実装は、ここで弾く対象ではありません。
func findFixedFences(lines []string) []lintreport.Finding {
	var hits []lintreport.Finding
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "echo ") || strings.Contains(t, "${") {
			continue
		}
		if fixedFence.MatchString(t) {
			hits = append(hits, lintreport.Finding{Line: i + 1, Message: t})
		}
	}
	return hits
}

// fencesBody は、その行がアクションに本文をフェンスさせるかを返します。
//
// アクションがフェンスするのは details-summary が空でない値を持つときだけです。キーの有無で判定すると
// `details-summary: ”` が「フェンス済み」に化けて検査が黙ります。式は静的に空か判定できないので、
// フェンスされない側へ倒します。
func fencesBody(line string) bool {
	m := detailsSummary.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	v := strings.TrimSpace(m[1])
	if v == "" || v == "''" || v == `""` {
		return false
	}
	return !strings.Contains(v, "${{")
}

func indentOf(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}

// stepIndentOf は、at 行が属するステップを開く "-" の桁を返します。
//
// 見つからない場合は found が false になります。**0 へ倒しません。** 0 へ倒すと後続の行が
// すべて「より深い桁」となり、**別のステップの details-summary を自分のものと見なします。**
func stepIndentOf(lines []string, at int) (int, bool) {
	for b := at; b >= 0; b-- {
		if stepBullet.MatchString(lines[b]) {
			return indentOf(lines[b]), true
		}
	}
	return 0, false
}

func isFenced(lines []string, at int) bool {
	stepIndent, ok := stepIndentOf(lines, at)
	if !ok {
		// ステップの境界が取れない呼び出しは、フェンスされない側へ倒す。同ファイルの
		// fencesBody と同じ判断で、取りこぼしではなく過検出の側へ寄せる。
		return false
	}
	for _, line := range lines[at+1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := indentOf(line)
		// 次のステップを開く "-"（同じ桁）か、ステップより浅いキーに出たらこの呼び出しは終わり。
		if indent < stepIndent || (indent == stepIndent && stepBullet.MatchString(line)) {
			return false
		}
		if fencesBody(line) {
			return true
		}
	}
	return false
}

func hasPassThroughCall(lines []string) bool {
	for i, line := range lines {
		if commentUse.MatchString(line) && !isFenced(lines, i) {
			return true
		}
	}
	return false
}

func findInterpolatedSpans(lines []string) []lintreport.Finding {
	var hits []lintreport.Finding
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "#") {
			continue
		}
		if interpolatedSpan.MatchString(t) {
			hits = append(hits, lintreport.Finding{Line: i + 1, Message: t})
		}
	}
	return hits
}

func scanWorkflow(file string, lines []string) []lintreport.Finding {
	var findings []lintreport.Finding

	for _, hit := range findFixedFences(lines) {
		findings = append(findings, lintreport.Finding{
			File: file, Line: hit.Line,
			Message: "固定長のフェンスを出力しています。本文がこのフェンスを閉じられます: " + hit.Message,
		})
	}

	if hasPassThroughCall(lines) {
		for _, hit := range findInterpolatedSpans(lines) {
			findings = append(findings, lintreport.Finding{
				File: file, Line: hit.Line,
				Message: "本文素通しの呼び出しがある workflow で、inline code span へ値を補間しています。" +
					"値に含まれるバッククォート1個が span を閉じ、以降が生 Markdown になります: " + hit.Message,
			})
		}
	}

	return findings
}
