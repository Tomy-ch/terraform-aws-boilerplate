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
//   - `needs` に、同じ workflow の中で `make infra-code-gate` を走らせる job を持ち、job の if: を持つなら
//     その job の成功を条件に含め、status の関数・論理和・否定を置かない（ADR-0601 決定9）。検出の job は
//     needs: と if: と continue-on-error: を持たず、検出の step は終了コードを隠さない
//   - plan の job は、workflow の起動が pull_request だけで、id が refuse-untrusted で if: を持たない step を
//     資格情報の取得より前に置き（ADR-0602 決定14）、terraform を `make ci-plan-*` でだけ走らせる
//     （lock を取らない経路。ADR-0604 決定8）
//   - apply の job は、workflow の起動が push と入力の無い workflow_dispatch だけで（ADR-0602 決定14）、
//     push に paths を持たず、cancel-in-progress: false の concurrency を（workflow 全体に在ればそれが、
//     無ければ job が）持ち —— 後から起動した run が、実行中の apply を途中で止めない ——、id が refuse-stale で if: を持たない step を資格情報の取得より前に置き
//     （古い commit の再実行を拒む）、terraform を `make ci-apply-*` でだけ走らせる（Policy を通った plan だけを
//     apply する経路。ADR-0604 決定11）
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
	refuseUntrusted         = "refuse-untrusted"
	refuseStale             = "refuse-stale"
)

var (
	idToken = regexp.MustCompile(`^["']?id-token["']?:[ \t]*["']?write\b`)
	// idTokenKey と writeAll は、permissionsBlock とは別の経路で token を得うる行を数える。値が次の行に続く形も
	// 拾うために、値が read か none と読めない id-token の行はすべて数える。
	idTokenKey    = regexp.MustCompile(`(?:^|[\s{,])["']?id-token["']?[ \t]*:(.*)$`)
	tokenDenied   = regexp.MustCompile(`^[ \t]*["']?(?:read|none)["']?[ \t]*(?:[,}#].*)?$`)
	writeAll      = regexp.MustCompile(`\bpermissions:[ \t]*["']?write-all\b`)
	permissionsAt = regexp.MustCompile(`^( *)permissions:[ \t]*(.*?)[ \t]*(?:#.*)?$`)
	longKey       = regexp.MustCompile(`(?i)aws_access_key_id|aws_secret_access_key|aws-access-key-id|aws-secret-access-key`)
	envScalar     = regexp.MustCompile(`^ {4}environment:[ \t]*(.*?)[ \t]*(?:#.*)?$`)
	envName       = regexp.MustCompile(`^ {6}name:[ \t]*(.*?)[ \t]*(?:#.*)?$`)
	credentials   = regexp.MustCompile(`\bmake\b.*\bci-aws-credentials\b`)
	ciEnv         = regexp.MustCompile(`\bCI_ENV=([a-z0-9]+)\b`)
	ciRole        = regexp.MustCompile(`\bCI_ROLE=(plan|apply|apply-identity)(?:\s|$)`)
	// CI の入口と検出は、run: の値が make と変数の代入とターゲット1つだけの形に限る。make のオプション
	// （-n・-i・-C・-f 等）や後続のコマンドは、実行しないか終了コードを隠しうる。
	ciPlan    = makeRun(`ci-plan-[a-z0-9-]+`)
	ciApply   = makeRun(`ci-apply-[a-z0-9-]+`)
	ciEntry   = regexp.MustCompile(`\bmake\b.*\bci-(?:plan|apply)-[a-z0-9-]+`)
	detection = makeRun(`infra-code-gate`)
	// status の関数・論理和・否定は、検出の失敗を越えて job を走らせうる。!= は比較なので除く。
	statusEscape = regexp.MustCompile(`\b(?:always|cancelled|failure)\(\)|\|\||!(?:[^=]|$)`)
	pushPaths    = regexp.MustCompile(`^\s+["']?paths(?:-ignore)?["']?[ \t]*:`)
	// tf-job-lint と同じく、サブコマンドを列挙しない。terraform を入れる行（mise の tools）は起動ではないので含めない。
	// local.mk の init-* / plan-* も terraform を走らせる。それ以外の名前の make のターゲットは字面からは選べない。
	runsTerraform = regexp.MustCompile(`\bmake\b.*\s(?:tf|init|plan)-|(?:^|[\s;&|(/"'])(?:terraform|tofu)["']?(?:\s|$)`)
	nameLine      = regexp.MustCompile(`^\s+(?:- )?name:`)
	installsTF    = regexp.MustCompile(`aqua:hashicorp/terraform\b`)
	onKey         = regexp.MustCompile(`^["']?on["']?:[ \t]*(.*?)[ \t]*(?:#.*)?$`)
	triggerKey    = regexp.MustCompile(`^ {2}([a-z_]+):[ \t]*([^#]*?)[ \t]*(?:#.*)?$`)
	inputsKey     = regexp.MustCompile(`^\s+["']?inputs["']?[ \t]*:`)
	needsScalar   = regexp.MustCompile(`^ {4}needs:[ \t]*(.*?)[ \t]*(?:#.*)?$`)
	jobKeyAt      = regexp.MustCompile(`^ {4}["']?([a-z-]+)["']?[ \t]*:[ \t]*(.*?)[ \t]*$`)
	skipsOrHides  = regexp.MustCompile(`^\s+(?:- )?["']?(?:if|continue-on-error|working-directory|defaults)["']?[ \t]*:`)
	listItem      = regexp.MustCompile(`^ {6}- [ \t]*["']?([A-Za-z0-9_-]+)["']?[ \t]*(?:#.*)?$`)
	concurrencyAt = regexp.MustCompile(`^( *)["']?concurrency["']?[ \t]*:[ \t]*(?:#.*)?$`)
	concurrencyKy = regexp.MustCompile(`^( *)["']?concurrency["']?[ \t]*:`)
	noCancel      = regexp.MustCompile(`^( *)["']?cancel-in-progress["']?[ \t]*:[ \t]*false[ \t]*(?:#.*)?$`)
	stepID        = regexp.MustCompile(`^ {6}(?:- | {2})id:[ \t]*["']?([A-Za-z0-9_-]+)["']?[ \t]*(?:#.*)?$`)
	stepIf        = regexp.MustCompile(`^ {6}(?:- | {2})["']?if["']?[ \t]*:`)
	stepContinue  = regexp.MustCompile(`^ {6}(?:- | {2})["']?continue-on-error["']?[ \t]*:`)
	stepUses      = regexp.MustCompile(`^ {6}(?:- | {2})uses:`)
	anyUses       = regexp.MustCompile(`^\s+(?:-\s*)?["']?uses["']?\s*:|^\s+-?\s*\{[^}]*\buses\s*:`)
	checkoutUses  = regexp.MustCompile(`^ {6}(?:- | {2})uses:[ \t]*["']?actions/checkout@`)
	noPersist     = regexp.MustCompile(`^ {10}persist-credentials:[ \t]*["']?false["']?[ \t]*(?:#.*)?$`)
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
	// attributed は、token を得うる行のうち、下で job か workflow 全体の permissions として読めた行の数。
	attributed := 0
	if values, at, ok := permissionsBlock(split.Preamble, ""); ok {
		switch {
		case unreadable(values):
			attributed++
			findings = append(findings, lintreport.Finding{File: file, Line: at,
				Message: "workflow 全体の permissions を読めません。write-all と1行の {} は使わず、権限を1行ずつ書いてください"})
		case hasIDToken(values):
			attributed++
			findings = append(findings, lintreport.Finding{File: file, Line: at,
				Message: "workflow 全体の permissions に id-token を置かないでください。token を要る job の permissions に置きます"})
		}
	}
	on, onLine := triggers(split.Preamble)
	w := workflowInfo{on: on, onLine: onLine, preamble: split.Preamble, jobs: split.Jobs}
	targets := 0
	for _, job := range split.Jobs {
		values, at, ok := permissionsBlock(job.Lines, "    ")
		if ok && unreadable(values) {
			attributed++
			findings = append(findings, lintreport.Finding{File: file, Line: at,
				Message: fmt.Sprintf("job %s の permissions を読めません。write-all と1行の {} は使わず、権限を1行ずつ書いてください", job.ID)})
			continue
		}
		if !ok || !hasIDToken(values) {
			continue
		}
		attributed++
		targets++
		findings = append(findings, checkJob(file, job, values, envs, w)...)
	}
	if n := countTokenLines(source); n > attributed {
		findings = append(findings, lintreport.Finding{File: file, Line: 1,
			Message: fmt.Sprintf("id-token: write か write-all を持つ行 %d 件のうち %d 件を、job の permissions として読めません", n, n-attributed)})
	}
	return targets, findings
}

// countTokenLines は、job の切り出しとは別に、token を得うる行をファイル全体で数えます。
func countTokenLines(source string) int {
	n := 0
	for _, text := range strings.Split(source, "\n") {
		if commentLine.MatchString(text) {
			continue
		}
		if m := idTokenKey.FindStringSubmatch(text); (m != nil && !tokenDenied.MatchString(m[1])) || writeAll.MatchString(text) {
			n++
		}
	}
	return n
}

// unreadable は、permissions の値が write-all か1行の {} の並びで、ここの読み方では権限を取り出せないかを返します。
func unreadable(values []string) bool {
	return len(values) == 1 && (strings.Trim(values[0], `"'`) == "write-all" || (strings.HasPrefix(values[0], "{") && values[0] != "{}"))
}

// workflowInfo は、job の検査が読む workflow 全体の部分です。
type workflowInfo struct {
	on       map[string][]workflow.Line
	onLine   int
	preamble []workflow.Line
	jobs     []workflow.Job
}

func checkJob(file string, job workflow.Job, permissions, envs []string, w workflowInfo) []lintreport.Finding {
	finding := func(line int, format string, args ...any) lintreport.Finding {
		return lintreport.Finding{File: file, Line: line, Message: fmt.Sprintf("job %s: ", job.ID) + fmt.Sprintf(format, args...)}
	}
	var findings []lintreport.Finding

	for _, v := range permissions {
		if v != "contents: read" && !idToken.MatchString(v) {
			findings = append(findings, finding(job.Number, "permissions は contents: read と id-token: write だけにしてください（%s）", v))
		}
	}

	env, envLine := environment(job)
	switch {
	case env == "":
		findings = append(findings, finding(job.Number, "environment を字面で宣言してください。role の信頼条件は environment の名前で job を限ります（ADR-0602 決定14）"))
	case !slices.Contains(envs, env):
		findings = append(findings, finding(envLine, "environment %s が environments.json にありません", env))
	}

	var credLines []workflow.Line
	for _, l := range job.Lines {
		if !commentLine.MatchString(l.Text) && !nameLine.MatchString(l.Text) && credentials.MatchString(l.Text) {
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
	findings = append(findings, checkSteps(job, cred, finding)...)
	findings = append(findings, checkDetection(job, w.jobs, finding)...)

	if role == "plan" {
		return append(findings, checkPlan(job, cred, w, finding)...)
	}
	return append(findings, checkApply(job, cred, w, finding)...)
}

func checkPlan(job workflow.Job, cred workflow.Line, w workflowInfo, finding func(int, string, ...any) lintreport.Finding) []lintreport.Finding {
	var findings []lintreport.Finding
	if keys := triggerNames(w.on); !slices.Equal(keys, []string{"pull_request"}) {
		findings = append(findings, finding(w.onLine, "plan の job を持つ workflow の起動は pull_request だけにしてください（%s）", strings.Join(keys, ", ")))
	}
	findings = append(findings, checkRefusal(job, cred, refuseUntrusted, finding)...)
	return append(findings, checkTerraform(job, ciPlan, "plan", finding)...)
}

func checkApply(job workflow.Job, cred workflow.Line, w workflowInfo, finding func(int, string, ...any) lintreport.Finding) []lintreport.Finding {
	var findings []lintreport.Finding
	keys := triggerNames(w.on)
	if len(keys) == 0 || slices.ContainsFunc(keys, func(k string) bool { return k != "push" && k != "workflow_dispatch" }) {
		findings = append(findings, finding(w.onLine, "apply の job を持つ workflow の起動は push と workflow_dispatch だけにしてください（%s）", strings.Join(keys, ", ")))
	}
	for _, k := range []string{"push", "workflow_dispatch"} {
		for _, l := range w.on[k] {
			if strings.Contains(l.Text, "{") {
				findings = append(findings, finding(l.Number, "apply の workflow の起動条件をフロー形式で書かないでください。paths と inputs を読めません"))
			}
		}
	}
	for _, l := range w.on["workflow_dispatch"] {
		if inputsKey.MatchString(l.Text) {
			findings = append(findings, finding(l.Number, "apply の workflow_dispatch に inputs を置かないでください。ref や commit を入力に取らせません（ADR-0602 決定14）"))
		}
	}
	for _, l := range w.on["push"] {
		if pushPaths.MatchString(l.Text) {
			findings = append(findings, finding(l.Number, "apply の workflow の push に paths を置かないでください。AWS の側の状態は、どのファイルを変えたかと無関係に plan を変えます"))
		}
	}
	// workflow 全体と job の concurrency は、どちらも実行中の run か job を取り消しうるので、在るものはすべて false でなければならない。
	wfHas, jobHas := hasConcurrency(w.preamble, ""), hasConcurrency(job.Lines, "    ")
	if !wfHas && !jobHas || wfHas && !cancelsNothing(w.preamble, "") || jobHas && !cancelsNothing(job.Lines, "    ") {
		findings = append(findings, finding(job.Number, "apply の job か workflow 全体に cancel-in-progress: false の concurrency を置いてください"))
	}
	findings = append(findings, checkRefusal(job, cred, refuseStale, finding)...)
	return append(findings, checkTerraform(job, ciApply, "apply", finding)...)
}

// checkDetection は、job が同じ workflow の検出の job の成功を待つことを検査します（ADR-0601 決定9）。
// 検出の job は名前ではなく、`make infra-code-gate` を走らせることで見分けます。job の if: は、status の
// 関数で検出の失敗を越えないよう、その成功を含むときだけ認めます。
func checkDetection(job workflow.Job, jobs []workflow.Job, finding func(int, string, ...any) lintreport.Finding) []lintreport.Finding {
	var detectors []workflow.Job
	for _, n := range needs(job) {
		for _, j := range jobs {
			if j.ID == n && detects(j) {
				detectors = append(detectors, j)
			}
		}
	}
	if len(detectors) == 0 {
		return []lintreport.Finding{finding(job.Number, "needs に、make infra-code-gate を走らせる job を同じ workflow の中に置いてください。資格情報を取る job は検出より先にコードを実行しません（ADR-0601 決定9）")}
	}
	var findings []lintreport.Finding
	for _, d := range detectors {
		// 依存先が skip すると検出の job も skip され、後続は走らないまま失敗も報告されない。
		if _, at, ok := jobKey(d, "needs"); ok {
			findings = append(findings, finding(at, "検出の job %s に needs: を置かないでください", d.ID))
		}
		for _, l := range d.Lines {
			if !commentLine.MatchString(l.Text) && skipsOrHides.MatchString(l.Text) {
				findings = append(findings, finding(l.Number, "検出の job %s に if:・continue-on-error:・working-directory:・defaults: を置かないでください。skip も失敗の無視も別の場所での実行も、成功と同じく後続を止めません", d.ID))
			}
		}
	}
	cond, at, ok := jobKey(job, "if")
	if !ok {
		return findings
	}
	if statusEscape.MatchString(cond) {
		findings = append(findings, finding(at, "job の if: に status の関数・|| ・! を置かないでください。検出の失敗を越えて走ります"))
	}
	if !slices.ContainsFunc(detectors, func(d workflow.Job) bool { return requiresSuccess(cond, d.ID) }) {
		findings = append(findings, finding(at, "job の if: に needs.%s.result == 'success' を含めてください", detectors[0].ID))
	}
	return findings
}

// checkRefusal は、id の step が資格情報の取得より前に在り、if: と continue-on-error: を持たないことを検査します。
// 拒否は skip ではなく失敗にします（ADR-0602 決定14）。
func checkRefusal(job workflow.Job, cred workflow.Line, id string, finding func(int, string, ...any) lintreport.Finding) []lintreport.Finding {
	var findings []lintreport.Finding
	refused := false
	for _, step := range workflow.SplitSteps(job) {
		if !hasStepID(step, id) {
			continue
		}
		// 資格情報を取る step 自身は、拒否の step と認めない。
		refused = step.Number < cred.Number && !slices.ContainsFunc(step.Lines, func(l workflow.Line) bool { return l.Number == cred.Number })
		for _, l := range step.Lines {
			if stepIf.MatchString(l.Text) || stepContinue.MatchString(l.Text) {
				findings = append(findings, finding(l.Number, "step %s に if: と continue-on-error: を置かないでください。拒否は skip ではなく失敗にします（ADR-0602 決定14）", id))
			}
		}
	}
	if !refused {
		findings = append(findings, finding(cred.Number, "資格情報を取る前に、id が %s の step を置いてください（ADR-0602 決定14）", id))
	}
	return findings
}

// checkTerraform は、job が terraform を entry の make でだけ走らせることを検査します。plan の入口は lock を
// 取らず（ADR-0604 決定8）、apply の入口は Policy を通った plan だけを apply する（同 決定11）ので、
// 役割の違う入口も認めません。
func checkTerraform(job workflow.Job, entry *regexp.Regexp, role string, finding func(int, string, ...any) lintreport.Finding) []lintreport.Finding {
	var findings []lintreport.Finding
	entries := 0
	for _, l := range job.Lines {
		if commentLine.MatchString(l.Text) || nameLine.MatchString(l.Text) || installsTF.MatchString(l.Text) {
			continue
		}
		matched := entry.MatchString(l.Text)
		if matched {
			entries++
		}
		if runsTerraform.MatchString(l.Text) || (!matched && ciEntry.MatchString(l.Text)) {
			findings = append(findings, finding(l.Number, "%s の job は terraform を make ci-%s-* でだけ走らせてください", role, role))
		}
	}
	if entries == 0 {
		findings = append(findings, finding(job.Number, "%s の job に make ci-%s-* がありません", role, role))
	}
	return findings
}

// checkSteps は、資格情報を持つ job の checkout が資格情報を残さないことと、資格情報を取った後に
// action を呼ばないことを検査します。step に切り出せなかった uses: は、job の行から別に数えて落とします。
func checkSteps(job workflow.Job, cred workflow.Line, finding func(int, string, ...any) lintreport.Finding) []lintreport.Finding {
	var findings []lintreport.Finding
	total, seen := 0, 0
	for _, l := range job.Lines {
		if !commentLine.MatchString(l.Text) && anyUses.MatchString(l.Text) {
			total++
		}
	}
	for _, step := range workflow.SplitSteps(job) {
		for _, l := range step.Lines {
			if !commentLine.MatchString(l.Text) && anyUses.MatchString(l.Text) {
				seen++
			}
		}
		checkout, persists := false, false
		for _, l := range step.Lines {
			if commentLine.MatchString(l.Text) {
				continue
			}
			if anyUses.MatchString(l.Text) && !stepUses.MatchString(l.Text) {
				// 引用符付きのキーやフロースタイルの step は、下の2つの検査が読めない。
				findings = append(findings, finding(l.Number, "uses: を読み取れません。`- uses: <action>` の形で書いてください"))
				continue
			}
			checkout = checkout || checkoutUses.MatchString(l.Text)
			persists = persists || noPersist.MatchString(l.Text)
			if l.Number > cred.Number && stepUses.MatchString(l.Text) {
				findings = append(findings, finding(l.Number, "資格情報を取った後の step で action を呼ばないでください。session が action へ届きます"))
			}
		}
		if checkout && !persists {
			findings = append(findings, finding(step.Number, "actions/checkout に persist-credentials: false を置いてください"))
		}
	}
	if total > seen {
		findings = append(findings, finding(job.Number, "steps を読めません（uses: %d 件のうち %d 件）。step は steps: より2桁深い `- ` で書いてください", total, total-seen))
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
				// 値を同じ行に書いた起動条件（フロー形式）は、本文として残して検査に読ませる。
				if k[2] != "" {
					out[current] = append(out[current], next)
				}
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

// cancelsNothing は、indent の桁の concurrency が cancel-in-progress: false を明示しているかを返します。
func cancelsNothing(lines []workflow.Line, indent string) bool {
	for i, l := range lines {
		if m := concurrencyAt.FindStringSubmatch(l.Text); m == nil || m[1] != indent {
			continue
		}
		for _, next := range lines[i+1:] {
			if strings.TrimSpace(next.Text) == "" || commentLine.MatchString(next.Text) {
				continue
			}
			if !strings.HasPrefix(next.Text, indent+"  ") {
				break
			}
			if m := noCancel.FindStringSubmatch(next.Text); m != nil && m[1] == indent+"  " {
				return true
			}
		}
	}
	return false
}

// makeRun は、step の run: の値が `make`、変数の代入、target の1つ、変数の代入だけで成る行に当たる正規表現を返します。
func makeRun(target string) *regexp.Regexp {
	const assign = `(?:\s+[A-Za-z_][A-Za-z0-9_]*=[^\s|;&#'"]*)*`
	return regexp.MustCompile(`^ {6}(?:- | {2})["']?run["']?[ \t]*:[ \t]*make` + assign + `\s+` + target + assign + `[ \t]*$`)
}

// requiresSuccess は、if: の式が && で結んだ項の1つとして、そのまま needs.<id>.result == 'success' を持つかを返します。
// 括弧で包んだ比較は、別の比較に使われうるので認めません。
func requiresSuccess(cond, id string) bool {
	cond = strings.TrimSpace(strings.TrimLeft(cond, ">|-+ "))
	cond = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(cond, "${{"), "}}"))
	term := regexp.MustCompile(`^needs\.` + regexp.QuoteMeta(id) + `\.result\s*==\s*'success'$`)
	return slices.ContainsFunc(strings.Split(cond, "&&"), func(t string) bool { return term.MatchString(strings.TrimSpace(t)) })
}

// detects は、job が `make infra-code-gate` を走らせる step を持つかを返します。
func detects(job workflow.Job) bool {
	return slices.ContainsFunc(job.Lines, func(l workflow.Line) bool { return detection.MatchString(l.Text) })
}

// hasConcurrency は、indent の桁に concurrency: を持つかを返します。値を1行に書いた形も数えます。
func hasConcurrency(lines []workflow.Line, indent string) bool {
	return slices.ContainsFunc(lines, func(l workflow.Line) bool {
		m := concurrencyKy.FindStringSubmatch(l.Text)
		return m != nil && m[1] == indent
	})
}

// jobKey は、job の直下の key の値と行を返します。値が次の行へ続く形は、続く行を空白でつないで返します。
func jobKey(job workflow.Job, key string) (string, int, bool) {
	for i, l := range job.Lines {
		m := jobKeyAt.FindStringSubmatch(l.Text)
		if m == nil || m[1] != key {
			continue
		}
		parts := []string{m[2]}
		for _, next := range job.Lines[i+1:] {
			if !strings.HasPrefix(next.Text, "      ") {
				break
			}
			parts = append(parts, strings.TrimSpace(next.Text))
		}
		return strings.TrimSpace(strings.Join(parts, " ")), l.Number, true
	}
	return "", 0, false
}
