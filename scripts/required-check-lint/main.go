// required-check-lint は、保護設定が required とした context を報告する job が実在し、その workflow の
// 起動条件が check を取りこぼさない形であることを検査します。
//
// 解いている問題:
//
// pull_request トリガーに paths / branches のフィルタを置くと、除外された Pull Request では run が
// 1件も起動しません。GitHub は報告されなかった context を「該当しない」とは解釈せず、報告を待ち続けます。
// **その Pull Request は恒久的にマージ可能になりません。**
//
// 起動条件は on: から外し、job の if: で表現します。skip された job は skipped を報告し、
// required check はそれを成功として数えます。
//
// 検査の入力は保護設定の宣言そのものです。検査が独自の一覧を持つと、宣言を変えたときに片方だけが動きます。
package main

import (
	"encoding/json"
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
	defaultRulesetPath  = ".github/settings/branch-protection.json"
	defaultWorkflowsDir = ".github/workflows"
)

// 検査対象を1件も持たないまま成功で終えないための番兵。「ゲートが外れた」と「合格した」を
// 区別できなくしないため、違反（1）とは別の終了コードで返します。
var errNoRequiredContext = xerrors.New("保護設定に required status check がありません")

type ruleset struct {
	Rules []struct {
		Type       string `json:"type"`
		Parameters struct {
			RequiredStatusChecks []struct {
				Context string `json:"context"`
			} `json:"required_status_checks"`
		} `json:"parameters"`
	} `json:"rules"`
}

func main() {
	log.SetFlags(0)
	if err := run(os.Args[1:], os.Stdout); err != nil {
		if xerrors.Is(err, errNoRequiredContext) {
			log.Printf("❌ required-check-lint: %v", err)
			os.Exit(2)
		}
		log.Fatalf("❌ required-check-lint: %v", err)
	}
}

func run(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("required-check-lint", flag.ContinueOnError)
	fs.SetOutput(out)
	rulesetPath := fs.String("ruleset", defaultRulesetPath, "保護設定の宣言のパス")
	workflowsDir := fs.String("workflows", defaultWorkflowsDir, "workflow を探すディレクトリ")
	if err := fs.Parse(args); err != nil {
		return xerrors.Wrap(err, "引数の解釈")
	}

	contexts, err := readRequiredContexts(*rulesetPath)
	if err != nil {
		return err
	}
	if len(contexts) == 0 {
		return xerrors.Wrapf(errNoRequiredContext, "%s", *rulesetPath)
	}

	sources, err := readWorkflows(*workflowsDir)
	if err != nil {
		return err
	}

	findings := check(contexts, sources, *rulesetPath)
	if len(findings) > 0 {
		fmt.Fprintf(out, "❌ required-check-lint: %d 件の不整合\n\n", len(findings))
		fmt.Fprintln(out, lintreport.Format(findings))
		return xerrors.Newf("%d 件の不整合", len(findings))
	}

	fmt.Fprintf(out, "✅ required-check-lint: required context %d 件の報告 job と起動条件を確認しました\n", len(contexts))
	return nil
}

func readRequiredContexts(path string) ([]string, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // 検査対象のパスは呼び出し側が渡す固定値
	if err != nil {
		return nil, xerrors.Wrapf(err, "%s の読み取り", path)
	}

	var rs ruleset
	if err := json.Unmarshal(raw, &rs); err != nil {
		return nil, xerrors.Wrapf(err, "%s の解釈", path)
	}

	var contexts []string
	for _, rule := range rs.Rules {
		if rule.Type != "required_status_checks" {
			continue
		}
		for _, c := range rule.Parameters.RequiredStatusChecks {
			contexts = append(contexts, c.Context)
		}
	}
	return contexts, nil
}

// Source は検査対象の workflow 1件です。
type Source struct {
	File   string
	Source string
}

func readWorkflows(dir string) ([]Source, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, xerrors.Wrapf(err, "%s の読み取り", dir)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}

	var out []Source
	for _, p := range workflow.SelectFiles(names, dir) {
		raw, err := os.ReadFile(filepath.Clean(p))
		if err != nil {
			return nil, xerrors.Wrapf(err, "%s の読み取り", p)
		}
		out = append(out, Source{File: p, Source: string(raw)})
	}
	return out, nil
}

// check は required context を報告する job と、その workflow の起動条件を検査します。
func check(requiredContexts []string, workflows []Source, rulesetPath string) []lintreport.Finding {
	var findings []lintreport.Finding

	required := make(map[string]bool, len(requiredContexts))
	declared := make(map[string]int, len(requiredContexts))
	for _, c := range requiredContexts {
		required[c] = true
		declared[c] = 0
	}

	for _, wf := range workflows {
		split := workflow.SplitJobs(wf.Source)
		if !split.Found {
			findings = append(findings, lintreport.Finding{
				File: wf.File, Line: 1, Message: "jobs: が見つかりません",
			})
			continue
		}

		for _, job := range split.Jobs {
			if !required[job.ID] {
				continue
			}
			declared[job.ID]++
			findings = append(findings, triggerFindings(job.ID, wf)...)
		}
	}

	for _, c := range requiredContexts {
		if declared[c] == 1 {
			continue
		}
		findings = append(findings, lintreport.Finding{
			File: rulesetPath, Line: 1,
			Message: fmt.Sprintf("required context `%s` を報告する job は 1 件必要です（実際: %d）", c, declared[c]),
		})
	}

	return findings
}

// 記法の差で検出が外れると、フィルタが残ったまま検査が沈黙する。GitHub が受け付ける形は
// すべて読む必要がある。
var (
	// マッピング記法の on:。値を持たない行だけに当てる。
	onKey = regexp.MustCompile(`^on:[ \t]*(?:#.*)?$`)
	// 配列記法（`on: [pull_request, push]`）と単一値（`on: pull_request`）。
	// この形はフィルタを書けないため、検出した時点でフィルタ無しが確定する。
	onInline = regexp.MustCompile(`^on:[ \t]*(\[[^\]]*\]|[A-Za-z_][A-Za-z0-9_]*)[ \t]*(?:#.*)?$`)
	// 引用符の有無を問わない pull_request キー。
	pullRequestKey = regexp.MustCompile(`^ {2}(?:"pull_request"|'pull_request'|pull_request):[ \t]*(?:#.*)?$`)
	topLevelKey    = regexp.MustCompile(`^\S`)
	eventKey       = regexp.MustCompile(`^ {2}(?:"[A-Za-z_]+"|'[A-Za-z_]+'|[A-Za-z_]+):`)
	filterKey      = regexp.MustCompile(`^ {4}(?:"|')?(paths|paths-ignore|branches|branches-ignore)(?:"|')?:[ \t]*(?:#.*)?$`)
	// 配列記法の中に pull_request が含まれるか。
	inlinePullRequest = regexp.MustCompile(`\bpull_request\b`)
)

type filterHit struct {
	key    string
	number int
}

func triggerFindings(context string, wf Source) []lintreport.Finding {
	hits, ok := findPullRequestTrigger(wf.Source)
	if !ok {
		return []lintreport.Finding{{
			File: wf.File, Line: 1,
			Message: fmt.Sprintf("required context `%s` の workflow に pull_request トリガーがありません", context),
		}}
	}

	findings := make([]lintreport.Finding, 0, len(hits))
	for _, h := range hits {
		findings = append(findings, lintreport.Finding{
			File: wf.File, Line: h.number,
			Message: fmt.Sprintf(
				"required context `%s` の pull_request に `%s` が残っています"+
					"（起動しなかった check は未報告のままマージを止めるため、job の if: へ移してください）",
				context, h.key),
		})
	}
	return findings
}

func findPullRequestTrigger(source string) ([]filterHit, bool) {
	lines := strings.Split(source, "\n")

	onIndex := -1
	for i, ln := range lines {
		// 配列記法・単一値記法はフィルタを書けない。pull_request を含むならフィルタ無しで確定する。
		if m := onInline.FindStringSubmatch(ln); m != nil {
			return nil, inlinePullRequest.MatchString(m[1])
		}
		if onKey.MatchString(ln) {
			onIndex = i
			break
		}
	}
	if onIndex == -1 {
		return nil, false
	}

	start := -1
	for i := onIndex + 1; i < len(lines); i++ {
		if pullRequestKey.MatchString(lines[i]) {
			start = i
			break
		}
	}
	if start == -1 {
		return nil, false
	}

	var hits []filterHit
	for i := start + 1; i < len(lines); i++ {
		text := lines[i]
		if topLevelKey.MatchString(text) || eventKey.MatchString(text) {
			break
		}
		if m := filterKey.FindStringSubmatch(text); m != nil {
			hits = append(hits, filterHit{key: m[1], number: i + 1})
		}
	}

	return hits, true
}
