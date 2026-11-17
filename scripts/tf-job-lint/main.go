// tf-job-lint は、AWS credentials を持たずに terraform を走らせる job が、ADR-0103 決定11 の制約を満たすことを
// 検査します。
//
// 解いている問題:
//
// コード実行の検出（ADR-0103）に依存しない job は、検出より先に、Pull Request が持ち込んだコードを実行しうる
// —— `external` data source は plan の中でプログラムを走らせ、provider は init が解決したものが走る。
// そのコードが届く範囲を、job の側で狭めておく。
//
// 決定11 の4制約のうち、ここが持つのは3つです。
//
//   - 権限はリポジトリの内容の読み取りに限る —— job（無ければ workflow）の permissions がちょうど
//     `contents: read` であるか、空であること
//   - checkout は資格情報を作業ツリーに残さない —— `actions/checkout` が `persist-credentials: false` を持つこと。
//     zizmor の artipacked もリポジトリ全体で同じことを見るが、その閾値の設定は code owner の対象の外にある
//   - 復元するキャッシュは固定値と突き合わせてから使う —— uses: を閉じた集合に限る。その集合のうち
//     キャッシュを持つ setup-mise の digest 照合は actions-mise-pin-lint が見る。キャッシュを復元する
//     action を足した日に、ここが落ちる
//
// outbound の許可リストは egress-check が持ちます。
package main

import (
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lintreport"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workflow"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const defaultWorkflowsDir = ".github/workflows"

var allowedUses = []string{
	"bullfrogsec/bullfrog@",
	"actions/checkout@",
	"./.github/actions/setup-mise",
}

var (
	// サブコマンドを列挙しない。列挙から漏れた呼び出しが、検査されないまま通るため。
	runsTerraform = regexp.MustCompile(`aqua:hashicorp/terraform\b|\bmake\b.*\stf-|(?:^|[\s;&|(/"'])(?:terraform|tofu)["']?(?:\s|$)`)
	idToken       = regexp.MustCompile(`^id-token:[ \t]*["']?write\b`)
	anyUses       = regexp.MustCompile(`^\s*(?:-\s*)?["']?uses["']?\s*:|^\s*-?\s*\{[^}]*\buses\s*:`)
	jobsKey       = regexp.MustCompile(`^jobs:[ \t]*(?:#.*)?$`)
	jobHeader     = regexp.MustCompile(`^ {2}["']?[A-Za-z0-9_-]+["']?:[ \t]*(?:#.*)?$`)
	withKey       = regexp.MustCompile(`^(\s+)with:[ \t]*(?:#.*)?$`)
	persistTrue   = regexp.MustCompile(`^\s+persist-credentials:[ \t]*["']?true\b`)
	checkoutUses  = regexp.MustCompile(`uses:[ \t]*["']?actions/checkout@`)
	noPersist     = regexp.MustCompile(`^\s+persist-credentials:[ \t]*["']?false["']?[ \t]*(?:#.*)?$`)
	permissionsAt = regexp.MustCompile(`^( *)permissions:[ \t]*(.*?)[ \t]*(?:#.*)?$`)
	usesLine      = regexp.MustCompile(`^\s+(?:- )?uses:[ \t]*["']?([^"'\s#]+)`)
	commentLine   = regexp.MustCompile(`^\s*#`)
)

// errNoTarget は、対象の job が1つも無いことを表します。「ゲートが外れた」と「合格した」を区別するため、
// 違反（1）とは別の終了コードで返します（ADR-0702 決定13）。
var (
	errNoTarget   = xerrors.New("terraform を走らせる job が1つもありません")
	errViolations = xerrors.New("違反があります")
)

func main() {
	log.SetFlags(0)
	dir := defaultWorkflowsDir
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	if err := run(dir, os.Stdout); err != nil {
		if xerrors.Is(err, errNoTarget) {
			log.Printf("❌ tf-job-lint: %v", err)
			os.Exit(2)
		}
		log.Fatalf("❌ tf-job-lint: %v", err)
	}
}

func run(dir string, out io.Writer) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return xerrors.Wrapf(err, "%s の読み取り", dir)
	}
	defer func() { _ = root.Close() }()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return xerrors.Wrapf(err, "%s の読み取り", dir)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}

	var findings []lintreport.Finding
	targets := 0
	for _, p := range workflow.SelectFiles(names, dir) {
		raw, err := root.ReadFile(filepath.Base(p))
		if err != nil {
			return xerrors.Wrapf(err, "%s の読み取り", p)
		}
		n, f := check(p, string(raw))
		targets += n
		findings = append(findings, f...)
	}
	if targets == 0 {
		return xerrors.Wrapf(errNoTarget, "%s", dir)
	}
	if len(findings) > 0 {
		fmt.Fprintf(out, "❌ tf-job-lint: %d 件の違反\n\n", len(findings))
		fmt.Fprintln(out, lintreport.Format(findings))
		return xerrors.Wrapf(errViolations, "%d 件", len(findings))
	}
	fmt.Fprintf(out, "✅ tf-job-lint: credentials を持たずに terraform を走らせる job %d 件の権限・checkout・uses: を確認しました\n", targets)
	return nil
}

// check は1つの workflow の対象の job を検査し、対象の数と違反を返します。
func check(file, source string) (int, []lintreport.Finding) {
	split := workflow.SplitJobs(source)
	targets := 0
	var findings []lintreport.Finding
	region := jobsRegion(source)
	// 切り出しとは別の経路で、terraform を走らせる行を数える。job に割り当てられなかった行は、どの検査も
	// 見ないまま通る —— jobs: や job の見出しを解釈できなかった workflow がそうなる。
	if missed := terraformLines(region) - terraformLines(jobBodies(split)); missed > 0 {
		findings = append(findings, lintreport.Finding{File: file, Line: 1,
			Message: fmt.Sprintf("terraform を走らせる行のうち %d 行が、どの job にも読み取れません。jobs: と job の見出しの書き方を確かめてください", missed)})
	}
	// フロースタイルで書いた job（`  name: {...}`）は見出しとして読めず、直前の job の本文に混ざる。
	for _, l := range region {
		if split.Found && strings.HasPrefix(l.Text, "  ") && !strings.HasPrefix(l.Text, "   ") && !commentLine.MatchString(l.Text) && !jobHeader.MatchString(l.Text) {
			findings = append(findings, lintreport.Finding{File: file, Line: l.Number,
				Message: "job の見出しを読み取れません。`  <job>:` の行の下へブロックで書いてください"})
		}
	}
	for _, job := range split.Jobs {
		if !isTarget(job) {
			continue
		}
		targets++
		if len(workflow.SplitSteps(job)) == 0 {
			findings = append(findings, lintreport.Finding{File: file, Line: job.Number, Message: fmt.Sprintf("job %s の steps を読み取れません", job.ID)})
			continue
		}
		findings = append(findings, checkPermissions(file, job, split.Preamble)...)
		findings = append(findings, checkUses(file, job)...)
		findings = append(findings, checkCheckout(file, job)...)
	}
	return targets, findings
}

// isTarget は、job が credentials を持たずに terraform を走らせるかを返します。id-token を持つ job は
// 検出への依存（ADR-0601 決定9）の側で扱うので、ここの対象にしません。
func isTarget(job workflow.Job) bool {
	if values, _, ok := permissionsBlock(job.Lines, "    "); ok {
		for _, v := range values {
			if idToken.MatchString(v) {
				return false
			}
		}
	}
	for _, l := range job.Lines {
		if !commentLine.MatchString(l.Text) && runsTerraform.MatchString(l.Text) {
			return true
		}
	}
	return false
}

// terraformLines は、コメントでない行のうち terraform を走らせる行を数えます。
func terraformLines(lines []workflow.Line) int {
	n := 0
	for _, l := range lines {
		if !commentLine.MatchString(l.Text) && runsTerraform.MatchString(l.Text) {
			n++
		}
	}
	return n
}

// jobsRegion は、jobs: の下の行を返します。jobs: が無ければ全行を返します —— その workflow の terraform は
// どの job にも割り当てられないので、すべて読み取れない行として数えるためです。
func jobsRegion(source string) []workflow.Line {
	lines := strings.Split(source, "\n")
	var out []workflow.Line
	in, found := false, false
	for i, text := range lines {
		switch {
		case jobsKey.MatchString(text):
			in, found = true, true
			continue
		case in && text != "" && !strings.HasPrefix(text, " ") && !strings.HasPrefix(text, "#"):
			in = false
		}
		if in {
			out = append(out, workflow.Line{Number: i + 1, Text: text})
		}
	}
	if !found {
		for i, text := range lines {
			out = append(out, workflow.Line{Number: i + 1, Text: text})
		}
	}
	return out
}

// jobBodies は、切り出した job の本文の行を返します。
func jobBodies(split workflow.Split) []workflow.Line {
	var out []workflow.Line
	for _, job := range split.Jobs {
		out = append(out, job.Lines...)
	}
	return out
}

// checkPermissions は、job の permissions が `contents: read` だけか空であることを検査します。job が
// permissions を持たなければ、workflow の permissions を見ます。
func checkPermissions(file string, job workflow.Job, preamble []workflow.Line) []lintreport.Finding {
	lines, at, ok := permissionsBlock(job.Lines, "    ")
	scope := "job " + job.ID
	if !ok {
		lines, at, ok = permissionsBlock(preamble, "")
		scope = "job " + job.ID + " が継ぐ workflow"
	}
	if !ok {
		return []lintreport.Finding{{File: file, Line: job.Number, Message: fmt.Sprintf("job %s に permissions がありません。既定の権限は書き込みを含みます", job.ID)}}
	}
	if len(lines) == 1 && (lines[0] == "contents: read" || lines[0] == "{}") {
		return nil
	}
	return []lintreport.Finding{{File: file, Line: at, Message: fmt.Sprintf("%s の permissions は `contents: read` だけにしてください（%s）", scope, strings.Join(lines, ", "))}}
}

// permissionsBlock は、指定した桁の permissions: と、その値（1行の値か、1段深い行の並び）を返します。
func permissionsBlock(lines []workflow.Line, indent string) ([]string, int, bool) {
	for i, l := range lines {
		m := permissionsAt.FindStringSubmatch(l.Text)
		if m == nil || m[1] != indent {
			continue
		}
		if m[2] != "" {
			return []string{m[2]}, l.Number, true
		}
		var values []string
		for _, next := range lines[i+1:] {
			if strings.TrimSpace(next.Text) == "" || commentLine.MatchString(next.Text) {
				continue
			}
			if !strings.HasPrefix(next.Text, indent+"  ") {
				break
			}
			values = append(values, strings.TrimSpace(next.Text))
		}
		return values, l.Number, true
	}
	return nil, 0, false
}

func checkUses(file string, job workflow.Job) []lintreport.Finding {
	var findings []lintreport.Finding
	for _, l := range job.Lines {
		if commentLine.MatchString(l.Text) {
			continue
		}
		m := usesLine.FindStringSubmatch(l.Text)
		if m == nil {
			// フロースタイルのステップ（`- { uses: ... }`）や引用符付きのキーは、ここの読み方では値を取り出せない。
			if anyUses.MatchString(l.Text) {
				findings = append(findings, lintreport.Finding{File: file, Line: l.Number,
					Message: fmt.Sprintf("job %s の uses: を読み取れません。`- uses: <action>` の形で書いてください", job.ID)})
			}
			continue
		}
		allowed := false
		for _, a := range allowedUses {
			if m[1] == a || (strings.HasSuffix(a, "@") && strings.HasPrefix(m[1], a)) {
				allowed = true
			}
		}
		if !allowed {
			findings = append(findings, lintreport.Finding{File: file, Line: l.Number,
				Message: fmt.Sprintf("job %s の %s は、credentials を持たずに terraform を走らせる job に置けない action です（ADR-0103 決定11）", job.ID, m[1])})
		}
	}
	return findings
}

// checkCheckout は、job の `actions/checkout` がすべて `persist-credentials: false` を持つことを検査します。
func checkCheckout(file string, job workflow.Job) []lintreport.Finding {
	var findings []lintreport.Finding
	for _, step := range workflow.SplitSteps(job) {
		uses, persists, persistsTrue := false, false, false
		withIndent := -1
		for _, l := range step.Lines {
			if commentLine.MatchString(l.Text) {
				continue
			}
			indent := len(l.Text) - len(strings.TrimLeft(l.Text, " "))
			if withIndent >= 0 && strings.TrimSpace(l.Text) != "" && indent <= withIndent {
				withIndent = -1
			}
			if m := withKey.FindStringSubmatch(l.Text); m != nil {
				withIndent = len(m[1])
				continue
			}
			uses = uses || checkoutUses.MatchString(l.Text)
			// with: の外の persist-credentials は action に渡らない。
			if withIndent >= 0 {
				persists = persists || noPersist.MatchString(l.Text)
				persistsTrue = persistsTrue || persistTrue.MatchString(l.Text)
			}
		}
		if uses && (!persists || persistsTrue) {
			findings = append(findings, lintreport.Finding{File: file, Line: step.Number,
				Message: fmt.Sprintf("job %s の actions/checkout に persist-credentials: false がありません（ADR-0103 決定11）", job.ID)})
		}
	}
	return findings
}
