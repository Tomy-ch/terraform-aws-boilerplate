// oidc-job-lint は、AWS の role を OIDC で assume する job（job の permissions に `id-token: write` を持つ job）が、
// ADR-0602・ADR-0601・ADR-0604 の制約を満たすことを検査します。
//
// 解いている問題:
//
// role の信頼条件は environment の名前で job を限る（ADR-0602 決定14）。その限定が効くのは、job の側が
// environment を字面で宣言し、役割に合った role だけを取り、plan と apply の起動条件が分かれている
// 間だけである。どれが崩れても、AWS の側の検査はすべて緑のまま通る。
//
// どの workflow にも:
//
//   - workflow 全体の permissions に id-token を置かない —— すべての job が token を取れるようになる
//   - 長期のアクセスキーを参照しない（ADR-0602 決定12）
//
// 対象の job に:
//
//   - environment を字面で宣言し、その名前が environments.json に在る
//   - `make ci-aws-credentials CI_ENV=<env> CI_ROLE=<役割>` をちょうど1回呼び、`<env>-<役割>` が environment の名前と一致する
//   - plan の job は、workflow の起動が pull_request だけで、`needs` に infra-code-gate を持ち（ADR-0601 決定9）、
//     id が refuse-untrusted で if: を持たない step を資格情報の取得より前に置き（ADR-0602 決定14）、
//     terraform を `make ci-plan-*` でだけ走らせる（lock を取らない経路。ADR-0604 決定8）
//   - apply の job は、workflow の起動が push と入力の無い workflow_dispatch だけで、cancel-in-progress: false の
//     concurrency を持つ（ADR-0601 決定3-5）
//
// 対象の job が1つも無ければ、違反とは別の終了コード 2 で失敗します（ADR-0702 決定13）。
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lintreport"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workflow"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	defaultWorkflowsDir     = ".github/workflows"
	defaultEnvironmentsFile = ".github/settings/environments.json"
	refuseStepID            = "refuse-untrusted"
	detectionJob            = "infra-code-gate"
)

var (
	idToken       = regexp.MustCompile(`^["']?id-token["']?:[ \t]*["']?write\b`)
	permissionsAt = regexp.MustCompile(`^( *)permissions:[ \t]*(.*?)[ \t]*(?:#.*)?$`)
	longKey       = regexp.MustCompile(`(?i)aws_access_key_id|aws_secret_access_key|aws-access-key-id|aws-secret-access-key`)
	envScalar     = regexp.MustCompile(`^ {4}environment:[ \t]*(.*?)[ \t]*(?:#.*)?$`)
	envName       = regexp.MustCompile(`^ {6}name:[ \t]*(.*?)[ \t]*(?:#.*)?$`)
	credentials   = regexp.MustCompile(`\bmake\b.*\bci-aws-credentials\b`)
	ciEnv         = regexp.MustCompile(`\bCI_ENV=([a-z0-9]+)\b`)
	ciRole        = regexp.MustCompile(`\bCI_ROLE=(plan|apply|apply-identity)(?:\s|$)`)
	ciPlan        = regexp.MustCompile(`\bmake\b.*\bci-plan-[a-z0-9-]+`)
	// tf-job-lint と同じく、サブコマンドを列挙しない。terraform を入れる行（mise の tools）は起動ではないので含めない。
	runsTerraform = regexp.MustCompile(`\bmake\b.*\stf-|(?:^|[\s;&|(/"'])(?:terraform|tofu)["']?(?:\s|$)`)
	nameLine      = regexp.MustCompile(`^\s+(?:- )?name:`)
	installsTF    = regexp.MustCompile(`aqua:hashicorp/terraform\b`)
	onKey         = regexp.MustCompile(`^["']?on["']?:[ \t]*(.*?)[ \t]*(?:#.*)?$`)
	triggerKey    = regexp.MustCompile(`^ {2}([a-z_]+):`)
	inputsKey     = regexp.MustCompile(`^ {4}inputs:`)
	needsScalar   = regexp.MustCompile(`^ {4}needs:[ \t]*(.*?)[ \t]*(?:#.*)?$`)
	listItem      = regexp.MustCompile(`^ {6}- [ \t]*["']?([A-Za-z0-9_-]+)["']?[ \t]*(?:#.*)?$`)
	concurrencyAt = regexp.MustCompile(`^ {4}concurrency:[ \t]*(?:#.*)?$`)
	noCancel      = regexp.MustCompile(`^ {6}cancel-in-progress:[ \t]*false[ \t]*(?:#.*)?$`)
	stepID        = regexp.MustCompile(`^ {6}(?:- | {2})id:[ \t]*["']?([A-Za-z0-9_-]+)["']?[ \t]*(?:#.*)?$`)
	stepIf        = regexp.MustCompile(`^ {6}(?:- | {2})if:`)
	commentLine   = regexp.MustCompile(`^\s*#`)
)

var (
	errNoTarget   = xerrors.New("OIDC で role を assume する job が1つもありません")
	errViolations = xerrors.New("違反があります")
	errEnvDecl    = xerrors.New("environments.json を読めません")
)

func main() {
	log.SetFlags(0)
	if err := run(defaultWorkflowsDir, defaultEnvironmentsFile, os.Stdout); err != nil {
		if xerrors.Is(err, errNoTarget) {
			log.Printf("❌ oidc-job-lint: %v", err)
			os.Exit(2)
		}
		log.Fatalf("❌ oidc-job-lint: %v", err)
	}
}

func run(dir, envFile string, out io.Writer) error {
	envs, err := readEnvironments(envFile)
	if err != nil {
		return err
	}
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
		n, f := check(p, string(raw), envs)
		targets += n
		findings = append(findings, f...)
	}
	if len(findings) > 0 {
		fmt.Fprintf(out, "❌ oidc-job-lint: %d 件の違反\n\n", len(findings))
		fmt.Fprintln(out, lintreport.Format(findings))
		return xerrors.Wrapf(errViolations, "%d 件", len(findings))
	}
	if targets == 0 {
		return xerrors.Wrapf(errNoTarget, "%s", dir)
	}
	fmt.Fprintf(out, "✅ oidc-job-lint: OIDC で role を assume する job %d 件の environment・役割・起動条件を確認しました\n", targets)
	return nil
}

// readEnvironments は、宣言された environment の名前を返します。0件の宣言は、どの job の environment も
// 宣言に無いと判定させてしまうので、読めないのと同じに扱います。
func readEnvironments(path string) ([]string, error) {
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, xerrors.Wrapf(errEnvDecl, "%s: %v", path, err)
	}
	var decl []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &decl); err != nil {
		return nil, xerrors.Wrapf(errEnvDecl, "%s: %v", path, err)
	}
	names := make([]string, 0, len(decl))
	for _, d := range decl {
		if d.Name != "" {
			names = append(names, d.Name)
		}
	}
	if len(names) == 0 {
		return nil, xerrors.Wrapf(errEnvDecl, "%s: environment が1つもありません", path)
	}
	return names, nil
}

// check は1つの workflow を検査し、対象の job の数と違反を返します。
func check(file, source string, envs []string) (int, []lintreport.Finding) {
	var findings []lintreport.Finding
	for i, text := range strings.Split(source, "\n") {
		if !commentLine.MatchString(text) && longKey.MatchString(text) {
			findings = append(findings, lintreport.Finding{File: file, Line: i + 1,
				Message: "長期のアクセスキーを参照しています。OIDC で role を assume してください（ADR-0602 決定12）"})
		}
	}
	split := workflow.SplitJobs(source)
	if values, at, ok := permissionsBlock(split.Preamble, ""); ok && hasIDToken(values) {
		findings = append(findings, lintreport.Finding{File: file, Line: at,
			Message: "workflow 全体の permissions に id-token を置かないでください。token を要る job の permissions に置きます"})
	}
	on, onLine := triggers(split.Preamble)
	targets := 0
	for _, job := range split.Jobs {
		values, _, ok := permissionsBlock(job.Lines, "    ")
		if !ok || !hasIDToken(values) {
			continue
		}
		targets++
		findings = append(findings, checkJob(file, job, envs, on, onLine)...)
	}
	return targets, findings
}

func checkJob(file string, job workflow.Job, envs []string, on map[string][]workflow.Line, onLine int) []lintreport.Finding {
	finding := func(line int, format string, args ...any) lintreport.Finding {
		return lintreport.Finding{File: file, Line: line, Message: fmt.Sprintf("job %s: ", job.ID) + fmt.Sprintf(format, args...)}
	}
	var findings []lintreport.Finding

	env, envLine := environment(job)
	switch {
	case env == "":
		findings = append(findings, finding(job.Number, "environment を字面で宣言してください。role の信頼条件は environment の名前で job を限ります（ADR-0602 決定14）"))
	case !slices.Contains(envs, env):
		findings = append(findings, finding(envLine, "environment %s が environments.json にありません", env))
	}

	var credLines []workflow.Line
	for _, l := range job.Lines {
		if !commentLine.MatchString(l.Text) && credentials.MatchString(l.Text) {
			credLines = append(credLines, l)
		}
	}
	if len(credLines) != 1 {
		return append(findings, finding(job.Number, "make ci-aws-credentials を1回だけ呼んでください（%d 回）", len(credLines)))
	}
	cred := credLines[0]
	e, r := ciEnv.FindStringSubmatch(cred.Text), ciRole.FindStringSubmatch(cred.Text)
	if e == nil || r == nil {
		return append(findings, finding(cred.Number, "ci-aws-credentials に CI_ENV と CI_ROLE を明示してください"))
	}
	role := r[1]
	if env != "" && env != e[1]+"-"+role {
		findings = append(findings, finding(cred.Number, "environment %s と、取る role の %s-%s が一致しません", env, e[1], role))
	}

	if role == "plan" {
		return append(findings, checkPlan(job, cred, on, onLine, finding)...)
	}
	return append(findings, checkApply(job, on, onLine, finding)...)
}

func checkPlan(job workflow.Job, cred workflow.Line, on map[string][]workflow.Line, onLine int, finding func(int, string, ...any) lintreport.Finding) []lintreport.Finding {
	var findings []lintreport.Finding
	if keys := triggerNames(on); !slices.Equal(keys, []string{"pull_request"}) {
		findings = append(findings, finding(onLine, "plan の job を持つ workflow の起動は pull_request だけにしてください（%s）", strings.Join(keys, ", ")))
	}
	if !slices.Contains(needs(job), detectionJob) {
		findings = append(findings, finding(job.Number, "needs に %s を置いてください。plan は検出より先に Pull Request のコードを実行しません（ADR-0601 決定9）", detectionJob))
	}
	refused := false
	for _, step := range workflow.SplitSteps(job) {
		if !hasStepID(step, refuseStepID) {
			continue
		}
		refused = step.Number < cred.Number
		for _, l := range step.Lines {
			if stepIf.MatchString(l.Text) {
				findings = append(findings, finding(l.Number, "step %s に if: を置かないでください。拒否は skip ではなく失敗にします（ADR-0602 決定14）", refuseStepID))
			}
		}
	}
	if !refused {
		findings = append(findings, finding(cred.Number, "資格情報を取る前に、id が %s の step を置いてください（ADR-0602 決定14）", refuseStepID))
	}
	plans := 0
	for _, l := range job.Lines {
		if commentLine.MatchString(l.Text) || nameLine.MatchString(l.Text) || installsTF.MatchString(l.Text) {
			continue
		}
		if ciPlan.MatchString(l.Text) {
			plans++
		}
		if runsTerraform.MatchString(l.Text) {
			findings = append(findings, finding(l.Number, "plan の job は terraform を make ci-plan-* でだけ走らせてください（ADR-0604 決定8）"))
		}
	}
	if plans == 0 {
		findings = append(findings, finding(job.Number, "plan の job に make ci-plan-* がありません"))
	}
	return findings
}

func checkApply(job workflow.Job, on map[string][]workflow.Line, onLine int, finding func(int, string, ...any) lintreport.Finding) []lintreport.Finding {
	var findings []lintreport.Finding
	keys := triggerNames(on)
	if len(keys) == 0 || slices.ContainsFunc(keys, func(k string) bool { return k != "push" && k != "workflow_dispatch" }) {
		findings = append(findings, finding(onLine, "apply の job を持つ workflow の起動は push と workflow_dispatch だけにしてください（%s）", strings.Join(keys, ", ")))
	}
	for _, l := range on["workflow_dispatch"] {
		if inputsKey.MatchString(l.Text) {
			findings = append(findings, finding(l.Number, "apply の workflow_dispatch に inputs を置かないでください。ref や commit を入力に取らせません（ADR-0602 決定14）"))
		}
	}
	if !cancelsNothing(job) {
		findings = append(findings, finding(job.Number, "apply の job に cancel-in-progress: false の concurrency を置いてください"))
	}
	return findings
}

func hasIDToken(values []string) bool {
	return slices.ContainsFunc(values, idToken.MatchString)
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

// environment は、job の environment の名前と行を返します。式（`${{ }}`）は字面の名前ではないので空を返します。
func environment(job workflow.Job) (string, int) {
	for i, l := range job.Lines {
		m := envScalar.FindStringSubmatch(l.Text)
		if m == nil {
			continue
		}
		name, line := m[1], l.Number
		if name == "" {
			for _, next := range job.Lines[i+1:] {
				if !strings.HasPrefix(next.Text, "      ") {
					break
				}
				if n := envName.FindStringSubmatch(next.Text); n != nil {
					name, line = n[1], next.Number
				}
			}
		}
		name = strings.Trim(name, `"'`)
		if strings.Contains(name, "${{") {
			return "", line
		}
		return name, line
	}
	return "", job.Number
}

// triggers は、workflow の on: の起動条件ごとの本文と、on: の行を返します。
func triggers(preamble []workflow.Line) (map[string][]workflow.Line, int) {
	out := map[string][]workflow.Line{}
	for i, l := range preamble {
		m := onKey.FindStringSubmatch(l.Text)
		if m == nil {
			continue
		}
		if v := strings.Trim(m[1], "[] "); v != "" {
			for _, k := range strings.Split(v, ",") {
				out[strings.Trim(strings.TrimSpace(k), `"'`)] = nil
			}
			return out, l.Number
		}
		current := ""
		for _, next := range preamble[i+1:] {
			if strings.TrimSpace(next.Text) == "" || commentLine.MatchString(next.Text) {
				continue
			}
			if !strings.HasPrefix(next.Text, "  ") {
				break
			}
			if k := triggerKey.FindStringSubmatch(next.Text); k != nil {
				current = k[1]
				out[current] = nil
				continue
			}
			out[current] = append(out[current], next)
		}
		return out, l.Number
	}
	return out, 1
}

func triggerNames(on map[string][]workflow.Line) []string {
	keys := make([]string, 0, len(on))
	for k := range on {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// needs は、job の needs: の名前を返します。1行の値、フローの並び、ブロックの並びを読みます。
func needs(job workflow.Job) []string {
	for i, l := range job.Lines {
		m := needsScalar.FindStringSubmatch(l.Text)
		if m == nil {
			continue
		}
		if v := strings.Trim(m[1], "[] "); v != "" {
			items := strings.Split(v, ",")
			out := make([]string, 0, len(items))
			for _, n := range items {
				out = append(out, strings.Trim(strings.TrimSpace(n), `"'`))
			}
			return out
		}
		var out []string
		for _, next := range job.Lines[i+1:] {
			item := listItem.FindStringSubmatch(next.Text)
			if item == nil {
				break
			}
			out = append(out, item[1])
		}
		return out
	}
	return nil
}

func hasStepID(step workflow.Step, id string) bool {
	for _, l := range step.Lines {
		if m := stepID.FindStringSubmatch(l.Text); m != nil && m[1] == id {
			return true
		}
	}
	return false
}

// cancelsNothing は、job が cancel-in-progress: false を明示した concurrency を持つかを返します。
func cancelsNothing(job workflow.Job) bool {
	for i, l := range job.Lines {
		if !concurrencyAt.MatchString(l.Text) {
			continue
		}
		for _, next := range job.Lines[i+1:] {
			if !strings.HasPrefix(next.Text, "      ") {
				break
			}
			if noCancel.MatchString(next.Text) {
				return true
			}
		}
	}
	return false
}
