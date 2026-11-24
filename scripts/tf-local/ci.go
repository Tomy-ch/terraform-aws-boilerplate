package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/cientry"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/policyresult"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// このファイルのサブコマンドは、エラーの文が公開されるログに出るので、入力の値（アカウント ID、token、
// Bucket の key）を入れない。

// roleFormat は、usecases/deployment-identity の role 名 `<project>-<env>-ci-<役割>` の写し。
const roleFormat = "arn:aws:iam::%s:role/%s-%s-ci-%s"

// oidcAudience は、usecases/deployment-identity の OIDC provider の client_id_list の写し。
const oidcAudience = "sts.amazonaws.com"

// oidcHostSuffix は、GitHub Actions が ACTIONS_ID_TOKEN_REQUEST_URL に置く要求先の host。
const oidcHostSuffix = ".actions.githubusercontent.com"

const maxTokenSize = 16 << 10

var (
	subPattern     = regexp.MustCompile(`^repo:[^\s:]+:environment:[A-Za-z0-9_.-]+$`)
	counterPattern = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
	branchPattern  = regexp.MustCompile(`^[A-Za-z0-9_.-]+(/[A-Za-z0-9_.-]+)*$`)
	rootPattern    = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	// assumeErrorPattern は、aws CLI が API のエラーを stderr に書く行。行頭の接頭辞は CLI の版で変わるので見ない。
	assumeErrorPattern = regexp.MustCompile(`An error occurred \(([A-Za-z0-9]{1,64})\) when calling the AssumeRoleWithWebIdentity operation`)

	errRole         = xerrors.New("❌ role の ARN を組めません（project・env・役割・アカウント ID のいずれかが不正です）")
	errOIDCToken    = xerrors.New("❌ OIDC token から sub を読めません")
	errOIDCRequest  = xerrors.New("❌ OIDC token を要求できません（job に id-token: write がありません）")
	errOIDCResponse = xerrors.New("❌ OIDC token の要求の応答から token を読めません")
	errPlanJSON     = xerrors.New("❌ plan の JSON を解釈できません")
	errPlanEmpty    = xerrors.New("❌ plan の JSON に resource の変更が1件もありません。検査の対象が0件です")
	errPlanErrored  = xerrors.New("❌ plan はエラーで終わっています")
	errPlanFailed   = xerrors.New("❌ plan が失敗しました（終了コード 1）")
	errPlanExitCode = xerrors.New("❌ plan の終了コードは 0 か 2 です")
	errPlanMismatch = xerrors.New("❌ plan の終了コードと JSON の変更の有無が食い違います")
	errPlanKey      = xerrors.New("❌ plan の出力の key を組めません")
	errApplyKey     = xerrors.New("❌ apply の出力の key を組めません")
	errApplyFailed  = xerrors.New("❌ apply が失敗しました")
	errApplyTimeout = xerrors.New("❌ apply を打ち切りました（timeout）")
	errFailureKind  = xerrors.New("❌ 失敗の種類を決められません")
	errDigest       = xerrors.New("❌ SHA-256 を計算するファイルを読めません")

	errAssumeSucceeded  = xerrors.New("❌ 拒否されるべき role の assume が通りました")
	errAssumeNotDenied  = xerrors.New("❌ role の assume が、AccessDenied ではない理由で失敗しました")
	errAssumeUnreadable = xerrors.New("❌ assume の失敗から、STS のエラーを1つに読めません")

	errPolicyJSON  = policyresult.ErrJSON
	errPolicyEmpty = policyresult.ErrEmpty
	errPolicyCount = policyresult.ErrCount
	errPolicyRules = policyresult.ErrRules
)

func roleARN(project, env, role, account string) (string, error) {
	if !tokenPattern.MatchString(project) || !tokenPattern.MatchString(env) ||
		!slices.Contains(cientry.Roles(), role) || !accountPattern.MatchString(account) {
		return "", errRole
	}

	return fmt.Sprintf(roleFormat, account, project, env, role), nil
}

// assumeDenied は、失敗した `aws sts assume-role-with-web-identity` の stderr から、拒否が AccessDenied で
// あることを確かめます。接続の失敗や token の不正は拒否ではないので、エラーの行がちょうど1つで、その Code が
// AccessDenied のときだけ成功します。stderr の全文はアカウント ID を含みうるので、文には Code だけを入れる。
func assumeDenied(role string, stderr []byte) (string, error) {
	matches := assumeErrorPattern.FindAllSubmatch(stderr, -1)
	if len(matches) != 1 {
		return "", xerrors.Wrapf(errAssumeUnreadable, "%s の role", role)
	}

	if code := string(matches[0][1]); code != "AccessDenied" {
		return "", xerrors.Wrapf(errAssumeNotDenied, "%s の role（%s）", role, code)
	}

	return fmt.Sprintf("✅ %s の role の assume は拒否されました（AccessDenied）", role), nil
}

// oidcURL は、GitHub Actions が渡す OIDC token の要求先に STS 向けの audience を付けて返します。
func oidcURL(environ []string) (string, error) {
	var request, bearer string

	for _, kv := range environ {
		if v, ok := strings.CutPrefix(kv, "ACTIONS_ID_TOKEN_REQUEST_URL="); ok {
			request = v
		}

		if v, ok := strings.CutPrefix(kv, "ACTIONS_ID_TOKEN_REQUEST_TOKEN="); ok {
			bearer = v
		}
	}

	if request == "" || bearer == "" {
		return "", errOIDCRequest
	}

	// bearer は audience を問わず token を発行させられるので、GitHub の要求先の外へ送らない。
	u, err := url.Parse(request)
	if err != nil || u.Scheme != "https" || u.User != nil || !strings.HasSuffix(u.Hostname(), oidcHostSuffix) || u.Hostname() == oidcHostSuffix {
		return "", xerrors.Wrap(errOIDCRequest, "要求先が GitHub Actions の https の URL ではありません")
	}

	q := u.Query()
	q.Set("audience", oidcAudience)
	u.RawQuery = q.Encode()

	return u.String(), nil
}

// oidcToken は、OIDC token の要求の応答 `{"value": "<JWT>"}` から token を返します。JWT の形でなければ失敗します。
func oidcToken(raw []byte) (string, error) {
	var resp struct {
		Value string `json:"value"`
	}

	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", xerrors.Wrap(errOIDCResponse, "応答が JSON ではありません")
	}

	parts := strings.Split(resp.Value, ".")
	if len(resp.Value) > maxTokenSize || len(parts) != 3 || slices.Contains(parts, "") || strings.ContainsAny(resp.Value, " \t\r\n") {
		return "", xerrors.Wrap(errOIDCResponse, "value が JWT の形ではありません")
	}

	return resp.Value, nil
}

// oidcSub は、GitHub Actions の OIDC token（JWT）の payload の sub を返します。署名は検証しない
// —— 値は公開ログへの表示用で、信頼の判断は AWS の STS が行う。
func oidcSub(raw string) (string, error) {
	token := strings.TrimSpace(raw)
	if token == "" || len(token) > maxTokenSize {
		return "", xerrors.Wrap(errOIDCToken, "token が空か、大きすぎます")
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", xerrors.Wrap(errOIDCToken, "JWT の形ではありません")
	}

	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return "", xerrors.Wrap(errOIDCToken, "payload が base64url ではありません")
	}

	var claims struct {
		Sub string `json:"sub"`
	}

	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", xerrors.Wrap(errOIDCToken, "payload が JSON ではありません")
	}

	if !subPattern.MatchString(claims.Sub) {
		return "", xerrors.Wrap(errOIDCToken, "sub が environment 形式ではありません")
	}

	return claims.Sub, nil
}

type planJSON struct {
	FormatVersion   string           `json:"format_version"`
	Errored         bool             `json:"errored"`
	ResourceChanges []resourceChange `json:"resource_changes"`
	OutputChanges   map[string]struct {
		Actions []string `json:"actions"`
	} `json:"output_changes"`
}

type resourceChange struct {
	Address         string `json:"address"`
	PreviousAddress string `json:"previous_address"`
	Change          struct {
		Actions   []string        `json:"actions"`
		Importing json.RawMessage `json:"importing"`
	} `json:"change"`
}

type planCounts struct {
	importing, add, change, destroy, read, forget, move, outputs int
}

func (c planCounts) changed() bool {
	return c != planCounts{}
}

func (c planCounts) String() string {
	if !c.changed() {
		return "No changes."
	}

	var b strings.Builder

	fmt.Fprintf(&b, "Plan: %d to import, %d to add, %d to change, %d to destroy.", c.importing, c.add, c.change, c.destroy)

	for _, extra := range []struct {
		n    int
		what string
	}{{c.read, "to read"}, {c.forget, "to forget"}, {c.move, "to move"}, {c.outputs, "output changes"}} {
		if extra.n > 0 {
			fmt.Fprintf(&b, " %d %s.", extra.n, extra.what)
		}
	}

	return b.String()
}

func runPlanSummary(args []string, readFile func(string) ([]byte, error)) (string, error) {
	fs := flag.NewFlagSet("plan-summary", flag.ContinueOnError)
	exitCode := fs.String("exitcode", "", "")

	if err := fs.Parse(args); err != nil {
		return "", xerrors.Wrap(errUsage, err.Error())
	}

	if fs.NArg() != 1 {
		return "", errUsage
	}

	// plan が失敗すれば show の JSON は無いので、ファイルより先に終了コードを見る。
	if *exitCode == "1" {
		return "", errPlanFailed
	}

	data, err := readFile(fs.Arg(0))
	if err != nil {
		return "", xerrors.Wrap(errPlanJSON, "ファイルを読めません")
	}

	return planSummary(*exitCode, data)
}

// planSummary は、`terraform plan -detailed-exitcode` の終了コードと `terraform show -json` の変更を
// 独立に数え、食い違えば失敗します。どちらか一方だけでは、plan の失敗と「変更なし」を取り違えうるためです。
func planSummary(exitCode string, data []byte) (string, error) {
	if exitCode != "0" && exitCode != "2" {
		return "", errPlanExitCode
	}

	var p planJSON
	if err := json.Unmarshal(data, &p); err != nil || p.FormatVersion == "" {
		return "", errPlanJSON
	}

	if p.Errored {
		return "", errPlanErrored
	}

	if len(p.ResourceChanges) == 0 {
		return "", errPlanEmpty
	}

	c, err := countChanges(p)
	if err != nil {
		return "", err
	}

	if c.changed() != (exitCode == "2") {
		return "", xerrors.Wrap(errPlanMismatch, "終了コード "+exitCode+"、JSON "+c.String())
	}

	return c.String(), nil
}

func countChanges(p planJSON) (planCounts, error) {
	var c planCounts

	for _, rc := range p.ResourceChanges {
		if len(rc.Change.Importing) > 0 && string(rc.Change.Importing) != "null" {
			c.importing++
		}

		if rc.PreviousAddress != "" && rc.PreviousAddress != rc.Address {
			c.move++
		}

		switch strings.Join(rc.Change.Actions, ",") {
		case "no-op":
		case "create":
			c.add++
		case "update":
			c.change++
		case "delete":
			c.destroy++
		case "delete,create", "create,delete":
			c.add++
			c.destroy++
		case "read":
			c.read++
		case "forget":
			c.forget++
		default:
			return planCounts{}, xerrors.Wrap(errPlanJSON, "知らない actions: "+strconv.Quote(strings.Join(rc.Change.Actions, ",")))
		}
	}

	for _, oc := range p.OutputChanges {
		if !slices.Equal(oc.Actions, []string{"no-op"}) {
			c.outputs++
		}
	}

	return c, nil
}

func runPlanKey(args []string) (string, error) {
	fs := flag.NewFlagSet("plan-key", flag.ContinueOnError)
	pr := fs.String("pr", "", "")
	branch := fs.String("branch", "", "")
	runID := fs.String("run", "", "")
	attempt := fs.String("attempt", "", "")
	root := fs.String("root", "", "")

	if err := fs.Parse(args); err != nil {
		return "", xerrors.Wrap(errUsage, err.Error())
	}

	if fs.NArg() > 0 {
		return "", errUsage
	}

	return planKey(*pr, *branch, *runID, *attempt, *root)
}

// planKey は、plan の出力を置く object の key を返します。Pull Request の plan は `pr/`、保護ブランチの
// plan は `branch/` の下に置く —— plan の role が書けるのはこの2つの接頭辞だけである
// （usecases/deployment-identity/internal/github-oidc/policies.tf）。
func planKey(pr, branch, runID, attempt, root string) (string, error) {
	if !counterPattern.MatchString(runID) || !counterPattern.MatchString(attempt) || !rootPattern.MatchString(root) {
		return "", xerrors.Wrap(errPlanKey, "run・attempt・root のいずれかが不正です")
	}

	name := fmt.Sprintf("%s-%s-%s.txt", runID, attempt, root)

	switch {
	case pr != "" && branch == "":
		if !counterPattern.MatchString(pr) {
			return "", xerrors.Wrap(errPlanKey, "Pull Request の番号が不正です")
		}

		return "pr/" + pr + "/" + name, nil
	case branch != "" && pr == "":
		if !branchPattern.MatchString(branch) || slices.Contains(strings.Split(branch, "/"), "..") || slices.Contains(strings.Split(branch, "/"), ".") {
			return "", xerrors.Wrap(errPlanKey, "ブランチ名が不正です")
		}

		return "branch/" + branch + "/" + name, nil
	default:
		return "", xerrors.Wrap(errPlanKey, "-pr と -branch のどちらか1つを渡してください")
	}
}

func runApplyKey(args []string) (string, error) {
	fs := flag.NewFlagSet("apply-key", flag.ContinueOnError)
	runID := fs.String("run", "", "")
	attempt := fs.String("attempt", "", "")
	root := fs.String("root", "", "")

	if err := fs.Parse(args); err != nil {
		return "", xerrors.Wrap(errUsage, err.Error())
	}

	if fs.NArg() > 0 {
		return "", errUsage
	}

	return applyKey(*runID, *attempt, *root)
}

// applyKey は、apply の出力を置く object の key を返します。apply-identity の role が書けるのは `apply/` の下だけで、
// plan の role はそこへ書けない（usecases/deployment-identity/internal/github-oidc/policies.tf）。
func applyKey(runID, attempt, root string) (string, error) {
	if !counterPattern.MatchString(runID) || !counterPattern.MatchString(attempt) || !rootPattern.MatchString(root) {
		return "", xerrors.Wrap(errApplyKey, "run・attempt・root のいずれかが不正です")
	}

	return fmt.Sprintf("apply/%s-%s-%s.txt", runID, attempt, root), nil
}

// applyTimedOut は、`timeout` が command を打ち切ったときに返す終了コード（GNU coreutils の timeout）。
const applyTimedOut = 124

func runApplyResult(args []string) (string, error) {
	fs := flag.NewFlagSet("apply-result", flag.ContinueOnError)
	exitCode := fs.String("exitcode", "", "")

	if err := fs.Parse(args); err != nil {
		return "", xerrors.Wrap(errUsage, err.Error())
	}

	if fs.NArg() > 0 {
		return "", errUsage
	}

	code, err := strconv.Atoi(*exitCode)
	if err != nil || code < 0 || code > 255 {
		return "", xerrors.Wrap(errUsage, "-exitcode は 0 から 255 の整数です")
	}

	switch code {
	case 0:
		return "✅ apply が終わりました", nil
	case applyTimedOut:
		return "", errApplyTimeout
	default:
		return "", xerrors.Wrapf(errApplyFailed, "終了コード %d", code)
	}
}

// lockError は、Terraform が state の lock を取れなかったときに出す見出し（internal/command/clistate/state.go）。
// Terraform の版を上げたら、この文言が変わっていないかを見直す。
const lockError = "Error acquiring the state lock"

// runFailureKind は、apply の入口の段の種類を返します。種類は revert の対象かの判定（scripts/apply-revert）が
// 読む。plan と apply の段は、どちらも state の lock を取るので、終了コード 1 で全文が lock の取得の失敗を
// 述べていれば lock とする。apply の段は、0 なら applied（apply は終わり、それより後の失敗は commit の中身に
// 依らない）、`timeout` の打ち切り（124）なら timeout とする。それ以外は段の名前（plan か apply）を返す。
func runFailureKind(args []string, readFile func(string) ([]byte, error)) (string, error) {
	fs := flag.NewFlagSet("failure-kind", flag.ContinueOnError)
	step := fs.String("step", "", "")
	exitCode := fs.String("exitcode", "", "")

	if err := fs.Parse(args); err != nil {
		return "", xerrors.Wrap(errUsage, err.Error())
	}

	code, err := strconv.Atoi(*exitCode)
	if err != nil || code < 0 || code > 255 {
		return "", xerrors.Wrap(errUsage, "-exitcode は 0 から 255 の整数です")
	}

	if *step != "plan" && *step != "apply" {
		return "", xerrors.Wrap(errUsage, "-step は plan か apply です")
	}

	if fs.NArg() != 1 {
		return "", errUsage
	}

	switch {
	case *step == "apply" && code == 0:
		return "applied", nil
	case *step == "apply" && code == applyTimedOut:
		return "timeout", nil
	case code != 1:
		return *step, nil
	}

	data, err := readFile(fs.Arg(0))
	if err != nil {
		return "", xerrors.Wrapf(errFailureKind, "%s の全文を読めません", *step)
	}

	if strings.Contains(string(data), lockError) {
		return "lock", nil
	}

	return *step, nil
}

func runPolicyResult(args []string, readFile func(string) ([]byte, error)) (string, error) {
	fs := flag.NewFlagSet("policy-result", flag.ContinueOnError)
	namespace := fs.String("namespace", "", "")
	rego := fs.String("rego", "", "")
	policy := fs.String("policy", "", "")

	if err := fs.Parse(args); err != nil {
		return "", xerrors.Wrap(errUsage, err.Error())
	}

	single := *namespace != "" || *rego != ""
	if fs.NArg() != 1 || single == (*policy != "") || single && (*namespace == "" || *rego == "") {
		return "", errUsage
	}

	var rules map[string]int

	if single {
		src, err := readFile(*rego)
		if err != nil {
			return "", xerrors.Wrap(errPolicyRules, "ファイルを読めません")
		}

		if n := policyresult.CountRules(src); n > 0 {
			rules = map[string]int{*namespace: n}
		}
	} else {
		var err error
		if rules, err = policyresult.Rules(*policy, readFile); err != nil {
			return "", err
		}
	}

	if len(rules) == 0 {
		return "", xerrors.Wrap(errPolicyRules, "deny contains が1つもありません")
	}

	data, err := readFile(fs.Arg(0))
	if err != nil {
		return "", xerrors.Wrap(errPolicyJSON, "ファイルを読めません")
	}

	entries, err := policyresult.Parse(data)
	if err != nil {
		return "", err
	}

	if single {
		entries = slices.DeleteFunc(entries, func(e policyresult.Entry) bool { return e.Namespace != *namespace })
	}

	return policyresult.Judge(rules, entries)
}

// fileSHA256 は、ファイルの SHA-256 を base64 で返します。S3 は受け取った本文をこの値と突き合わせ、違えば
// 書き込みを拒む。
func fileSHA256(path string, readFile func(string) ([]byte, error)) (string, error) {
	data, err := readFile(path)
	if err != nil {
		return "", errDigest
	}

	sum := sha256.Sum256(data)

	return base64.StdEncoding.EncodeToString(sum[:]), nil
}
