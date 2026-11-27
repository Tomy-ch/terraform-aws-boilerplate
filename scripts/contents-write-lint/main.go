// contents-write-lint は、リポジトリへ書き込める GITHUB_TOKEN（`contents: write`）を持つ job の形を検査します。
//
// 解いている問題:
//
// `contents: write` の token は、保護されていないブランチとタグへ push でき、Release も作れる。ruleset は
// 保護するブランチしか守らないので、その token が何に使われるかを決めるのは job の形だけである。形が崩れても
// actionlint も zizmor も何も言わない。この検査は、`contents: write` を持つ job を、apply の失敗の revert の
// ブランチを push する形（.github/workflows/README.md の「apply の失敗の revert」）だけに限る。job の
// permissions のほかの write（Pull Request へコメントする job の `pull-requests: write` など）は対象にしない。
//
// どの workflow にも:
//
//   - workflow 全体の permissions に write を置かない —— すべての job が書き込みの token を持つ
//   - `write-all` を置かない
//
// `contents: write` を持つ job に:
//
//   - permissions は `contents: write` だけにする
//   - environment:・container:・services:・defaults:・env:・continue-on-error:・uses:（再利用 workflow）を置かない
//   - `secrets.` を参照しない —— token は `github.token` で step の env に渡す
//   - workflow の起動を push だけにする —— Pull Request の head のコードに書き込みの token を渡さない
//   - needs: に、`make ci-revert-plan RUNNER_MODE=host` を走らせる job をちょうど1つ置く
//   - step の uses: は bullfrog と actions/checkout だけにし、checkout は persist-credentials: false にする
//   - run: は `make ci-revert-push RUNNER_MODE=host` をちょうど1回にする。道具を導入しない
//   - step の env: は pushEnvKeys に限り、step に if:・continue-on-error:・shell:・working-directory: を置かない
//   - YAML のアンカー・エイリアス・タグと、フロー記法の mapping（1行に詰めた step を含む）を置かない
//
// contents の権限は、値が read か none と読めなければ書き込みとして扱います。`contents: write` の行と run: の行は、
// job の permissions と step として読めた数とは別の経路で数え直し、合わなければ落とします。
// 対象の job が1つも無ければ、違反とは別の終了コード 2 で失敗します（ADR-0702 決定13）。
package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/harden"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lintreport"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workflow"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const defaultWorkflowsDir = ".github/workflows"

// pushEnvKeys は、書き込みの job の step が env: に置いてよい名前。make ci-revert-push が読む値に限る。
var pushEnvKeys = []string{"GH_TOKEN", "REVERT_HEAD", "REVERT_COMMITS", "REVERT_BRANCH"}

var (
	contentsKey = regexp.MustCompile(`(?:^|[\s{,])["']?contents["']?[ \t]*:(.*)$`)
	tokenDenied = regexp.MustCompile(`^[ \t]*["']?(?:read|none)["']?[ \t]*(?:[,}#].*)?$`)
	// yamlNode は、アンカー・エイリアス・タグとフロー記法の mapping。別の場所に書いた値や1行に詰めた step を
	// この job へ持ち込み、行ごとの読み方では追えない。
	yamlNode      = regexp.MustCompile(`(?:^[ \t]*-|:)[ \t]+[&*!]|<<[ \t]*:|(?:^[ \t]*-|:)[ \t]*\{`)
	anyRun        = regexp.MustCompile(`\brun["']?[ \t]*:`)
	writeAll      = regexp.MustCompile(`\bpermissions["']?[ \t]*:[ \t]*["']?write-all\b`)
	anyWrite      = regexp.MustCompile(`:[ \t]*["']?write\b`)
	permissionsAt = regexp.MustCompile(`^( *)permissions:[ \t]*(.*?)[ \t]*(?:#.*)?$`)
	jobKey        = regexp.MustCompile(`^ {4}["']?(environment|container|services|defaults|env|continue-on-error|uses)["']?[ \t]*:`)
	secretsRef    = regexp.MustCompile(`\bsecrets\.`)
	onScalar      = regexp.MustCompile(`^["']?on["']?:[ \t]*(.*?)[ \t]*(?:#.*)?$`)
	triggerKey    = regexp.MustCompile(`^ {2}["']?([a-z_]+)["']?[ \t]*:`)
	needsScalar   = regexp.MustCompile(`^ {4}needs:[ \t]*(.*?)[ \t]*(?:#.*)?$`)
	needsItem     = regexp.MustCompile(`^ {6}-[ \t]+(.*?)[ \t]*(?:#.*)?$`)
	stepUses      = regexp.MustCompile(`^ {6}(?:- | {2})["']?uses["']?[ \t]*:[ \t]*["']?([^"'\s@]+)@`)
	anyUses       = regexp.MustCompile(`\buses["']?[ \t]*:`)
	stepRun       = regexp.MustCompile(`^ {6}(?:- | {2})["']?run["']?[ \t]*:[ \t]*(.*?)[ \t]*$`)
	stepHides     = regexp.MustCompile(`^ {6}(?:- | {2})["']?(if|continue-on-error|shell|working-directory)["']?[ \t]*:`)
	stepEnv       = regexp.MustCompile(`^ {6}(?:- | {2})["']?env["']?[ \t]*:[ \t]*(.*?)[ \t]*$`)
	envKey        = regexp.MustCompile(`^ {10}["']?([^"':\s]+)["']?[ \t]*:`)
	noPersist     = regexp.MustCompile(`^ {10}persist-credentials:[ \t]*["']?false["']?[ \t]*(?:#.*)?$`)
	pushRun       = regexp.MustCompile(`^make ci-revert-push RUNNER_MODE=host$`)
	planRun       = regexp.MustCompile(`^ {6}(?:- | {2})["']?run["']?[ \t]*:[ \t]*make ci-revert-plan RUNNER_MODE=host[ \t]*$`)
	commentLine   = regexp.MustCompile(`^\s*#`)
)

var allowedUses = []string{"bullfrogsec/bullfrog", "actions/checkout"}

var (
	errNoTarget   = xerrors.New("contents: write を持つ job が1つもありません")
	errViolations = xerrors.New("違反があります")
)

func main() {
	log.SetFlags(0)
	if err := run(defaultWorkflowsDir, os.Stdout); err != nil {
		if xerrors.Is(err, errNoTarget) {
			log.Printf("❌ contents-write-lint: %v", err)
			os.Exit(2)
		}
		log.Fatalf("❌ contents-write-lint: %v", err)
	}
}

func run(dir string, out io.Writer) error {
	entries, err := os.ReadDir(dir)
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
		raw, err := os.ReadFile(filepath.Clean(p))
		if err != nil {
			return xerrors.Wrapf(err, "%s の読み取り", p)
		}
		n, f := check(p, string(raw))
		targets += n
		findings = append(findings, f...)
	}
	if len(findings) > 0 {
		fmt.Fprintf(out, "❌ contents-write-lint: %d 件の違反\n\n", len(findings))
		fmt.Fprintln(out, lintreport.Format(findings))
		return xerrors.Wrapf(errViolations, "%d 件", len(findings))
	}
	if targets == 0 {
		return xerrors.Wrapf(errNoTarget, "%s", dir)
	}
	fmt.Fprintf(out, "✅ contents-write-lint: contents: write を持つ job %d 件の形を確認しました\n", targets)
	return nil
}

// check は、1つの workflow の対象の job の数と違反を返します。
func check(file, source string) (int, []lintreport.Finding) {
	var findings []lintreport.Finding
	for i, text := range strings.Split(source, "\n") {
		if !commentLine.MatchString(text) && writeAll.MatchString(text) {
			findings = append(findings, lintreport.Finding{File: file, Line: i + 1, Message: "write-all を置かないでください。権限を1行ずつ書きます"})
		}
	}
	split := workflow.SplitJobs(source)
	if values, at, ok := permissions(split.Preamble, ""); ok {
		for _, v := range values {
			if anyWrite.MatchString(v) {
				findings = append(findings, lintreport.Finding{File: file, Line: at,
					Message: fmt.Sprintf("workflow 全体の permissions に write を置かないでください（%s）。要る job の permissions に置きます", v)})
			}
		}
	}

	targets, attributed := 0, 0
	for _, job := range split.Jobs {
		values, at, ok := permissions(job.Lines, "    ")
		if ok && len(values) == 1 && strings.HasPrefix(values[0], "{") {
			findings = append(findings, lintreport.Finding{File: file, Line: at,
				Message: fmt.Sprintf("job %s の permissions を読めません。1行の {} は使わず、権限を1行ずつ書いてください", job.ID)})
			continue
		}
		if !ok || !slices.ContainsFunc(values, isContentsWrite) {
			continue
		}
		targets++
		attributed++
		findings = append(findings, checkJob(file, job, values, split)...)
	}
	if n := countContentsWrite(split.Preamble, split.Jobs); n != attributed {
		findings = append(findings, lintreport.Finding{File: file, Line: 1,
			Message: fmt.Sprintf("contents: write の行 %d 件のうち、job の permissions として読めたのは %d 件です", n, attributed)})
	}
	return targets, findings
}

// isContentsWrite は、行が contents の権限を read と none 以外の値で与えるかを返します。
func isContentsWrite(text string) bool {
	m := contentsKey.FindStringSubmatch(text)
	return m != nil && !tokenDenied.MatchString(m[1])
}

// countContentsWrite は、job の切り出しとは別に、コメントでない contents: write の行を数えます。
func countContentsWrite(preamble []workflow.Line, jobs []workflow.Job) int {
	n := 0
	count := func(lines []workflow.Line) {
		for _, l := range lines {
			if !commentLine.MatchString(l.Text) && isContentsWrite(l.Text) {
				n++
			}
		}
	}
	count(preamble)
	for _, j := range jobs {
		count(j.Lines)
	}
	return n
}

func checkJob(file string, job workflow.Job, values []string, split workflow.Split) []lintreport.Finding {
	finding := func(line int, format string, args ...any) lintreport.Finding {
		return lintreport.Finding{File: file, Line: line, Message: fmt.Sprintf("job %s: ", job.ID) + fmt.Sprintf(format, args...)}
	}
	var findings []lintreport.Finding

	if len(values) != 1 {
		findings = append(findings, finding(job.Number, "permissions は contents: write だけにしてください（%s）", strings.Join(values, ", ")))
	}
	for _, l := range job.Lines {
		if commentLine.MatchString(l.Text) {
			continue
		}
		if m := jobKey.FindStringSubmatch(l.Text); m != nil {
			findings = append(findings, finding(l.Number, "%s: を置かないでください", m[1]))
		}
		if yamlNode.MatchString(l.Text) {
			findings = append(findings, finding(l.Number, "アンカー・エイリアス・タグとフロー記法の mapping を使わないでください"))
		}
		if secretsRef.MatchString(l.Text) {
			findings = append(findings, finding(l.Number, "secrets を参照しないでください。token は github.token で step の env に渡します"))
		}
	}
	if on, line := triggers(split.Preamble); len(on) != 1 || on[0] != "push" {
		findings = append(findings, finding(line, "workflow の起動を push だけにしてください（%s）", strings.Join(on, ", ")))
	}
	findings = append(findings, checkNeeds(job, split.Jobs, finding)...)
	findings = append(findings, checkSteps(job, finding)...)
	return findings
}

func checkNeeds(job workflow.Job, jobs []workflow.Job, finding func(int, string, ...any) lintreport.Finding) []lintreport.Finding {
	ids := needs(job)
	if len(ids) != 1 {
		return []lintreport.Finding{finding(job.Number, "needs: に make ci-revert-plan を走らせる job をちょうど1つ置いてください（%d 件）", len(ids))}
	}
	for _, j := range jobs {
		if j.ID != ids[0] {
			continue
		}
		for _, l := range j.Lines {
			if planRun.MatchString(l.Text) {
				return nil
			}
		}
	}
	return []lintreport.Finding{finding(job.Number, "needs: の %s は make ci-revert-plan RUNNER_MODE=host を走らせる job ではありません", ids[0])}
}

func checkSteps(job workflow.Job, finding func(int, string, ...any) lintreport.Finding) []lintreport.Finding {
	var findings []lintreport.Finding
	runs := 0
	// egress が生成した bullfrog の三つ組の continue-on-error:・if:・run: は見逃し、run: の数にも入れない
	// （harden.GeneratedLines。ADR-0702 決定20）。
	generated := harden.GeneratedLines(job)
	for _, step := range workflow.SplitSteps(job) {
		inEnv, checkout, persistOff := false, false, false
		for _, l := range step.Lines {
			if commentLine.MatchString(l.Text) {
				continue
			}
			if m := stepHides.FindStringSubmatch(l.Text); m != nil && !generated[l.Number] {
				findings = append(findings, finding(l.Number, "step に %s: を置かないでください", m[1]))
			}
			if anyUses.MatchString(l.Text) {
				m := stepUses.FindStringSubmatch(l.Text)
				switch {
				case m == nil || !slices.Contains(allowedUses, m[1]):
					findings = append(findings, finding(l.Number, "uses: は %s だけにしてください", strings.Join(allowedUses, " と ")))
				case m[1] == "actions/checkout":
					checkout = true
				}
			}
			if noPersist.MatchString(l.Text) {
				persistOff = true
			}
			if m := stepRun.FindStringSubmatch(l.Text); m != nil && !generated[l.Number] {
				runs++
				if !pushRun.MatchString(m[1]) {
					findings = append(findings, finding(l.Number, "run: は make ci-revert-push RUNNER_MODE=host だけにしてください"))
				}
			}
			if m := stepEnv.FindStringSubmatch(l.Text); m != nil {
				inEnv = true
				if m[1] != "" {
					findings = append(findings, finding(l.Number, "step の env: は1行に1つずつ書いてください"))
				}
				continue
			}
			if inEnv {
				if m := envKey.FindStringSubmatch(l.Text); m != nil {
					if !slices.Contains(pushEnvKeys, m[1]) {
						findings = append(findings, finding(l.Number, "step の env: に %s を置かないでください（%s だけ）", m[1], strings.Join(pushEnvKeys, "・")))
					}
					continue
				}
				inEnv = false
			}
		}
		if checkout && !persistOff {
			findings = append(findings, finding(step.Number, "checkout は persist-credentials: false にしてください"))
		}
	}
	if runs != 1 {
		findings = append(findings, finding(job.Number, "run: を持つ step をちょうど1つにしてください（%d 件）", runs))
	}
	// step として読めない run:（1行に詰めた step など）は、上の検査を受けないまま走る。
	mentions := 0
	for _, l := range job.Lines {
		if !commentLine.MatchString(l.Text) && !generated[l.Number] && anyRun.MatchString(l.Text) {
			mentions++
		}
	}
	if mentions != runs {
		findings = append(findings, finding(job.Number, "run: の行 %d 件のうち、step の run: として読めたのは %d 件です", mentions, runs))
	}
	return findings
}

// permissions は、indent の深さの permissions: の値を1行ずつ返します。
func permissions(lines []workflow.Line, indent string) ([]string, int, bool) {
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
			if commentLine.MatchString(next.Text) || strings.TrimSpace(next.Text) == "" {
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

// triggers は、workflow の起動の名前と、on: の行番号を返します。
func triggers(preamble []workflow.Line) ([]string, int) {
	for i, l := range preamble {
		m := onScalar.FindStringSubmatch(l.Text)
		if m == nil {
			continue
		}
		if m[1] != "" {
			return []string{strings.Trim(m[1], `"'`)}, l.Number
		}
		var names []string
		for _, next := range preamble[i+1:] {
			if commentLine.MatchString(next.Text) || strings.TrimSpace(next.Text) == "" {
				continue
			}
			if !strings.HasPrefix(next.Text, "  ") {
				break
			}
			if k := triggerKey.FindStringSubmatch(next.Text); k != nil {
				names = append(names, k[1])
			}
		}
		return names, l.Number
	}
	return nil, 1
}

func needs(job workflow.Job) []string {
	for i, l := range job.Lines {
		m := needsScalar.FindStringSubmatch(l.Text)
		if m == nil {
			continue
		}
		if v := m[1]; v != "" {
			v = strings.Trim(v, "[] ")
			var ids []string
			for _, id := range strings.Split(v, ",") {
				if id = strings.Trim(strings.TrimSpace(id), `"'`); id != "" {
					ids = append(ids, id)
				}
			}
			return ids
		}
		// job の名前として読めない項目も、項目として数える。読めない項目で止めると、件数が縮んで「ちょうど1つ」を通る。
		var ids []string
		for _, next := range job.Lines[i+1:] {
			if commentLine.MatchString(next.Text) || strings.TrimSpace(next.Text) == "" {
				continue
			}
			item := needsItem.FindStringSubmatch(next.Text)
			if item == nil {
				break
			}
			ids = append(ids, strings.Trim(strings.TrimSpace(item[1]), `"'`))
		}
		return ids
	}
	return nil
}
