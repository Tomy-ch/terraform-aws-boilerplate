// pr-comment-secret-lint は、Pull Request へコメントする job に GITHUB_TOKEN 以外の secret を
// 渡していないことを検査します。
//
// 解いている問題:
//
// secret のマスキングは、runner が job の出力を log へ捕える経路しか覆いません。`tee` でファイルへ
// 書いたバイトはその経路を通らないため、**log ではマスクされて見える値が、public な Pull Request の
// コメントに生で載ります。**
//
// 追えるのはコンテキストの直接参照だけです。別 job の outputs 経由で渡す間接参照は静的に追えません。
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lintreport"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workflow"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	// commentAction は Pull Request へコメントするローカルアクションです。これを使う job が検査対象になります。
	commentAction = "./.github/actions/upsert-pr-comment"
	// allowedSecret はコメント投稿そのものに必要で、Actions が発行する短命のトークンです。唯一許可します。
	//
	//nolint:gosec // G101 の誤検知。**許可する secret の名前**であって、資格情報そのものではない。
	// この値を隠すと、何を許可しているかが読めなくなる。
	allowedSecret = "GITHUB_TOKEN"

	defaultWorkflowsDir = ".github/workflows"
)

var (
	commentActionUse = workflow.UsesActionPattern(commentAction, false)
	expression       = regexp.MustCompile(`\$\{\{([\s\S]*?)\}\}`)
	// secrets コンテキストへの参照。名前の有無はこの後ろを見て決めます。
	secretContext = regexp.MustCompile(`\bsecrets\b`)
	// secrets の直後に続く、参照する名前の書き方。GitHub は属性形と添字形の2通りを受け付けます。
	//
	// 名前を後ろから別に読むのは、1本の正規表現へ詰めるとどの枝が当たったかを捕獲組の番号で
	// 見分けることになり、書き方が増えるたびに条件が枝分かれするためです。どちらにも当たらない
	// 参照はコンテキスト全体の参照で、名前を持ちません。
	secretNameForms = []*regexp.Regexp{
		regexp.MustCompile(`^\s*\.\s*([A-Za-z0-9_-]+)`),
		regexp.MustCompile(`^\s*\[\s*["']([A-Za-z0-9_-]+)["']\s*\]`),
	}
)

// 検査対象を1件も持たないまま成功で終えないための番兵。
var errNoCommentingJob = xerrors.New("コメント投稿 job が1件も見つかりません")

func main() {
	log.SetFlags(0)
	if err := run(os.Args[1:], os.Stdout); err != nil {
		if xerrors.Is(err, errNoCommentingJob) {
			log.Printf("❌ pr-comment-secret-lint: %v", err)
			os.Exit(2)
		}
		log.Fatalf("❌ pr-comment-secret-lint: %v", err)
	}
}

func run(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("pr-comment-secret-lint", flag.ContinueOnError)
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

	var findings []lintreport.Finding
	commenting := 0

	for _, path := range workflow.SelectFiles(names, *dir) {
		raw, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return xerrors.Wrapf(err, "%s の読み取り", path)
		}
		scan := scanWorkflow(path, string(raw))
		if !scan.found {
			findings = append(findings, lintreport.Finding{
				File: path, Line: 1, Message: "jobs: が見つかりません",
			})
			continue
		}
		findings = append(findings, scan.findings...)
		commenting += scan.commentingJobs
	}

	if commenting == 0 {
		return errNoCommentingJob
	}

	if len(findings) > 0 {
		fmt.Fprintf(out, "❌ pr-comment-secret-lint: %d 件の違反\n\n", len(findings))
		fmt.Fprintln(out, lintreport.Format(findings))
		return xerrors.Newf("%d 件の違反", len(findings))
	}

	fmt.Fprintf(out, "✅ pr-comment-secret-lint: コメント投稿 job %d 件を確認しました\n", commenting)
	return nil
}

// secretRef は検出した secret の参照です。Name が空ならコンテキスト全体の参照です。
type secretRef struct {
	number int
	name   string
}

type secretScan struct {
	found          bool
	findings       []lintreport.Finding
	commentingJobs int
}

// secretName は secrets の直後の文字列から、参照している secret の名前を読みます。
func secretName(rest string) string {
	for _, form := range secretNameForms {
		if m := form.FindStringSubmatch(rest); m != nil {
			return m[1]
		}
	}
	return ""
}

// secretReferences は1行から、許可されない secret 参照を取り出します。
//
// ${{ }} 式の中だけを見ます。散文中の「secrets」に反応させないためです。
func secretReferences(line workflow.Line) []secretRef {
	var refs []secretRef

	for _, expr := range expression.FindAllStringSubmatch(line.Text, -1) {
		body := expr[1]
		for _, loc := range secretContext.FindAllStringIndex(body, -1) {
			name := secretName(body[loc[1]:])
			if name == allowedSecret {
				continue
			}
			refs = append(refs, secretRef{number: line.Number, name: name})
		}
	}

	return refs
}

// describeSecret は違反メッセージ内での secret の呼び方を返します。
func describeSecret(name string) string {
	if name == "" {
		return "`secrets` コンテキスト全体"
	}
	return "`secrets." + name + "`"
}

func usesCommentAction(job workflow.Job) bool {
	for _, ln := range job.Lines {
		if commentActionUse.MatchString(ln.Text) {
			return true
		}
	}
	return false
}

// scanWorkflow は workflow 1本を走査し、渡してはいけない secret 参照を違反として返します。
//
// job 本文と workflow 全体の env: を別々に見ます。前者はその job に閉じますが、後者はコメント投稿 job
// にも届くため、参照している場所が job の外でも違反になります。
func scanWorkflow(file, source string) secretScan {
	split := workflow.SplitJobs(source)
	if !split.Found {
		return secretScan{found: false}
	}

	var commenting []workflow.Job
	for _, job := range split.Jobs {
		if usesCommentAction(job) {
			commenting = append(commenting, job)
		}
	}

	var findings []lintreport.Finding
	for _, job := range commenting {
		for _, line := range job.Lines {
			for _, ref := range secretReferences(line) {
				findings = append(findings, lintreport.Finding{
					File: file, Line: ref.number,
					Message: fmt.Sprintf(
						"job `%s` は %s を使うため %s を渡せません"+
							"（マスキングは tee したファイルに効かず、生値が Pull Request のコメントに載ります）",
						job.ID, commentAction, describeSecret(ref.name)),
				})
			}
		}
	}

	if len(commenting) > 0 {
		for _, line := range split.Preamble {
			for _, ref := range secretReferences(line) {
				findings = append(findings, lintreport.Finding{
					File: file, Line: ref.number,
					Message: fmt.Sprintf(
						"workflow 全体に及ぶ %s は %s を使う job にも届きます"+
							"（マスキングは tee したファイルに効かず、生値が Pull Request のコメントに載ります）",
						describeSecret(ref.name), commentAction),
				})
			}
		}
	}

	return secretScan{found: true, findings: findings, commentingJobs: len(commenting)}
}
