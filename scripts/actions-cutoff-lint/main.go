// actions-cutoff-lint は、打ち切られた job でも Pull Request に結果が残ることを検査します。
//
// 3点を1本の走査にまとめています。どれか1つだけを直しても、Pull Request の読み手が何も得ないためです。
//
//  1. 全 job に timeout-minutes があること
//     無いと GitHub 既定の360分まで走り、1つのハングが runner を6時間押さえます。
//
//  2. コメント投稿ステップの if: が打ち切りに到達すること
//     **Actions は status 関数を含まない if: に暗黙の success() を前置します。** そのため打ち切られた
//     job はコメントステップを skip する一方、Fail ステップは赤くします。理由の読めない赤は、
//     どちらの半分より悪い状態です。
//
//  3. コメント投稿ステップの title: が打ち切り時の見出しを持つこと
//     tee で書きかけのファイルが残ると、コメント投稿は本文の有無では打ち切りを判別できません。
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
	// job 直下のキーだけを見ます。ステップの uses: を拾わないよう桁で絞ります。
	jobLevelUses    = regexp.MustCompile(`^ {4}uses:\s*\S`)
	jobLevelTimeout = regexp.MustCompile(`^ {4}timeout-minutes:\s*\S`)
	// ステップのキーは列8に並びます。先頭キーだけは "      - " に続けて同じ列から始まります。
	stepKeyIf     = regexp.MustCompile(`^(?: {6}- | {8})if:[ \t]*(.*)$`)
	stepKeyTitle  = regexp.MustCompile(`^ {10}title:[ \t]*(.*)$`)
	stepContinued = regexp.MustCompile(`^ {9,}\S`)
	commentUse    = workflow.UsesActionPattern(commentAction, true)
	// 合格条件から failure() を外しているのは、それが cancelled で false になるためです。
	// 「status 関数を持つか」で書くと、関数はあるのに打ち切り時は沈黙するステップを取り逃がします。
	reachesCancelled = regexp.MustCompile(`\b(?:always|cancelled)\s*\(\s*\)`)
	cutOffHeading    = regexp.MustCompile(`CUT OFF`)
)

// blockScalarHead は、値が次行以降にあることを示す if: の頭です。
var blockScalarHead = map[string]bool{"": true, ">": true, ">-": true, "|": true, "|-": true}

// 検査対象を1件も持たないまま成功で終えないための番兵。
var errNothingChecked = xerrors.New("検査対象の job が1件も見つかりません")

func main() {
	log.SetFlags(0)
	if err := run(os.Args[1:], os.Stdout); err != nil {
		if xerrors.Is(err, errNothingChecked) {
			log.Printf("❌ actions-cutoff-lint: %v", err)
			os.Exit(2)
		}
		log.Fatalf("❌ actions-cutoff-lint: %v", err)
	}
}

func run(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("actions-cutoff-lint", flag.ContinueOnError)
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
	jobs, steps := 0, 0

	for _, path := range workflow.SelectFiles(names, *dir) {
		raw, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return xerrors.Wrapf(err, "%s の読み取り", path)
		}
		scan := scanWorkflow(path, string(raw))
		if !scan.found {
			findings = append(findings, lintreport.Finding{File: path, Line: 1, Message: "jobs: が見つかりません"})
			continue
		}
		findings = append(findings, scan.findings...)
		jobs += scan.checkedJobs
		steps += scan.checkedSteps
	}

	if jobs == 0 {
		return errNothingChecked
	}

	if len(findings) > 0 {
		fmt.Fprintf(out, "❌ actions-cutoff-lint: %d 件の違反\n\n", len(findings))
		fmt.Fprintln(out, lintreport.Format(findings))
		return xerrors.Newf("%d 件の違反", len(findings))
	}

	fmt.Fprintf(out, "✅ actions-cutoff-lint: job %d 件・コメント投稿ステップ %d 件を確認しました\n", jobs, steps)
	return nil
}

type cutOffScan struct {
	found        bool
	findings     []lintreport.Finding
	checkedJobs  int
	checkedSteps int
}

type locatedValue struct {
	line  int
	value string
}

// callsReusableWorkflow は reusable workflow を呼ぶ job かを返します。
//
// 呼び出し job には timeout-minutes を書けない（invalid key）ため、その検査から外します。
func callsReusableWorkflow(job workflow.Job) bool {
	for _, ln := range job.Lines {
		if jobLevelUses.MatchString(ln.Text) {
			return true
		}
	}
	return false
}

func hasJobTimeout(job workflow.Job) bool {
	for _, ln := range job.Lines {
		if jobLevelTimeout.MatchString(ln.Text) {
			return true
		}
	}
	return false
}

func callsCommentAction(step workflow.Step) bool {
	for _, ln := range step.Lines {
		if commentUse.MatchString(ln.Text) {
			return true
		}
	}
	return false
}

// conditionOf はステップの if: を読みます。
//
// `if: >-` のような折り畳みスカラーでは値が次行以降にあります。ステップ内の後続の深い行を
// 続きとみなして1つの式へ連結します。
func conditionOf(step workflow.Step) (locatedValue, bool) {
	for i, ln := range step.Lines {
		m := stepKeyIf.FindStringSubmatch(ln.Text)
		if m == nil {
			continue
		}

		head := strings.TrimSpace(m[1])
		var parts []string
		if !blockScalarHead[head] {
			parts = append(parts, head)
		}
		for _, next := range step.Lines[i+1:] {
			if !stepContinued.MatchString(next.Text) {
				break
			}
			parts = append(parts, strings.TrimSpace(next.Text))
		}

		return locatedValue{line: ln.Number, value: strings.Join(parts, " ")}, true
	}
	return locatedValue{}, false
}

func titleOf(step workflow.Step) (locatedValue, bool) {
	for _, ln := range step.Lines {
		if m := stepKeyTitle.FindStringSubmatch(ln.Text); m != nil {
			return locatedValue{line: ln.Number, value: strings.TrimSpace(m[1])}, true
		}
	}
	return locatedValue{}, false
}

func scanWorkflow(file, source string) cutOffScan {
	split := workflow.SplitJobs(source)
	if !split.Found {
		return cutOffScan{found: false}
	}

	var findings []lintreport.Finding
	jobs, steps := 0, 0

	for _, job := range split.Jobs {
		if !callsReusableWorkflow(job) {
			jobs++
			if !hasJobTimeout(job) {
				findings = append(findings, lintreport.Finding{
					File: file, Line: job.Number,
					Message: fmt.Sprintf("job `%s` に timeout-minutes がありません（GitHub 既定の360分まで走ります）", job.ID),
				})
			}
		}

		for _, step := range workflow.SplitSteps(job) {
			if !callsCommentAction(step) {
				continue
			}
			steps++
			findings = append(findings, checkCommentStep(file, job.ID, step)...)
		}
	}

	return cutOffScan{found: true, findings: findings, checkedJobs: jobs, checkedSteps: steps}
}

func checkCommentStep(file, jobID string, step workflow.Step) []lintreport.Finding {
	var findings []lintreport.Finding

	cond, ok := conditionOf(step)
	switch {
	case !ok:
		findings = append(findings, lintreport.Finding{
			File: file, Line: step.Number,
			Message: fmt.Sprintf("job `%s` の %s ステップに if: がありません（暗黙の success() で打ち切り時に skip されます）", jobID, commentAction),
		})
	case !reachesCancelled.MatchString(cond.value):
		findings = append(findings, lintreport.Finding{
			File: file, Line: cond.line,
			Message: fmt.Sprintf("job `%s` の %s ステップの if: が打ち切りに到達しません（always() / cancelled() が要ります。failure() は cancelled では false です）", jobID, commentAction),
		})
	}

	title, ok := titleOf(step)
	switch {
	case !ok:
		findings = append(findings, lintreport.Finding{
			File: file, Line: step.Number,
			Message: fmt.Sprintf("job `%s` の %s ステップに title: がありません（本文だけ書きかけで残った打ち切りを見出しで区別できません）", jobID, commentAction),
		})
	case !cutOffHeading.MatchString(title.value):
		findings = append(findings, lintreport.Finding{
			File: file, Line: title.line,
			Message: fmt.Sprintf("job `%s` の %s ステップの title: に打ち切り時の見出しがありません（`${{ steps.X.outputs.title || '## ⚠️ …: CUT OFF (no result produced)' }}` の形にしてください）", jobID, commentAction),
		})
	}

	return findings
}
