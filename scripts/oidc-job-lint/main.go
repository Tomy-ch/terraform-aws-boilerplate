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
//   - `make ci-aws-credentials CI_ENV=<env> CI_ROLE=<役割> RUNNER_MODE=host` だけの run: をちょうど1回持ち、
//     `<env>-<役割>` が environment の名前と一致する。CI_ENV と CI_ROLE は、job のちょうど1つの入口
//     `make ci-<種別>-<env>-<root>` の env と、root と種別が決める role（entryCredential。ADR-0604 決定3・4・8）に一致する
//   - `needs` に、同じ workflow の中で `make infra-code-gate` を走らせる job を持ち、job の if: を持つなら
//     その job の成功を条件に含め、status の関数・論理和・否定を置かない（ADR-0601 決定9）。検出の job は
//     needs:・if:・continue-on-error:・working-directory:・defaults:・env:・shell:・container:・services: を持たず、
//     run: は `make infra-code-gate-test` と `make infra-code-gate`（RUNNER_MODE=host だけ）に限る
//   - workflow 全体と対象の job と検出の job に MAKEFLAGS・MAKEFILES・PATH を書かず、GITHUB_PATH と
//     GITHUB_ENV に書き込まず、YAML のアンカーとエイリアスを使わない。workflow 全体に defaults: と env: を、job に
//     continue-on-error:・container:・services:・defaults:・env: を、資格情報を取る step と CI の入口の step に
//     if:・continue-on-error:・shell:・working-directory: を、拒否の step に shell:・working-directory: を置かない。
//     step の env: は拒否の step と入口が読む名前（stepEnvKeys）に限り、run: を持つ step は資格情報より前の
//     役割の拒否の step・資格情報を取る step・CI の入口に限る —— どれも
//     入口を実行させないか、道具を替えるか、失敗を成功として報告させる
//   - plan の job は、workflow の起動が pull_request だけで、id が refuse-untrusted で if: と continue-on-error: を持たない step を
//     資格情報の取得より前に置き（ADR-0602 決定14）、terraform を `make ci-plan-*` でだけ、ちょうど1回走らせる
//     （lock を取らない経路。ADR-0604 決定8）。資格情報を取った後で plan の入口より前に
//     `make ci-assume-denied CI_ENV=<env> RUNNER_MODE=host` だけの run: をちょうど1回持ち、その CI_ENV は資格情報と
//     同じである —— この job の token で apply 系の role の assume が拒否されることを確かめる。この step は
//     plan の job のほかに置かない
//   - apply の job は、workflow の起動が push と入力の無い workflow_dispatch だけで（ADR-0602 決定14）、
//     push に paths を持たず、concurrency を持ち、workflow 全体と job に在る concurrency が
//     すべて cancel-in-progress: false で —— 後から起動した run が、実行中の apply を途中で止めない ——、id が refuse-stale で
//     if: と continue-on-error: を持たない step を資格情報の取得より前に置き
//     （古い commit の再実行を拒む）、terraform を `make ci-apply-*` でだけ、ちょうど1回走らせる（Policy を通った plan だけを
//     apply する経路。ADR-0604 決定11）
//   - lock の解除の job（`make ci-unlock-*` を持つ job）は、外す root の lock の解除の role（identity root は
//     unlock-identity、ほかの root は unlock）を取り、workflow の起動が
//     入力の無い workflow_dispatch だけで、cancel-in-progress: false の concurrency と refuse-stale の step を持ち、
//     terraform も他の CI の入口も走らせず、同じ workflow に plan と apply の job を置かない。concurrency の group は
//     字面で書き、`make ci-unlock-<env>-<root>` に対して `make ci-apply-<env>-<root>` を走らせるただ1つの apply の job の、
//     字面の group と一致しない —— group はリポジトリの中で workflow をまたいで共有され、同じ group では lock の解除の
//     起動が待機中の apply の run を取り消す
//   - lock を残す job（`make ci-hold-*` を持つ job）は、残す root の apply の job と同じ role（identity root は
//     apply-identity、ほかの root は apply）を取り、workflow の起動が入力の無い workflow_dispatch だけで、
//     cancel-in-progress: false の concurrency と refuse-stale の step を持ち、terraform を `make ci-hold-*` でだけ
//     1回走らせ、同じ workflow に他の CI の入口を持つ job を置かない。environment と concurrency の group は
//     字面で書き、`make ci-hold-<env>-<root>` に対して `make ci-apply-<env>-<root>` を走らせるただ1つの apply の job の
//     environment と、字面の group に一致する —— 同じ group なので apply と並んで lock を奪い合わない（ADR-0604 決定8）
//   - plan と apply と lock を残す job は lock の解除の role を取らない —— lock file を消す権限は lock の解除の job だけが持つ
//
// CI の入口（登録簿の種別の `make ci-<種別>-*`）を走らせる job が対象でなければ違反にし、
// 入口の行をファイル全体で別に数えて、job の行として読めない行があれば落とす。root の網羅も、対象の job だけを数える。
//
// 対象の job が1つも無ければ、違反とは別の終了コード 2 で失敗します（ADR-0702 決定13）。
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/cientry"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lintreport"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/requiredchecks"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workflow"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	defaultWorkflowsDir     = ".github/workflows"
	defaultEnvironmentsFile = ".github/settings/environments.json"
	defaultRulesetFile      = ".github/settings/branch-protection.json"
	defaultRootsDir         = "env"
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
	credRun       = makeRun(`ci-aws-credentials\s+CI_ENV=` + envWord + `\s+CI_ROLE=[a-z-]+`)
	ciEnv         = regexp.MustCompile(`\bCI_ENV=(` + envWord + `)\b`)
	ciRole        = func() *regexp.Regexp {
		roles := cientry.Roles()
		for i, r := range roles {
			roles[i] = regexp.QuoteMeta(r)
		}
		return regexp.MustCompile(`\bCI_ROLE=(` + strings.Join(roles, "|") + `)(?:\s|$)`)
	}()
	// makeRun の形以外を拒む理由は scripts/README.md の oidc-job-lint の行が持つ。
	ciPlan     = entryRuns[cientry.Plan]
	deniedRun  = makeRun(`ci-assume-denied\s+CI_ENV=` + envWord)
	deniedLine = regexp.MustCompile(`\bmake\b.*\bci-assume-denied\b`)
	ciApply    = entryRuns[cientry.Apply]
	ciEntry    = cientry.MustPattern(`\bmake\b.*\bci-`, `-[a-z0-9-]+`, cientry.Kinds()...)
	// entryToken は make の有無も名前の形も問わない。行継続・$(MAKE)・式で組んだ名前の入口も数える。
	entryToken  = cientry.MustPattern(`\bci-`, `-`, cientry.Kinds()...)
	unlockEntry = cientry.MustPattern(`\bmake\b.*\bci-`, `-([a-z0-9-]+)`, kindOf[cientry.Unlock])
	holdEntry   = cientry.MustPattern(`\bmake\b.*\bci-`, `-([a-z0-9-]+)`, kindOf[cientry.Hold])
	otherEntry  = cientry.MustPattern(`\bmake\b.*\bci-`, `-[a-z0-9-]+`, slices.DeleteFunc(cientry.Kinds(), func(k cientry.Kind) bool { return k.Name == cientry.Hold })...)
	applyEntry  = cientry.MustPattern(`\bmake\b.*\bci-`, `-([a-z0-9-]+)`, kindOf[cientry.Apply])
	planEntry   = cientry.MustPattern(`\bmake\b.*\bci-`, `-[a-z0-9-]+`, kindOf[cientry.Plan])
	runEntry    = cientry.MustPattern(`\bmake\b.*\bci-`, `-[a-z0-9-]+`, kindOf[cientry.Plan], kindOf[cientry.Apply])
	detection   = makeRun(`infra-code-gate`)
	orderCheck  = makeRun(`resource-order-check`)
	layerCheck  = makeRun(`layer-check`)
	rootCheck   = makeRun(`root-check`)
	// status の関数・論理和・否定は、検出の失敗を越えて job を走らせうる。!= は比較なので除く。
	statusEscape = regexp.MustCompile(`\b(?:always|cancelled|failure)\(\)|\|\||!(?:[^=]|$)`)
	pushPaths    = regexp.MustCompile(`^\s+["']?paths(?:-ignore)?["']?[ \t]*:`)
	// tf-job-lint と同じく、サブコマンドを列挙しない。terraform を入れる行（mise の tools）は起動ではないので含めない。
	// local.mk の init-* / plan-* も terraform を走らせる。それ以外の名前の make のターゲットは字面からは選べない。
	runsTerraform = regexp.MustCompile(`\bmake\b.*\s(?:tf|init|plan)-|(?:^|[\s;&|(/"'])(?:terraform|tofu)["']?(?:\s|$)`)
	nameLine      = regexp.MustCompile(`^\s+(?:- )?name:`)
	// installsTF は、mise の tools: で terraform を入れる行。run: の行は、同じ字面を含んでも起動として検査する。
	installsTF   = regexp.MustCompile(`^\s+["']?tools["']?[ \t]*:.*\baqua:hashicorp/terraform\b`)
	onKey        = regexp.MustCompile(`^["']?on["']?:[ \t]*(.*?)[ \t]*(?:#.*)?$`)
	triggerKey   = regexp.MustCompile(`^ {2}([a-z_]+):[ \t]*([^#]*?)[ \t]*(?:#.*)?$`)
	inputsKey    = regexp.MustCompile(`^\s+["']?inputs["']?[ \t]*:`)
	needsScalar  = regexp.MustCompile(`^ {4}needs:[ \t]*(.*?)[ \t]*(?:#.*)?$`)
	jobKeyAt     = regexp.MustCompile(`^ {4}["']?([a-z-]+)["']?[ \t]*:[ \t]*(.*?)[ \t]*$`)
	skipsOrHides = regexp.MustCompile(`^\s+(?:- )?["']?(?:if|continue-on-error|working-directory|defaults|env|shell|container|services)["']?[ \t]*:`)
	makeFlags    = regexp.MustCompile(`\b(?:GNU)?MAKEFLAGS\b|\bMFLAGS\b|\bMAKEFILES\b`)
	pathKey      = regexp.MustCompile(`(?:^|[\s{,])["']?PATH["']?[ \t]*:`)
	stepFiles    = regexp.MustCompile(`\bGITHUB_(?:PATH|ENV)\b`)
	// アンカーとエイリアスは、別の場所に書いた値をこの行へ持ち込む。行ごとの読み方では追えない。
	yamlAlias     = regexp.MustCompile(`(?:^|[:\-][ \t]+)[&*][A-Za-z0-9_-]+(?:[ \t]|$)|<<[ \t]*:`)
	envAt         = regexp.MustCompile(`^["']?env["']?[ \t]*:`)
	stepEnv       = regexp.MustCompile(`^ {6}(?:- | {2})["']?env["']?[ \t]*:[ \t]*(.*?)[ \t]*$`)
	stepEnvKey    = regexp.MustCompile(`^ +["']?([^"':\s]+)["']?[ \t]*:[ \t]*(.*?)[ \t]*$`)
	blockScalar   = regexp.MustCompile(`^[|>][-+0-9]*(?:[ \t]+#.*)?$`)
	stepRun       = regexp.MustCompile(`^ {6}(?:- | {2})["']?run["']?[ \t]*:`)
	stepWorkDir   = regexp.MustCompile(`^ {6}(?:- | {2})["']?working-directory["']?[ \t]*:`)
	detectionRun  = makeRun(`infra-code-gate(?:-test)?`)
	orderRun      = makeRun(`resource-order-check(?:-test)?`)
	orderTestRun  = makeRun(`resource-order-check-test`)
	layerRun      = makeRun(`layer-check(?:-test)?`)
	layerTestRun  = makeRun(`layer-check-test`)
	rootRun       = makeRun(`root-check(?:-test)?`)
	rootTestRun   = makeRun(`root-check-test`)
	stepShell     = regexp.MustCompile(`^ {6}(?:- | {2})["']?shell["']?[ \t]*:`)
	defaultsAt    = regexp.MustCompile(`^["']?defaults["']?[ \t]*:`)
	listItem      = regexp.MustCompile(`^ {6}- [ \t]*["']?([A-Za-z0-9_-]+)["']?[ \t]*(?:#.*)?$`)
	concurrencyAt = regexp.MustCompile(`^( *)["']?concurrency["']?[ \t]*:[ \t]*(?:#.*)?$`)
	concurrencyKy = regexp.MustCompile(`^( *)["']?concurrency["']?[ \t]*:`)
	concurrencyIs = regexp.MustCompile(`^( *)["']?concurrency["']?[ \t]*:[ \t]*([^#\s][^#]*?)[ \t]*(?:#.*)?$`)
	groupAt       = regexp.MustCompile(`^( *)["']?group["']?[ \t]*:[ \t]*(.*?)[ \t]*(?:#.*)?$`)
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

// stepEnvKeys は、資格情報を取る job の step が env: に置いてよい名前です。拒否の step が読む値と、
// 入口が読む値に限る。ほかの名前を拒む理由は scripts/README.md の oidc-job-lint の行が持つ。
var stepEnvKeys = []string{"GH_TOKEN", "REPO", "REF", "SHA", "ACCOUNT_ID", "HEAD_REPO_ID", "REPO_ID", "ACTOR", "CI_ACCOUNT_ID", "CI_PR"}

var (
	errNoTarget   = xerrors.New("OIDC で role を assume する job が1つもありません")
	errViolations = xerrors.New("違反があります")
	errEnvDecl    = xerrors.New("environments.json を読めません")
)

func main() {
	log.SetFlags(0)
	if err := run(defaultWorkflowsDir, defaultEnvironmentsFile, defaultRulesetFile, defaultRootsDir, os.Stdout); err != nil {
		if xerrors.Is(err, errNoTarget) {
			log.Printf("❌ oidc-job-lint: %v", err)
			os.Exit(2)
		}
		log.Fatalf("❌ oidc-job-lint: %v", err)
	}
}

// run は dir の workflow を検査します。rootsDir が空でなければ、その下の root ごとに CI の経路があることも見る。
func run(dir, envFile, rulesetFile, rootsDir string, out io.Writer) error {
	envs, err := readEnvironments(envFile)
	if err != nil {
		return err
	}
	contexts, err := requiredchecks.Read(rulesetFile)
	if err != nil {
		return xerrors.Wrap(err, "required の context の読み取り")
	}
	required := map[string]bool{}
	for _, c := range contexts {
		required[c] = true
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

	var (
		findings []lintreport.Finding
		groups   []groupRecord
	)
	sources := map[string]string{}
	targets := 0
	prGates := map[string]int{}
	for _, p := range workflow.SelectFiles(names, dir) {
		raw, err := root.ReadFile(filepath.Base(p))
		if err != nil {
			return xerrors.Wrapf(err, "%s の読み取り", p)
		}
		sources[p] = string(raw)
		n, f := check(p, string(raw), envs)
		targets += n
		findings = append(findings, f...)
		_, f = checkRevertWiring(p, string(raw))
		findings = append(findings, f...)
		groups = append(groups, groupRecords(p, string(raw))...)
		// 複数の検査を走らせる job では、job 単位の違反が検査ごとに同じ文で出るので、1件にする。
		seen := map[lintreport.Finding]bool{}
		for _, g := range applyGates {
			n, f = checkRequiredGateJobs(p, string(raw), required, g)
			prGates[g.noun] += n
			for _, x := range f {
				if !seen[x] {
					seen[x] = true
					findings = append(findings, x)
				}
			}
		}
	}
	findings = append(findings, checkUnlockIsolation(groups)...)
	findings = append(findings, checkHoldPairing(groups)...)
	for _, g := range applyGates {
		findings = append(findings, checkPullRequestGate(dir, groups, prGates[g.noun], g)...)
	}
	roots := 0
	// root が1つも無いときも、違反を先に出してから errNoTarget を返す（targets == 0 と同じ順序）。
	var noRoots error
	if rootsDir != "" {
		n, f, err := checkRootCoverage(rootsDir, sources)
		roots = n
		switch {
		case xerrors.Is(err, errNoTarget):
			noRoots = err
		case err != nil:
			return err
		}
		findings = append(findings, f...)
	}
	if len(findings) > 0 {
		fmt.Fprintf(out, "❌ oidc-job-lint: %d 件の違反\n\n", len(findings))
		fmt.Fprintln(out, lintreport.Format(findings))
		return xerrors.Wrapf(errViolations, "%d 件", len(findings))
	}
	if noRoots != nil {
		return noRoots
	}
	if targets == 0 {
		return xerrors.Wrapf(errNoTarget, "%s", dir)
	}
	fmt.Fprintf(out, "✅ oidc-job-lint: OIDC で role を assume する job %d 件の environment・役割・起動条件と、root %d 件の CI の経路を確認しました\n", targets, roots)
	return nil
}

// prGateIf は、Pull Request の必須検査の job が置いてよい唯一の if: の式。changes の job が path から決めた
// 条件で、フィルタが判定に届かなければ空になり、job は走る（.github/workflows/README.md の「必須検査は
// すべての Pull Request で報告される」の fail open）。
const prGateIf = "!cancelled() && needs.changes.outputs.relevant != 'false'"

// checkRequiredGateJobs は、pull_request で起動する workflow のうち、required の context を報告し
// g の自己テストと検査の両方を走らせる job の数と、その job が検査を走らせずに成功を報告しうる形の違反を返します。
func checkRequiredGateJobs(file, source string, required map[string]bool, g gate) (int, []lintreport.Finding) {
	split := workflow.SplitJobs(source)
	on, _ := triggers(split.Preamble)
	if _, ok := on["pull_request"]; !ok {
		return 0, nil
	}
	n := 0
	var findings []lintreport.Finding
	for _, j := range split.Jobs {
		if !required[j.ID] || !runsGate(j, g) || !hasLine(j, g.testRun) {
			continue
		}
		n++
		findings = append(findings, checkRequiredGateJob(file, j, split.Preamble, g)...)
	}
	return n, findings
}

// checkRequiredGateJob は、必須検査の job と g の検査の step が、skip も失敗の無視も別の場所や別の shell
// での実行もできないことを検査します。skip された必須検査は成功として数えられるので、job の if: は
// prGateIf の形だけを認める。
func checkRequiredGateJob(file string, job workflow.Job, preamble []workflow.Line, g gate) []lintreport.Finding {
	finding := func(line int, format string, args ...any) lintreport.Finding {
		return lintreport.Finding{File: file, Line: line, Message: fmt.Sprintf("job %s: ", job.ID) + fmt.Sprintf(format, args...)}
	}
	var findings []lintreport.Finding
	if cond, at, ok := jobKey(job, "if"); ok && ifExpression(cond) != prGateIf {
		findings = append(findings, finding(at, "検査を走らせる必須検査の job の if: は ${{ %s }} だけにしてください。定数・status の関数・|| ・! は、検査を走らせずに成功を報告させます", prGateIf))
	}
	for _, key := range []string{"continue-on-error", "container", "services", "defaults", "env"} {
		if _, at, ok := jobKey(job, key); ok {
			findings = append(findings, finding(at, "検査を走らせる必須検査の job に %s: を置かないでください", key))
		}
	}
	for _, l := range preamble {
		if defaultsAt.MatchString(l.Text) || envAt.MatchString(l.Text) {
			findings = append(findings, finding(l.Number, "検査を走らせる必須検査の workflow に defaults: と env: を置かないでください。run: の shell・場所・道具を替えます"))
		}
	}
	for _, l := range append(slices.Clone(preamble), job.Lines...) {
		if commentLine.MatchString(l.Text) {
			continue
		}
		if makeFlags.MatchString(l.Text) {
			findings = append(findings, finding(l.Number, "MAKEFLAGS と MAKEFILES を渡さないでください。-n や -i は何も検査せずに成功します"))
		}
		if pathKey.MatchString(l.Text) {
			findings = append(findings, finding(l.Number, "PATH を替えないでください。make を別のものに差し替えます"))
		}
		findings = append(findings, checkIndirect(l, finding)...)
	}
	for _, step := range workflow.SplitSteps(job) {
		if runs, ok := stepRunLine(step); !ok || !g.runs.MatchString(runs.Text) {
			continue
		}
		for _, l := range step.Lines {
			if stepIf.MatchString(l.Text) || stepContinue.MatchString(l.Text) || stepShell.MatchString(l.Text) || stepWorkDir.MatchString(l.Text) {
				findings = append(findings, finding(l.Number, "%sの step に if:・continue-on-error:・shell:・working-directory: を置かないでください。skip も失敗も成功として報告され、別の Makefile を走らせます", g.noun))
			}
		}
	}
	return findings
}

// ifExpression は、if: の値から ${{ }} とブロックの記号を外し、空白を1つに詰めた式を返します。
func ifExpression(cond string) string {
	cond = strings.TrimSpace(strings.TrimLeft(cond, ">|-+ "))
	cond = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(cond, "${{"), "}}"))
	return strings.Join(strings.Fields(cond), " ")
}

// checkPullRequestGate は、apply の job が待つ g の検査を、Pull Request の必須検査も走らせることを検査します。
// apply の側だけでは、規則を崩す変更が merge されてから apply が止まる。
func checkPullRequestGate(dir string, records []groupRecord, found int, g gate) []lintreport.Finding {
	if found > 0 || !slices.ContainsFunc(records, func(r groupRecord) bool { return r.kind == cientry.Apply }) {
		return nil
	}
	return []lintreport.Finding{{File: dir, Line: 1, Message: fmt.Sprintf("pull_request で起動し保護設定が required とする job に、%s を走らせるものがありません。apply の job が待つ%sを、merge の前の必須検査にも置いてください", g.targets, g.noun)}}
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
	// entries は、CI の入口の行のうち、job の行として読めた行の数。
	entries := 0
	for _, job := range split.Jobs {
		n := jobEntryLines(job)
		entries += n
		values, at, ok := permissionsBlock(job.Lines, "    ")
		if ok && unreadable(values) {
			attributed++
			findings = append(findings, lintreport.Finding{File: file, Line: at,
				Message: fmt.Sprintf("job %s の permissions を読めません。write-all と1行の {} は使わず、権限を1行ずつ書いてください", job.ID)})
			continue
		}
		if !assumesRole(job) {
			// id-token の行を持つが読めない job は、下の countTokenLines の違反が原因を言う。
			if n > 0 && !hasTokenLine(job) {
				first := job.Lines[slices.IndexFunc(job.Lines, func(l workflow.Line) bool { return entryLine(l.Text) })].Number
				findings = append(findings, lintreport.Finding{File: file, Line: job.Number,
					Message: fmt.Sprintf("job %s は CI の入口（%s）を走らせますが（%d 行目）、job の permissions に id-token: write がありません。この job はこの検査の規則を受けず、root の経路にも数えません", job.ID, entryNames, first)})
			}
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
	if n := countEntryLines(source); n > entries {
		findings = append(findings, lintreport.Finding{File: file, Line: 1,
			Message: fmt.Sprintf("CI の入口（%s）を持つ行 %d 件のうち %d 件を、job の行として読めません", entryNames, n, n-entries)})
	}
	return targets, findings
}

// assumesRole は、job がこの検査の対象（job の permissions に id-token: write を読める形で持つ job）かを返します。
// check・groupRecords・checkRootCoverage はこの述語で対象を決める。
func assumesRole(job workflow.Job) bool {
	values, _, ok := permissionsBlock(job.Lines, "    ")
	return ok && !unreadable(values) && hasIDToken(values)
}

// hasTokenLine は、job が token を得うる id-token の行（値が read か none と読めないもの）を持つかを返します。
func hasTokenLine(job workflow.Job) bool {
	return slices.ContainsFunc(job.Lines, func(l workflow.Line) bool {
		if commentLine.MatchString(l.Text) {
			return false
		}
		m := idTokenKey.FindStringSubmatch(l.Text)
		return m != nil && !tokenDenied.MatchString(m[1])
	})
}

// entryLine は、行が CI の入口を走らせうる字面かを返します。コメントと name: の行は除き、run: の外の字面
// （echo の引数、env: の値）も入口として数える。
func entryLine(text string) bool {
	return !commentLine.MatchString(text) && !nameLine.MatchString(text) && entryToken.MatchString(text)
}

// countEntryLines は、job の切り出しとは別に、CI の入口の行をファイル全体で数えます。
func countEntryLines(source string) int {
	n := 0
	for _, text := range strings.Split(source, "\n") {
		if entryLine(text) {
			n++
		}
	}
	return n
}

func jobEntryLines(job workflow.Job) int {
	n := 0
	for _, l := range job.Lines {
		if entryLine(l.Text) {
			n++
		}
	}
	return n
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
	if !credRun.MatchString(cred.Text) {
		findings = append(findings, finding(cred.Number, "資格情報を取る step は run: make ci-aws-credentials CI_ENV=<env> CI_ROLE=<役割> RUNNER_MODE=host だけにしてください。ほかの代入やオプションは、token を出力させるか道具を替えます"))
	}
	if env != "" && env != e[1]+"-"+role {
		findings = append(findings, finding(cred.Number, "environment %s と、取る role の %s-%s が一致しません", env, e[1], role))
	}
	kind := jobKind(job, role)
	plan := kind == cientry.Plan
	findings = append(findings, checkRole(job, cred, role, kind, finding)...)
	findings = append(findings, checkSteps(job, cred, finding)...)
	findings = append(findings, checkDetection(job, w.jobs, finding)...)
	refusal := refuseStale
	if plan {
		refusal = refuseUntrusted
	}
	findings = append(findings, checkHidden(job, cred, refusal, w, finding)...)

	switch kind {
	case cientry.Unlock:
		return append(findings, checkUnlock(job, cred, w, finding)...)
	case cientry.Hold:
		return append(findings, checkHold(job, cred, w, finding)...)
	case cientry.Plan:
		return append(findings, checkPlan(job, cred, w, finding)...)
	}
	return append(findings, checkApply(job, cred, w, finding)...)
}

// jobKind は、資格情報を取る job の種別を、走らせる入口と取る role から決めます。lock の解除の入口を先に、
// lock を残す入口を次に見る。plan の入口の job が lock の解除の role を取った違反は checkRole が出すので、
// plan の規則で見る。ほかの role を取った plan の入口の job は、apply の規則が落とす。
func jobKind(job workflow.Job, role string) string {
	switch {
	case isUnlock(job):
		return cientry.Unlock
	case isHold(job):
		return cientry.Hold
	case kindHasRole(kindOf[cientry.Plan], role) || (isUnlockRole(role) && hasLine(job, planEntry)):
		return cientry.Plan
	}
	return cientry.Apply
}

func checkPlan(job workflow.Job, cred workflow.Line, w workflowInfo, finding func(int, string, ...any) lintreport.Finding) []lintreport.Finding {
	var findings []lintreport.Finding
	if keys := triggerNames(w.on); !eventsAllowed(keys, kindOf[cientry.Plan]) {
		findings = append(findings, finding(w.onLine, "plan の job を持つ workflow の起動は pull_request だけにしてください（%s）", strings.Join(keys, ", ")))
	}
	findings = append(findings, checkRefusal(job, cred, refuseUntrusted, finding)...)
	findings = append(findings, checkDenied(job, cred, finding)...)
	return append(findings, checkTerraform(job, cientry.Plan, finding)...)
}

// checkDenied は、plan の job が `make ci-assume-denied` をちょうど1回、資格情報を取った後で plan の入口より前に
// 持ち、その CI_ENV が資格情報の CI_ENV と同じことを検査します。step を消すか別の env へ向ける変更は、
// AWS の側の検査をすべて緑のまま、拒否されるべき主体の確認だけを止めます。
func checkDenied(job workflow.Job, cred workflow.Line, finding func(int, string, ...any) lintreport.Finding) []lintreport.Finding {
	var lines []workflow.Line
	for _, l := range job.Lines {
		if !commentLine.MatchString(l.Text) && !nameLine.MatchString(l.Text) && deniedLine.MatchString(l.Text) {
			lines = append(lines, l)
		}
	}
	if len(lines) != 1 {
		return []lintreport.Finding{finding(job.Number, "plan の job に make ci-assume-denied を1回だけ置いてください（%d 回）。apply 系の role の assume が拒否されることを、Pull Request ごとに確かめます", len(lines))}
	}
	denied := lines[0]
	var findings []lintreport.Finding
	if !deniedRun.MatchString(denied.Text) {
		findings = append(findings, finding(denied.Number, "拒否の確認の step は run: make ci-assume-denied CI_ENV=<env> RUNNER_MODE=host だけにしてください。ほかの代入は試す役割を狭めます"))
	}
	if denied.Number < cred.Number {
		findings = append(findings, finding(denied.Number, "make ci-assume-denied は資格情報を取る step より後に置いてください。plan の role の assume が通った後でなければ、拒否が信頼条件によるものか分かりません"))
	}
	for _, l := range job.Lines {
		if !commentLine.MatchString(l.Text) && ciPlan.MatchString(l.Text) && l.Number < denied.Number {
			findings = append(findings, finding(denied.Number, "make ci-assume-denied は plan の入口より前に置いてください。信頼条件が崩れた run で plan の全文を置きません"))
			break
		}
	}
	if d, c := ciEnv.FindStringSubmatch(denied.Text), ciEnv.FindStringSubmatch(cred.Text); d != nil && c != nil && d[1] != c[1] {
		findings = append(findings, finding(denied.Number, "make ci-assume-denied の CI_ENV=%s が、資格情報の CI_ENV=%s と違います", d[1], c[1]))
	}
	return findings
}

func checkApply(job workflow.Job, cred workflow.Line, w workflowInfo, finding func(int, string, ...any) lintreport.Finding) []lintreport.Finding {
	var findings []lintreport.Finding
	if keys := triggerNames(w.on); !eventsAllowed(keys, kindOf[cientry.Apply]) {
		findings = append(findings, finding(w.onLine, "apply の job を持つ workflow の起動は push と workflow_dispatch だけにしてください（%s）", strings.Join(keys, ", ")))
	}
	findings = append(findings, checkTriggerForm(w, "apply", finding)...)
	for _, l := range w.on["push"] {
		if pushPaths.MatchString(l.Text) {
			findings = append(findings, finding(l.Number, "apply の workflow の push に paths を置かないでください。AWS の側の状態は、どのファイルを変えたかと無関係に plan を変えます"))
		}
	}
	findings = append(findings, checkNoCancel(job, w, "apply", finding)...)
	for _, g := range applyGates {
		findings = append(findings, checkApplyGate(job, w.jobs, g, finding)...)
	}
	findings = append(findings, checkUpstream(job, w.jobs, finding)...)
	findings = append(findings, checkRefusal(job, cred, refuseStale, finding)...)
	return append(findings, checkTerraform(job, cientry.Apply, finding)...)
}

// checkTriggerForm は、push と workflow_dispatch の起動条件がブロック形式で、workflow_dispatch が inputs を
// 持たないことを検査します。kind は違反の文に出す job の種別です。
func checkTriggerForm(w workflowInfo, kind string, finding func(int, string, ...any) lintreport.Finding) []lintreport.Finding {
	var findings []lintreport.Finding
	for _, k := range []string{"push", "workflow_dispatch"} {
		for _, l := range w.on[k] {
			if strings.Contains(l.Text, "{") {
				findings = append(findings, finding(l.Number, "%s の workflow の起動条件をフロー形式で書かないでください。paths と inputs を読めません", kind))
			}
		}
	}
	for _, l := range w.on["workflow_dispatch"] {
		if inputsKey.MatchString(l.Text) {
			findings = append(findings, finding(l.Number, "%s の workflow_dispatch に inputs を置かないでください。ref や commit を入力に取らせません（ADR-0602 決定14）", kind))
		}
	}
	return findings
}

// checkNoCancel は、job か workflow 全体に concurrency が在り、在るものがすべて cancel-in-progress: false で
// あることを検査します。どちらの concurrency も実行中の run か job を取り消しうる。
func checkNoCancel(job workflow.Job, w workflowInfo, kind string, finding func(int, string, ...any) lintreport.Finding) []lintreport.Finding {
	wfHas, jobHas := hasConcurrency(w.preamble, ""), hasConcurrency(job.Lines, "    ")
	if !wfHas && !jobHas || wfHas && !cancelsNothing(w.preamble, "") || jobHas && !cancelsNothing(job.Lines, "    ") {
		return []lintreport.Finding{finding(job.Number, "%s の job か workflow 全体に cancel-in-progress: false の concurrency を置いてください", kind)}
	}
	return nil
}

// checkRole は、資格情報の CI_ENV と CI_ROLE が、job の種別の入口 make ci-<種別>-<env>-<root> の env と、root と
// 種別が決める role（entryCredential）に一致し、lock の解除の job のほかが lock の解除の role を取らないことを
// 検査します。入口がちょうど1つでない job は比べない —— 入口の数の違反を checkTerraform・checkHoldEntry・
// checkUnlockEntry が出す。
func checkRole(job workflow.Job, cred workflow.Line, role, kind string, finding func(int, string, ...any) lintreport.Finding) []lintreport.Finding {
	var findings []lintreport.Finding
	if kind != cientry.Unlock && kind != cientry.Hold && isUnlockRole(role) {
		findings = append(findings, finding(cred.Number, "%s の role は lock の解除の job（make ci-unlock-*）だけが取ってください", role))
	}
	entries := kindEntries(job, kind)
	if len(entries) != 1 {
		return findings
	}
	envRoot := entryRuns[kind].FindStringSubmatch(entries[0].Text)[1]
	env, want, ok := entryCredential(kind, envRoot)
	if !ok {
		return append(findings, finding(entries[0].Number, "入口 make ci-%s-%s の root を読めません。<env>-identity か <env>-workload の名前にしてください（ADR-0604 決定5）", kind, envRoot))
	}
	if role != want {
		switch kind {
		case cientry.Unlock:
			findings = append(findings, finding(cred.Number, "lock の解除の job は %s の role を取ってください（%s）。その root の lock file を消す権限だけを持つ role です", want, role))
		case cientry.Hold:
			findings = append(findings, finding(cred.Number, "lock を残す job は %s の role を取ってください（%s）。その root の apply の job と同じ role で lock を取ります（ADR-0604 決定8）", want, role))
		default:
			if !isUnlockRole(role) {
				findings = append(findings, finding(cred.Number, "%s の job は %s の role を取ってください（%s）。入口 make ci-%s-%s の root が決める role です（ADR-0604 決定3・4）", kind, want, role, kind, envRoot))
			}
		}
	}
	if m := ciEnv.FindStringSubmatch(cred.Text); m != nil && m[1] != env {
		findings = append(findings, finding(cred.Number, "資格情報の CI_ENV=%s が、入口 make ci-%s-%s の env %s と違います", m[1], kind, envRoot, env))
	}
	return findings
}

func isUnlockRole(role string) bool {
	return slices.ContainsFunc(cientry.Kinds(), func(k cientry.Kind) bool {
		return k.Lock == cientry.LockRemove && kindHasRole(k, role)
	})
}

func kindHasRole(k cientry.Kind, role string) bool {
	return role != "" && (k.Role(cientry.RootIdentity) == role || k.Role(cientry.RootWorkload) == role)
}

// eventsAllowed は、workflow の起動のイベント keys が空でなく、すべて種別 k が起動として許すイベントかを返します。
func eventsAllowed(keys []string, k cientry.Kind) bool {
	return len(keys) > 0 && !slices.ContainsFunc(keys, func(e string) bool { return !slices.Contains(k.Events, e) })
}

// entryCredential は、入口 make ci-<kind>-<envRoot> の envRoot を末尾の -identity か -workload で切り、
// 資格情報が取るべき env と role を返します（ADR-0604 決定3・4・8）。
// どちらの接尾辞でもないか、env が空か、種別が登録簿に無ければ ok を偽にする。
func entryCredential(kind, envRoot string) (env, role string, ok bool) {
	env, root, ok := cientry.SplitEnvRoot(envRoot)
	if !ok {
		return "", "", false
	}
	k, ok := cientry.Lookup(kind)
	if !ok {
		return "", "", false
	}
	return env, k.Role(root), true
}

// kindOf は、この検査が規則で名指す種別。どれかが登録簿に無ければ、零値の種別で検査を組まないよう起動時に panic する。
var kindOf = func() map[string]cientry.Kind {
	m := map[string]cientry.Kind{}
	for _, name := range []string{cientry.Plan, cientry.Apply, cientry.Unlock, cientry.Hold} {
		k, ok := cientry.Lookup(name)
		if !ok {
			panic(fmt.Sprintf("oidc-job-lint: 規則で名指す種別 %s が lib/cientry の登録簿にありません", name))
		}
		m[name] = k
	}
	return m
}()

// entryRuns は、種別ごとの入口の run: の形。捕捉するのは入口の <env>-<root>。
var entryRuns = func() map[string]*regexp.Regexp {
	m := map[string]*regexp.Regexp{}
	for _, k := range cientry.Kinds() {
		m[k.Name] = makeRun(`ci-` + regexp.QuoteMeta(k.Name) + `-([a-z0-9-]+)`)
	}
	return m
}()

// isKindEntry は、行が種別 kind の入口の run: かを返します。入口の数と、資格情報と比べる入口は、この述語で決める。
func isKindEntry(kind, text string) bool {
	return !commentLine.MatchString(text) && !nameLine.MatchString(text) && entryRuns[kind].MatchString(text)
}

func kindEntries(job workflow.Job, kind string) []workflow.Line {
	var lines []workflow.Line
	for _, l := range job.Lines {
		if isKindEntry(kind, l.Text) {
			lines = append(lines, l)
		}
	}
	return lines
}

// checkHold は、lock の解除を障害の前に試すために lock を外さずに残す job を検査します。入口の本数と terraform の
// 直接の実行は checkHoldEntry が、apply の job との group と environment の一致は checkHoldPairing が見る。
func checkHold(job workflow.Job, cred workflow.Line, w workflowInfo, finding func(int, string, ...any) lintreport.Finding) []lintreport.Finding {
	var findings []lintreport.Finding
	if keys := triggerNames(w.on); !eventsAllowed(keys, kindOf[cientry.Hold]) {
		findings = append(findings, finding(w.onLine, "lock を残す job を持つ workflow の起動は workflow_dispatch だけにしてください。push で起動すると、merge のたびに apply を止める lock を残します（%s）", strings.Join(keys, ", ")))
	}
	findings = append(findings, checkTriggerForm(w, "lock を残す", finding)...)
	findings = append(findings, checkNoCancel(job, w, "lock を残す", finding)...)
	for _, j := range w.jobs {
		if j.ID != job.ID && hasLine(j, otherEntry) {
			findings = append(findings, finding(j.Number, "lock を残す job と同じ workflow に、ほかの CI の入口を持つ job（%s）を置かないでください。同じ run の中で lock を残す処理と並んで走ります", j.ID))
		}
	}
	findings = append(findings, checkRefusal(job, cred, refuseStale, finding)...)
	return append(findings, checkHoldEntry(job, finding)...)
}

// checkHoldEntry は、lock を残す job が make ci-hold-* をちょうど1回だけ走らせ、terraform を直に走らせず、他の CI の
// 入口も走らせないことを検査します。
func checkHoldEntry(job workflow.Job, finding func(int, string, ...any) lintreport.Finding) []lintreport.Finding {
	var findings []lintreport.Finding
	entries := 0
	for _, l := range job.Lines {
		if commentLine.MatchString(l.Text) || nameLine.MatchString(l.Text) || installsTF.MatchString(l.Text) {
			continue
		}
		matched := isKindEntry(cientry.Hold, l.Text)
		if matched {
			entries++
		}
		if runsTerraform.MatchString(l.Text) || (!matched && ciEntry.MatchString(l.Text)) {
			findings = append(findings, finding(l.Number, "lock を残す job は terraform を直に走らせず、他の CI の入口も走らせず、make ci-hold-* だけを走らせてください"))
		}
	}
	if entries != 1 {
		findings = append(findings, finding(job.Number, "lock を残す job は make ci-hold-* を1回だけ走らせてください（%d 回）", entries))
	}
	return findings
}

// checkUnlock は、state に残った lock を外す job を検査します。人が起動したときだけ走り、実行中の lock の解除を
// 取り消さず、terraform も他の入口も走らせないことを見ます。apply の group との不一致は checkUnlockIsolation が見る。
func checkUnlock(job workflow.Job, cred workflow.Line, w workflowInfo, finding func(int, string, ...any) lintreport.Finding) []lintreport.Finding {
	var findings []lintreport.Finding
	if keys := triggerNames(w.on); !eventsAllowed(keys, kindOf[cientry.Unlock]) {
		findings = append(findings, finding(w.onLine, "lock の解除の job を持つ workflow の起動は workflow_dispatch だけにしてください。push で起動すると、merge のたびに lock を消す経路になります（%s）", strings.Join(keys, ", ")))
	}
	findings = append(findings, checkTriggerForm(w, "lock の解除", finding)...)
	findings = append(findings, checkNoCancel(job, w, "lock の解除", finding)...)
	for _, j := range w.jobs {
		if j.ID != job.ID && (hasLine(j, runEntry) || hasLine(j, holdEntry)) {
			findings = append(findings, finding(j.Number, "lock の解除の job と同じ workflow に plan と apply と lock を残す job（%s）を置かないでください。同じ run の中で lock の解除と並んで走ります", j.ID))
		}
	}
	findings = append(findings, checkRefusal(job, cred, refuseStale, finding)...)
	return append(findings, checkUnlockEntry(job, finding)...)
}

// checkUnlockEntry は、lock の解除の job が make ci-unlock-* をちょうど1回だけ走らせ、terraform も他の CI の入口も
// 走らせないことを検査します。
func checkUnlockEntry(job workflow.Job, finding func(int, string, ...any) lintreport.Finding) []lintreport.Finding {
	var findings []lintreport.Finding
	entries := 0
	for _, l := range job.Lines {
		if commentLine.MatchString(l.Text) || nameLine.MatchString(l.Text) || installsTF.MatchString(l.Text) {
			continue
		}
		matched := isKindEntry(cientry.Unlock, l.Text)
		if matched {
			entries++
		}
		if runsTerraform.MatchString(l.Text) || (!matched && ciEntry.MatchString(l.Text)) {
			findings = append(findings, finding(l.Number, "lock の解除の job は terraform も他の CI の入口も走らせず、make ci-unlock-* だけを走らせてください"))
		}
	}
	if entries != 1 {
		findings = append(findings, finding(job.Number, "lock の解除の job は make ci-unlock-* を1回だけ走らせてください（%d 回）", entries))
	}
	return findings
}

// envWord は env の名前の形。log-archive のようにハイフンで区切った語も env の名前である。
const envWord = `[a-z0-9]+(?:-[a-z0-9]+)*`

var entryNames = func() string {
	kinds := cientry.Kinds()
	names := make([]string, len(kinds))
	for i, k := range kinds {
		names[i] = "ci-" + k.Name + "-*"
	}
	return "make " + strings.Join(names, "・")
}()

// groupRecord は、資格情報を取る job のうち apply の job と lock の解除の job と lock を残す job の、入口の
// `<env>-<root>` と実効の concurrency の group と environment です。
type groupRecord struct {
	file        string
	job         workflow.Job
	root        string
	group       string
	kind        string
	environment string
}

// groupRecords は、workflow の apply の job と lock の解除の job と lock を残す job を、workflow をまたぐ照合のために
// 取り出します。
func groupRecords(file, source string) []groupRecord {
	split := workflow.SplitJobs(source)
	var records []groupRecord
	for _, job := range split.Jobs {
		if !assumesRole(job) {
			continue
		}
		kind, entry := cientry.Apply, applyEntry
		switch {
		case isUnlock(job):
			kind, entry = cientry.Unlock, unlockEntry
		case isHold(job):
			kind, entry = cientry.Hold, holdEntry
		}
		root := entryRoot(job, entry)
		if root == "" {
			continue
		}
		env, _ := environment(job)
		records = append(records, groupRecord{file: file, job: job, root: root, group: effectiveGroup(job, split.Preamble), kind: kind, environment: env})
	}
	return records
}

// entryRoot は、job が走らせる最初の CI の入口から、re が取り出す `<env>-<root>` を返します。無ければ空文字を返す。
func entryRoot(job workflow.Job, re *regexp.Regexp) string {
	for _, l := range job.Lines {
		if commentLine.MatchString(l.Text) || nameLine.MatchString(l.Text) {
			continue
		}
		if m := re.FindStringSubmatch(l.Text); m != nil {
			return m[1]
		}
	}
	return ""
}

// checkUnlockIsolation は、lock の解除の job の concurrency の group が、同じ root の apply の job の group と
// 字面で違うことを検査します。GitHub の concurrency の group はリポジトリの中で workflow をまたいで共有され、
// 同じ group では lock の解除の起動が、待機中の apply の run を取り消す。
func checkUnlockIsolation(records []groupRecord) []lintreport.Finding {
	var findings []lintreport.Finding
	for _, u := range records {
		if u.kind != cientry.Unlock {
			continue
		}
		if problem := isolationProblem(u, records); problem != "" {
			findings = append(findings, lintreport.Finding{File: u.file, Line: u.job.Number, Message: fmt.Sprintf("job %s: %s", u.job.ID, problem)})
		}
	}
	return findings
}

// isolationProblem は、lock の解除の job u の group が apply の job の group と違うと確かめられない理由を返します。
// 確かめられれば空文字を返す。比べる相手は、u の入口と同じ `<env>-<root>` の apply の入口を走らせる job に限り、
// ちょうど1つを要る。
func isolationProblem(u groupRecord, records []groupRecord) string {
	if !isLiteral(u.group) {
		return "lock の解除の job の concurrency の group を字面で書いてください。apply の job の group と違うことを確かめられません"
	}
	var applies []groupRecord
	for _, a := range records {
		if a.kind == cientry.Apply && a.root == u.root {
			applies = append(applies, a)
		}
	}
	if len(applies) != 1 {
		return fmt.Sprintf("make ci-apply-%s を走らせる apply の job が、ちょうど1つではありません（%d 件）。concurrency の group を比べる相手が決まりません", u.root, len(applies))
	}
	a := applies[0]
	if !isLiteral(a.group) {
		return fmt.Sprintf("apply の job %s（%s）の concurrency の group を字面で読めません。lock の解除の job の group と違うことを確かめられません", a.job.ID, a.file)
	}
	if a.group == u.group {
		return fmt.Sprintf("concurrency の group %q が、apply の job %s（%s）の group と同じです。lock の解除の起動が、待機中の apply の run を取り消します", u.group, a.job.ID, a.file)
	}
	return ""
}

// checkHoldPairing は、lock を残す job の concurrency の group と environment が、同じ root の apply の job のものと
// 字面で一致することを検査します。group が違えば apply と並んで走って lock を奪い合い、environment が違えば apply と
// 違う role の信頼条件で lock を取る。
func checkHoldPairing(records []groupRecord) []lintreport.Finding {
	var findings []lintreport.Finding
	for _, h := range records {
		if h.kind != cientry.Hold {
			continue
		}
		if problem := holdProblem(h, records); problem != "" {
			findings = append(findings, lintreport.Finding{File: h.file, Line: h.job.Number, Message: fmt.Sprintf("job %s: %s", h.job.ID, problem)})
		}
	}
	return findings
}

// holdProblem は、lock を残す job h の group と environment が apply の job のものと一致すると確かめられない理由を
// 返します。確かめられれば空文字を返す。比べる相手は、h の入口と同じ `<env>-<root>` の apply の入口を走らせる job に
// 限り、ちょうど1つを要る。
func holdProblem(h groupRecord, records []groupRecord) string {
	if !isLiteral(h.group) {
		return "lock を残す job の concurrency の group を字面で書いてください。apply の job の group と同じことを確かめられません"
	}
	var applies []groupRecord
	for _, a := range records {
		if a.kind == cientry.Apply && a.root == h.root {
			applies = append(applies, a)
		}
	}
	if len(applies) != 1 {
		return fmt.Sprintf("make ci-apply-%s を走らせる apply の job が、ちょうど1つではありません（%d 件）。concurrency の group と environment を比べる相手が決まりません", h.root, len(applies))
	}
	a := applies[0]
	if !isLiteral(a.group) {
		return fmt.Sprintf("apply の job %s（%s）の concurrency の group を字面で読めません。lock を残す job の group と同じことを確かめられません", a.job.ID, a.file)
	}
	if a.group != h.group {
		return fmt.Sprintf("concurrency の group %q が、apply の job %s（%s）の group %q と違います。apply と並んで走り、lock を奪い合います", h.group, a.job.ID, a.file, a.group)
	}
	if a.environment == "" || a.environment != h.environment {
		return fmt.Sprintf("environment %q が、apply の job %s（%s）の environment %q と違います。lock は apply と同じ role で取ります", h.environment, a.job.ID, a.file, a.environment)
	}
	return ""
}

// isLiteral は、実効の group が字面で読めた値かを返します。空文字は読めなかった group で、式は実行時まで値が決まらない。
func isLiteral(group string) bool {
	return group != "" && !strings.Contains(group, "${{")
}

// effectiveGroup は、job の concurrency が在ればその group を、無ければ workflow 全体の group を返します。
func effectiveGroup(job workflow.Job, preamble []workflow.Line) string {
	if hasConcurrency(job.Lines, "    ") {
		return concurrencyGroup(job.Lines, "    ")
	}
	return concurrencyGroup(preamble, "")
}

// concurrencyGroup は、indent の桁の concurrency の group を、引用符を外して返します。1行に書いた形は値そのものを返す。
// 読めなければ空文字を返す。
func concurrencyGroup(lines []workflow.Line, indent string) string {
	for i, l := range lines {
		if m := concurrencyIs.FindStringSubmatch(l.Text); m != nil && m[1] == indent {
			return literalGroup(m[2])
		}
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
			if m := groupAt.FindStringSubmatch(next.Text); m != nil && m[1] == indent+"  " {
				return literalGroup(m[2])
			}
		}
	}
	return ""
}

// literalGroup は、1行に書いた group の値から引用符を外して返します。ブロックスカラー（| や >）とフロー形式は
// 値が次の行か括弧の中にあって読めないので、空文字を返す。
func literalGroup(value string) string {
	if blockScalar.MatchString(value) || strings.HasPrefix(value, "{") {
		return ""
	}
	return strings.Trim(value, `"'`)
}

func isUnlock(job workflow.Job) bool {
	return hasLine(job, unlockEntry)
}

func isHold(job workflow.Job) bool {
	return hasLine(job, holdEntry)
}

func hasLine(job workflow.Job, re *regexp.Regexp) bool {
	return slices.ContainsFunc(job.Lines, func(l workflow.Line) bool {
		return !commentLine.MatchString(l.Text) && !nameLine.MatchString(l.Text) && re.MatchString(l.Text)
	})
}

// gate は、資格情報を取る job が needs に置ける、資格情報を持たない検査の job の種類です。job は名前ではなく、
// 走らせる make のターゲット（marks）で見分けます。runs はその job に置ける run: の閉じた集合、testRun は
// 自己テストの run: で、check は違反の文に出す検査の make です。
type gate struct {
	noun, label, targets, check string
	marks, runs, testRun        *regexp.Regexp
}

var (
	detectionGate = gate{noun: "検出", label: "検出の job", targets: "make infra-code-gate-test と make infra-code-gate", check: "make infra-code-gate", marks: detection, runs: detectionRun}
	orderGate     = gate{noun: "順序の検査", label: "順序の検査の job", targets: "make resource-order-check-test と make resource-order-check", check: "make resource-order-check", marks: orderCheck, runs: orderRun, testRun: orderTestRun}
	layerGate     = gate{noun: "層の検査", label: "層の検査の job", targets: "make layer-check-test と make layer-check", check: "make layer-check", marks: layerCheck, runs: layerRun, testRun: layerTestRun}
	rootGate      = gate{noun: "root の検査", label: "root の検査の job", targets: "make root-check-test と make root-check", check: "make root-check", marks: rootCheck, runs: rootRun, testRun: rootTestRun}
	// applyGates は、apply の job が検出に加えて必ず待ち、Pull Request の必須検査も走らせる検査である。
	applyGates = []gate{orderGate, layerGate, rootGate}
)

// checkDetection は、job が同じ workflow の検出の job の成功を待つことを検査します（ADR-0601 決定9）。
// needs に置けるのは検出の job と applyGates の検査の job で、どれも同じ形の規則で見ます。workload root の apply の
// job に限り、同じ env の identity root の apply の job も置ける（upstreamApply）。job の if: は、status の関数で
// 失敗を越えないよう、検出の成功と、needs のすべての検査と identity root の apply の成功を含むときだけ認めます。
func checkDetection(job workflow.Job, jobs []workflow.Job, finding func(int, string, ...any) lintreport.Finding) []lintreport.Finding {
	type gated struct {
		job  workflow.Job
		gate gate
	}
	var detectors, upstream []workflow.Job
	var checks []gated
	var findings []lintreport.Finding
	for _, n := range needs(job) {
		i := slices.IndexFunc(jobs, func(j workflow.Job) bool { return j.ID == n })
		if i >= 0 && runsGate(jobs[i], detectionGate) {
			detectors = append(detectors, jobs[i])
			continue
		}
		if i >= 0 && upstreamApply(job, jobs[i]) {
			upstream = append(upstream, jobs[i])
			continue
		}
		k := slices.IndexFunc(applyGates, func(g gate) bool { return i >= 0 && runsGate(jobs[i], g) })
		if k < 0 {
			findings = append(findings, finding(job.Number, "needs の %s は、%s ではありません。資格情報を取る job が待つ job は、これらの形に限ります", n, allowedNeeds()))
			continue
		}
		checks = append(checks, gated{jobs[i], applyGates[k]})
	}
	if len(detectors) == 0 {
		return append(findings, finding(job.Number, "needs に、make infra-code-gate を走らせる job を同じ workflow の中に置いてください。資格情報を取る job は検出より先にコードを実行しません（ADR-0601 決定9）"))
	}
	for _, d := range detectors {
		findings = append(findings, checkGateJob(d, detectionGate, finding)...)
	}
	for _, c := range checks {
		findings = append(findings, checkGateJob(c.job, c.gate, finding)...)
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
	for _, c := range checks {
		if !requiresSuccess(cond, c.job.ID) {
			findings = append(findings, finding(at, "job の if: に needs.%s.result == 'success' を含めてください", c.job.ID))
		}
	}
	for _, u := range upstream {
		if !requiresSuccess(cond, u.ID) {
			findings = append(findings, finding(at, "job の if: に needs.%s.result == 'success' を含めてください", u.ID))
		}
	}
	return findings
}

// upstreamApply は、job が workload root の apply の job で、dep が同じ env の identity root の apply の job かを返します。
// 待たれる側は資格情報を取り、入口を run: の値そのものとして持つ job に限る。字面に入口を含むだけの job は待てない。
func upstreamApply(job, dep workflow.Job) bool {
	if isUnlock(job) || isHold(job) || isUnlock(dep) || isHold(dep) || !hasLine(dep, credentials) {
		return false
	}
	env, ok := strings.CutSuffix(entryRoot(job, ciApply), "-workload")
	return ok && env != "" && entryRoot(dep, ciApply) == env+"-identity"
}

// checkUpstream は、workload root の apply の job が、同じ workflow の同じ env の identity root の apply の job を
// needs に持つことを検査します。待たないと、identity root の apply が書く権限を使う前に走る。
func checkUpstream(job workflow.Job, jobs []workflow.Job, finding func(int, string, ...any) lintreport.Finding) []lintreport.Finding {
	var findings []lintreport.Finding
	for _, dep := range jobs {
		if dep.ID != job.ID && upstreamApply(job, dep) && !slices.Contains(needs(job), dep.ID) {
			findings = append(findings, finding(job.Number, "workload root の apply の job の needs に、同じ env の identity root の apply の job %s を置いてください。identity root の apply が書く権限を使う前に走ります", dep.ID))
		}
	}
	return findings
}

// allowedNeeds は、資格情報を取る job の needs に置ける job の形を、違反の文のために並べます。
func allowedNeeds() string {
	parts := make([]string, 0, 2+len(applyGates))
	parts = append(parts, fmt.Sprintf("%s（%s）", detectionGate.label, detectionGate.check))
	for _, g := range applyGates {
		parts = append(parts, fmt.Sprintf("%s（%s）", g.label, g.check))
	}
	parts = append(parts, "workload root の apply の job に限り同じ env の identity root の apply の job（make ci-apply-<env>-identity）")
	return strings.Join(parts, "・")
}

// checkGateJob は、needs の先の検査の job が skip も失敗の無視もできず、決めたターゲットだけを走らせることを検査します。
func checkGateJob(d workflow.Job, g gate, finding func(int, string, ...any) lintreport.Finding) []lintreport.Finding {
	var findings []lintreport.Finding
	// 依存先が skip すると検査の job も skip され、後続は走らないまま失敗も報告されない。
	if _, at, ok := jobKey(d, "needs"); ok {
		findings = append(findings, finding(at, "%s %s に needs: を置かないでください", g.label, d.ID))
	}
	for _, l := range d.Lines {
		if commentLine.MatchString(l.Text) {
			continue
		}
		if skipsOrHides.MatchString(l.Text) {
			findings = append(findings, finding(l.Number, "%s %s に if:・continue-on-error:・working-directory:・defaults:・env:・shell:・container:・services: を置かないでください。skip も失敗の無視も別の場所や別の shell での実行も、成功と同じく後続を止めません", g.label, d.ID))
		}
		if makeFlags.MatchString(l.Text) {
			findings = append(findings, finding(l.Number, "%s %s で MAKEFLAGS と MAKEFILES を渡さないでください。-n や -i は何も検査せずに成功します", g.label, d.ID))
		}
		findings = append(findings, checkIndirect(l, finding)...)
	}
	for _, step := range workflow.SplitSteps(d) {
		if runs, ok := stepRunLine(step); ok && !g.runs.MatchString(runs.Text) {
			findings = append(findings, finding(runs.Number, "%s %s の run: は %s だけにしてください。ほかの run: は後続の step の PATH と環境を替えます", g.label, d.ID, g.targets))
		}
	}
	return findings
}

// checkApplyGate は、apply の job が g の検査の job を needs に持つことを検査します。
func checkApplyGate(job workflow.Job, jobs []workflow.Job, g gate, finding func(int, string, ...any) lintreport.Finding) []lintreport.Finding {
	for _, n := range needs(job) {
		if slices.ContainsFunc(jobs, func(j workflow.Job) bool { return j.ID == n && !runsGate(j, detectionGate) && runsGate(j, g) }) {
			return nil
		}
	}
	return []lintreport.Finding{finding(job.Number, "apply の job の needs に、%s を走らせる job を同じ workflow の中に置いてください。Pull Request の検査は merge された木を見ません", g.check)}
}

// checkHidden は、入口の make を実行しないか、失敗を成功として報告させる形を検査します（plan の job は
// required check、apply の job は通知の起動条件である）。run: は字面からは中身を選べないので、置ける step を
// 閉じた集合に限る。各形を拒む理由は、その違反の文が持つ。
func checkHidden(job workflow.Job, cred workflow.Line, refusal string, w workflowInfo, finding func(int, string, ...any) lintreport.Finding) []lintreport.Finding {
	var findings []lintreport.Finding
	for _, l := range append(slices.Clone(w.preamble), job.Lines...) {
		if commentLine.MatchString(l.Text) {
			continue
		}
		if makeFlags.MatchString(l.Text) {
			findings = append(findings, finding(l.Number, "MAKEFLAGS と MAKEFILES を渡さないでください。-n や -i、先に読む makefile は何も実行せずに成功させます"))
		}
		if pathKey.MatchString(l.Text) {
			findings = append(findings, finding(l.Number, "PATH を替えないでください。make と terraform を別のものに差し替えます"))
		}
		findings = append(findings, checkIndirect(l, finding)...)
	}
	for _, l := range w.preamble {
		if defaultsAt.MatchString(l.Text) || envAt.MatchString(l.Text) {
			findings = append(findings, finding(l.Number, "資格情報を取る job を持つ workflow に defaults: と env: を置かないでください。run: の shell・場所・道具を替えます"))
		}
	}
	for _, key := range []string{"continue-on-error", "container", "services", "defaults", "env"} {
		if _, at, ok := jobKey(job, key); ok {
			findings = append(findings, finding(at, "資格情報を取る job に %s: を置かないでください", key))
		}
	}
	for _, step := range workflow.SplitSteps(job) {
		findings = append(findings, checkStepEnv(step, finding)...)
		runs, ok := stepRunLine(step)
		if !ok {
			continue
		}
		entry, credStep := ciEntry.MatchString(runs.Text), credentials.MatchString(runs.Text)
		// 拒否の確認は plan の job でだけ入口と同じに扱う。形と位置は checkDenied が見る。
		if deniedLine.MatchString(runs.Text) {
			if refusal != refuseUntrusted {
				findings = append(findings, finding(runs.Number, "make ci-assume-denied は plan の job にだけ置いてください"))
			}
			entry = deniedRun.MatchString(runs.Text)
		}
		// 拒否の step と認めるのは、役割の id を持ち資格情報より前に在るものだけ。後ろの run: は資格情報を読める。
		refuses := hasStepID(step, refusal) && step.Number < cred.Number
		if !entry && !credStep && !refuses {
			findings = append(findings, finding(runs.Number, "資格情報を取る job の run: は、資格情報より前の拒否の step・make ci-aws-credentials・CI の入口・plan の job の make ci-assume-denied だけにしてください。ほかの run: は後続の step の PATH と環境を替えるか、資格情報を読みます"))
		}
		for _, l := range step.Lines {
			switch {
			case (entry || credStep) && (stepIf.MatchString(l.Text) || stepContinue.MatchString(l.Text) || stepShell.MatchString(l.Text) || stepWorkDir.MatchString(l.Text)):
				findings = append(findings, finding(l.Number, "資格情報を取る step と CI の入口の step に if:・continue-on-error:・shell:・working-directory: を置かないでください。skip も失敗も成功として報告され、別の Makefile を走らせます"))
			case !entry && !credStep && (stepShell.MatchString(l.Text) || stepWorkDir.MatchString(l.Text)):
				// 拒否の step の if: と continue-on-error: は checkRefusal が見る。
				findings = append(findings, finding(l.Number, "拒否の step に shell:・working-directory: を置かないでください。拒否を実行させずに通します"))
			}
		}
	}
	return findings
}

// checkIndirect は、step の外から PATH と環境を替える書き込みと、別の場所の値を持ち込むアンカーを検査します。
func checkIndirect(l workflow.Line, finding func(int, string, ...any) lintreport.Finding) []lintreport.Finding {
	var findings []lintreport.Finding
	if stepFiles.MatchString(l.Text) {
		findings = append(findings, finding(l.Number, "GITHUB_PATH と GITHUB_ENV に書かないでください。後続の step の PATH と環境を替えます"))
	}
	if yamlAlias.MatchString(l.Text) {
		findings = append(findings, finding(l.Number, "YAML のアンカーとエイリアスを使わないでください。持ち込んだ値を読めません"))
	}
	return findings
}

// checkStepEnv は、step の env: が stepEnvKeys の名前だけを、ブロック形式で持つことを検査します。名前の桁は
// 最初の名前の行で決め、それより深い行はブロックスカラーの値の続きとしてだけ認める。
func checkStepEnv(step workflow.Step, finding func(int, string, ...any) lintreport.Finding) []lintreport.Finding {
	var findings []lintreport.Finding
	in, keyIndent, inValue := false, 0, false
	for _, l := range step.Lines {
		if commentLine.MatchString(l.Text) || strings.TrimSpace(l.Text) == "" {
			continue
		}
		if m := stepEnv.FindStringSubmatch(l.Text); m != nil {
			in, keyIndent, inValue = true, 0, false
			if m[1] != "" && !strings.HasPrefix(m[1], "#") {
				findings = append(findings, finding(l.Number, "step の env: はブロック形式で書いてください。名前を読めません"))
			}
			continue
		}
		indent := len(l.Text) - len(strings.TrimLeft(l.Text, " "))
		if !in || indent <= 8 {
			in = false
			continue
		}
		if keyIndent == 0 {
			keyIndent = indent
		}
		if indent > keyIndent && inValue {
			continue
		}
		m := stepEnvKey.FindStringSubmatch(l.Text)
		if indent != keyIndent || m == nil {
			findings = append(findings, finding(l.Number, "step の env: の名前を読めません。名前は同じ桁に、値の続きはブロックスカラー（| か >）で書いてください"))
			continue
		}
		inValue = blockScalar.MatchString(m[2])
		if !slices.Contains(stepEnvKeys, m[1]) {
			findings = append(findings, finding(l.Number, "資格情報を取る job の step の env: に置けるのは %s だけです。ほかの名前は道具やシェルを替えます", strings.Join(stepEnvKeys, "・")))
		}
	}
	return findings
}

func stepRunLine(step workflow.Step) (workflow.Line, bool) {
	for _, l := range step.Lines {
		if stepRun.MatchString(l.Text) {
			return l, true
		}
	}
	return workflow.Line{}, false
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
func checkTerraform(job workflow.Job, kind string, finding func(int, string, ...any) lintreport.Finding) []lintreport.Finding {
	var findings []lintreport.Finding
	entries := 0
	for _, l := range job.Lines {
		if commentLine.MatchString(l.Text) || nameLine.MatchString(l.Text) || installsTF.MatchString(l.Text) {
			continue
		}
		matched := isKindEntry(kind, l.Text)
		if matched {
			entries++
		}
		if runsTerraform.MatchString(l.Text) || (!matched && ciEntry.MatchString(l.Text)) {
			findings = append(findings, finding(l.Number, "%s の job は terraform を make ci-%s-* でだけ走らせてください", kind, kind))
		}
	}
	if entries != 1 {
		findings = append(findings, finding(job.Number, "%s の job は make ci-%s-* を1回だけ走らせてください（%d 回）", kind, kind, entries))
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

// makeRun は、step の run: の値が `make`、target の1つ、前後どちらかの RUNNER_MODE=host だけで成る行に当たる正規表現を返します。
func makeRun(target string) *regexp.Regexp {
	const mode = `(?:\s+RUNNER_MODE=host)?`
	return regexp.MustCompile(`^ {6}(?:- | {2})["']?run["']?[ \t]*:[ \t]*make` + mode + `\s+` + target + mode + `[ \t]*$`)
}

// requiresSuccess は、if: の式が && で結んだ項の1つとして、そのまま needs.<id>.result == 'success' を持つかを返します。
// 括弧で包んだ比較は、別の比較に使われうるので認めません。
func requiresSuccess(cond, id string) bool {
	cond = strings.TrimSpace(strings.TrimLeft(cond, ">|-+ "))
	cond = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(cond, "${{"), "}}"))
	term := regexp.MustCompile(`^needs\.` + regexp.QuoteMeta(id) + `\.result\s*==\s*'success'$`)
	return slices.ContainsFunc(strings.Split(cond, "&&"), func(t string) bool { return term.MatchString(strings.TrimSpace(t)) })
}

// runsGate は、job が g の検査そのもの（自己テストではない方のターゲット）を走らせるかを返します。
func runsGate(job workflow.Job, g gate) bool {
	return slices.ContainsFunc(job.Lines, func(l workflow.Line) bool { return g.marks.MatchString(l.Text) })
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

var (
	revertResults    = regexp.MustCompile(`([a-z0-9-]+)=\$\{\{ needs\.([a-z0-9-]+)\.result \}\}:\$\{\{ needs\.([a-z0-9-]+)\.outputs\.apply-outcome \}\}:\$\{\{ needs\.([a-z0-9-]+)\.outputs\.failure \}\}`)
	revertWorkflow   = regexp.MustCompile(`^\s+APPLY_REVERT_WORKFLOW:[ \t]*(\S+)[ \t]*(?:#.*)?$`)
	revertResultsKey = regexp.MustCompile(`^(\s+)APPLY_REVERT_RESULTS:`)
	jobIf            = regexp.MustCompile(`^ {4}if:(.*)$`)
	jobName          = regexp.MustCompile(`^ {4}name:`)
)

// checkRevertWiring は、make ci-apply-* を走らせる job を持つ workflow で、revert-plan と通知が、その
// workflow の apply の job のすべてを過不足なく名指していることを検査します（README.md の「apply の失敗の
// revert」）。apply の job を持つかを返す。
func checkRevertWiring(file, source string) (bool, []lintreport.Finding) {
	split := workflow.SplitJobs(source)
	byID := map[string]workflow.Job{}
	var apply []string
	for _, job := range split.Jobs {
		byID[job.ID] = job
		if hasLine(job, ciApply) {
			apply = append(apply, job.ID)
		}
	}
	if len(apply) == 0 {
		return false, nil
	}
	finding := func(line int, format string, args ...any) lintreport.Finding {
		return lintreport.Finding{File: file, Line: line, Message: fmt.Sprintf(format, args...)}
	}
	var findings []lintreport.Finding
	slices.Sort(apply)

	rp, ok := byID["revert-plan"]
	if !ok {
		findings = append(findings, finding(1, "apply の job（%s）を持つ workflow に revert-plan の job がありません", strings.Join(apply, ", ")))
	} else {
		findings = append(findings, checkRevertPlan(rp, apply, filepath.Base(file), finding)...)
	}

	if nf, ok := byID["notify-failure"]; !ok {
		findings = append(findings, finding(1, "apply の job を持つ workflow に notify-failure の job がありません"))
	} else {
		for _, a := range apply {
			if !slices.Contains(needs(nf), a) {
				findings = append(findings, finding(nf.Number, "notify-failure の needs に %s がありません", a))
			}
		}
	}
	for _, a := range apply {
		// apply-revert は最後に成功した run の job を表示名で引くので、表示名を id と同じにしておく。
		if slices.ContainsFunc(byID[a].Lines, func(l workflow.Line) bool { return jobName.MatchString(l.Text) }) {
			findings = append(findings, finding(byID[a].Number, "apply の job %s に name: があります（apply-revert は job を表示名で引く）", a))
		}
		if !slices.ContainsFunc(split.Jobs, func(j workflow.Job) bool { return notifiesSuccess(j, a) }) {
			findings = append(findings, finding(byID[a].Number, "%s の成功を通知する job（needs に置き、if: が needs.%s.result == 'success' を含む）がありません", a, a))
		}
	}
	return true, findings
}

func checkRevertPlan(rp workflow.Job, apply []string, base string, finding func(int, string, ...any) lintreport.Finding) []lintreport.Finding {
	var findings []lintreport.Finding
	got := slices.Clone(needs(rp))
	slices.Sort(got)
	if !slices.Equal(got, apply) {
		findings = append(findings, finding(rp.Number, "revert-plan の needs が apply の job（%s）と一致しません（%s）", strings.Join(apply, ", "), strings.Join(got, ", ")))
	}
	cond := ""
	for _, l := range rp.Lines {
		if m := jobIf.FindStringSubmatch(l.Text); m != nil {
			cond = m[1]
		}
	}
	for _, a := range apply {
		if !strings.Contains(cond, "needs."+a+".result == 'failure'") {
			findings = append(findings, finding(rp.Number, "revert-plan の if: に needs.%s.result == 'failure' がありません", a))
		}
	}

	var passed []string
	workflowName := ""
	for i, l := range rp.Lines {
		if m := revertWorkflow.FindStringSubmatch(l.Text); m != nil {
			workflowName = strings.Trim(m[1], `"'`)
		}
		m := revertResultsKey.FindStringSubmatch(l.Text)
		if m == nil {
			continue
		}
		value := []string{l.Text}
		for _, next := range rp.Lines[i+1:] {
			if strings.TrimSpace(next.Text) != "" && !strings.HasPrefix(next.Text, m[1]+" ") {
				break
			}
			value = append(value, next.Text)
		}
		for _, r := range revertResults.FindAllStringSubmatch(strings.Join(value, "\n"), -1) {
			if r[1] != r[2] || r[1] != r[3] || r[1] != r[4] {
				findings = append(findings, finding(l.Number, "APPLY_REVERT_RESULTS の %s が、別の job の結果を参照しています", r[1]))
			}
			passed = append(passed, r[1])
		}
	}
	slices.Sort(passed)
	if !slices.Equal(passed, apply) {
		findings = append(findings, finding(rp.Number, "APPLY_REVERT_RESULTS が渡す job（%s）が apply の job（%s）と一致しません", strings.Join(passed, ", "), strings.Join(apply, ", ")))
	}
	if workflowName != base {
		findings = append(findings, finding(rp.Number, "APPLY_REVERT_WORKFLOW が %q で、この workflow のファイル名 %q ではありません", workflowName, base))
	}
	return findings
}

func notifiesSuccess(j workflow.Job, apply string) bool {
	if !slices.Contains(needs(j), apply) {
		return false
	}
	return slices.ContainsFunc(j.Lines, func(l workflow.Line) bool {
		m := jobIf.FindStringSubmatch(l.Text)
		return m != nil && strings.Contains(m[1], "needs."+apply+".result == 'success'")
	})
}

// checkRootCoverage は、rootsDir の下で *.tf を持つディレクトリが ADR-0604 決定5 の配置（<env>/ と <env>/identity/）
// にだけ在り、各 root に、Pull Request で起動する workflow の plan の job が1つ以上、apply の job と lock の解除の job と
// lock を残す job がちょうど1つずつあることを検査します。数えるのは対象の job（assumesRole）だけである。見た root の数を返し、0件なら errNoTarget にする。
func checkRootCoverage(rootsDir string, sources map[string]string) (int, []lintreport.Finding, error) {
	var findings []lintreport.Finding
	var roots []string
	err := filepath.WalkDir(rootsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			findings = append(findings, lintreport.Finding{File: path, Line: 1, Message: "root を置くディレクトリの symlink は読みません"})
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		// .terraform は init が作る module と provider の写しで、root ではない。
		if path != rootsDir && d.Name() == ".terraform" {
			return filepath.SkipDir
		}
		tf, err := filepath.Glob(filepath.Join(path, "*.tf"))
		if err != nil || len(tf) == 0 {
			return err
		}
		rel, err := filepath.Rel(rootsDir, path)
		if err != nil {
			return err
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		switch {
		case len(parts) == 1 && parts[0] != ".":
			roots = append(roots, parts[0]+"-workload")
		case len(parts) == 2 && parts[1] == "identity":
			roots = append(roots, parts[0]+"-identity")
		default:
			findings = append(findings, lintreport.Finding{File: path, Line: 1,
				Message: fmt.Sprintf("root %s が ADR-0604 決定5 の配置（<env>/ と <env>/identity/）の外にあります", filepath.ToSlash(rel))})
		}
		return nil
	})
	if err != nil {
		return 0, nil, xerrors.Wrapf(err, "%s の読み取り", rootsDir)
	}
	if len(roots) == 0 && len(findings) == 0 {
		return 0, nil, xerrors.Wrapf(errNoTarget, "%s に root がありません", rootsDir)
	}

	files := slices.Sorted(maps.Keys(sources))
	for _, root := range roots {
		count := func(kind string, pullRequestOnly bool) int {
			re := cientry.MustPattern(`\bmake\b.*\bci-`, `-`+regexp.QuoteMeta(root)+`(?:\s|$)`, kindOf[kind])
			n := 0
			for _, f := range files {
				split := workflow.SplitJobs(sources[f])
				if pullRequestOnly {
					if on, _ := triggers(split.Preamble); !slices.Contains(triggerNames(on), "pull_request") {
						continue
					}
				}
				for _, job := range split.Jobs {
					if assumesRole(job) && hasLine(job, re) {
						n++
					}
				}
			}
			return n
		}
		if count(cientry.Plan, true) == 0 {
			findings = append(findings, lintreport.Finding{File: rootsDir, Line: 1, Message: fmt.Sprintf("root %s を Pull Request で plan する job がありません", root)})
		}
		if n := count(cientry.Apply, false); n != 1 {
			findings = append(findings, lintreport.Finding{File: rootsDir, Line: 1, Message: fmt.Sprintf("root %s を apply する job が %d 件です（1件を期待）", root, n)})
		}
		if n := count(cientry.Unlock, false); n != 1 {
			findings = append(findings, lintreport.Finding{File: rootsDir, Line: 1, Message: fmt.Sprintf("root %s の lock を外す job が %d 件です（1件を期待）", root, n)})
		}
		if n := count(cientry.Hold, false); n != 1 {
			findings = append(findings, lintreport.Finding{File: rootsDir, Line: 1, Message: fmt.Sprintf("root %s の lock を残す job が %d 件です（1件を期待）", root, n)})
		}
	}
	return len(roots), findings, nil
}
