package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/anomaly"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/awsenv"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var (
	// tempUserARN は一時 IAM user の ARN。path 付きの user も通し、path の誤りは 1-3 の確認で NG にする。
	tempUserARN = regexp.MustCompile(`^arn:aws:iam::[0-9]{12}:user/(?:.+/)?([A-Za-z0-9+=,.@_-]+)$`)

	errNotTempUser = xerrors.New("一時 IAM user の資格情報ではありません。make aws-login をやり直してください")
	// errAWSStart は、aws を起動できないこと（PATH に無いなど）。呼び出しの失敗（aws の非ゼロ終了）とは分け、
	// 確認を続けずに検証できないとする。
	errAWSStart = xerrors.New("aws を起動できません")
	// errNoProfile は、AWS_PROFILE が空なこと。資格情報を変える環境変数を外し、profile を渡すのは make である。
	errNoProfile = xerrors.New("AWS_PROFILE が空です。make bootstrap-check-account か make bootstrap-verify から起動してください")
	// errStaticCredentials は、profile より優先される資格情報か、profile の読み先を変える指定が環境に在ること。
	errStaticCredentials = xerrors.New("環境に profile を迂回する資格情報の指定が在ります。make bootstrap-check-account か make bootstrap-verify から起動してください（make が外します）")
	// errNotReadOnly は、読むだけでない aws の操作を起動しようとしたこと（確認の表の誤り）。
	errNotReadOnly = xerrors.New("読むだけでない aws の操作は起動しません")

	readOnlyPrefixes = []string{"get-", "list-", "describe-", "head-", "simulate-"}
)

// awsProfile は、aws に使わせる profile。静的な資格情報が環境に在るか、AWS_PROFILE が空なら、aws を起動せずに止める。
func awsProfile(getenv func(string) string) (string, error) {
	if found := awsenv.Found(getenv); len(found) > 0 {
		return "", xerrors.Wrapf(errStaticCredentials, "%s", strings.Join(found, "・"))
	}

	profile := getenv("AWS_PROFILE")
	if profile == "" {
		return "", errNoProfile
	}

	return profile, nil
}

// awsResult は、aws の1回の起動の結果。err は終了の失敗（*exec.ExitError）か起動の失敗。
type awsResult struct {
	stdout []byte
	stderr string
	err    error
}

// awsFunc は aws を引数どおりに起動する。環境は呼び出し元（make）が整えたものを引き継ぐ。
type awsFunc func(args ...string) awsResult

// hostAWS は、bin（名前なら PATH から探す）を子プロセスとして起動する awsFunc を返す。
func hostAWS(bin string) awsFunc {
	return func(args ...string) awsResult {
		path, err := exec.LookPath(bin)
		if err != nil {
			return awsResult{err: err}
		}

		var stdout, stderr bytes.Buffer

		cmd := &exec.Cmd{Path: path, Args: append([]string{bin}, args...), Stdout: &stdout, Stderr: &stderr}
		err = cmd.Run()

		return awsResult{stdout: stdout.Bytes(), stderr: stderr.String(), err: err}
	}
}

func (r awsResult) failed() bool {
	return r.err != nil
}

type accountOptions struct {
	project string
	region  string
}

func parseAccountArgs(args []string) (accountOptions, error) {
	fs := flag.NewFlagSet("account", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var o accountOptions

	fs.StringVar(&o.project, "project", "", "")
	fs.StringVar(&o.region, "region", "", "")

	if err := fs.Parse(args); err != nil {
		return accountOptions{}, xerrors.Wrap(errUsage, err.Error())
	}

	if fs.NArg() != 0 || o.project == "" || o.region == "" {
		return accountOptions{}, errUsage
	}

	return o, nil
}

// query は、aws の呼び出しに --output と --region を付けて起動し、同じ引数の結果を使い回す。terse なら、失敗の理由に
// stderr の文を使わず、エラーコードだけを使う（stderr の文は ARN を含む。公開されるログに出す経路で使う）。
type query struct {
	aws    awsFunc
	region string
	cache  map[string]awsResult
	terse  bool
}

func newQuery(aws awsFunc, region string) *query {
	return &query{aws: aws, region: region, cache: map[string]awsResult{}}
}

// 操作名（args の2番目）が読むだけのもの（get-・list-・describe-・head-・simulate-）でなければ、起動せずに errNotReadOnly。
func (q *query) call(output string, args ...string) (awsResult, error) {
	if len(args) < 2 || !slices.ContainsFunc(readOnlyPrefixes, func(p string) bool { return strings.HasPrefix(args[1], p) }) {
		return awsResult{}, xerrors.Wrapf(errNotReadOnly, "aws %s", strings.Join(args, " "))
	}

	full := append(append([]string{}, args...), "--output", output, "--region", q.region)
	key := strings.Join(full, "\x00")

	if r, ok := q.cache[key]; ok {
		return r, nil
	}

	r := q.aws(full...)

	var exitErr *exec.ExitError
	if r.err != nil && !xerrors.As(r.err, &exitErr) {
		return awsResult{}, xerrors.Wrap(errAWSStart, r.err.Error())
	}

	q.cache[key] = r

	return r, nil
}

// object は、aws の JSON の応答を読む。呼び出しの失敗と、読めない応答（空、JSON でない、object でない）は
// 理由を返す。
func (q *query) object(args ...string) (map[string]any, string, error) {
	r, err := q.call("json", args...)
	if err != nil {
		return nil, "", err
	}

	if r.failed() {
		return nil, "呼び出しが失敗: " + q.failure(r.stderr), nil
	}

	v, ok := decodeObject(r.stdout)
	if !ok {
		return nil, "応答を読めない", nil
	}

	return v, "", nil
}

// failure は、aws の失敗の stderr から理由に使う文。terse なら `An error occurred (<code>)` のコードと、その後ろの本文を
// 1行に縮めたもの（ARN と context の entry と12桁の数字を伏せる。terseBody）。コードが無ければ文を出さないことを述べる。
func (q *query) failure(stderr string) string {
	if !q.terse {
		return firstLine(stderr)
	}

	loc := awsErrorCode.FindStringSubmatchIndex(stderr)
	if loc == nil {
		return "エラーの文は出さない"
	}

	code := stderr[loc[2]:loc[3]]

	body := stderr[loc[1]:]
	if _, after, ok := strings.Cut(body, "operation: "); ok {
		body = after
	}

	if body = terseBody(body); body == "" {
		return code
	}

	return code + ": " + body
}

var (
	contextEntryPattern = regexp.MustCompile(`\S*ContextKey\S*`)
	arnPattern          = regexp.MustCompile(`arn:[^\s,;"'()\[\]{}]*`)
)

// terseBody は、aws のエラーの本文を公開されるログに出せる形にする。context の entry を `<context>`、ARN を `<arn>`、
// 12桁の数字を `<account>` に置き換え、空白を1つに縮めて1行にする。
func terseBody(s string) string {
	s = contextEntryPattern.ReplaceAllString(s, "<context>")
	s = arnPattern.ReplaceAllString(s, "<arn>")
	s = accountPattern.ReplaceAllString(s, "<account>")

	return strings.Join(strings.Fields(s), " ")
}

// decodeObject は、JSON の object を読む。空、JSON でない、object でない（null を含む）ものは false。
func decodeObject(b []byte) (map[string]any, bool) {
	var v map[string]any

	err := json.Unmarshal(b, &v)

	return v, err == nil && v != nil
}

// firstLine は、空白だけでない最初の行。無ければ「不明」。
func firstLine(s string) string {
	for line := range strings.SplitSeq(s, "\n") {
		if strings.TrimSpace(line) != "" {
			return strings.TrimRight(line, "\r")
		}
	}

	return "不明"
}

// verdict は1件の確認の結果。reason は NG に添える理由（判定が偽なだけなら空）。
type verdict struct {
	ok     bool
	reason string
}

type check struct {
	desc string
	eval func(q *query) (verdict, error)
}

// expectJSON は、aws の応答が pred を満たせば ok とする確認。
func expectJSON(desc string, pred func(map[string]any) bool, args ...string) check {
	return check{desc: desc, eval: func(q *query) (verdict, error) {
		v, reason, err := q.object(args...)
		if err != nil || reason != "" {
			return verdict{reason: reason}, err
		}

		return verdict{ok: pred(v)}, nil
	}}
}

// accountChecks は、アカウントの準備（1-0 から 2-4）の確認の表。user は一時 IAM user の名前。
func accountChecks(user, project string) []check {
	tagKey := project + ":bootstrap"

	checks := []check{
		expectJSON("1-0 アカウントが Paid plan", func(v map[string]any) bool {
			return at(v, "accountPlanType") == "PAID"
		}, "freetier", "get-account-plan-state"),
		expectJSON("1-1 root に MFA が在る（2台以上かはコンソールで見る）", func(v map[string]any) bool {
			return at(v, "SummaryMap", "AccountMFAEnabled") == float64(1)
		}, "iam", "get-account-summary"),
		expectJSON("1-3 一時 IAM user のポリシーが AdministratorAccess と SignInLocalDevelopmentAccess だけ", func(v map[string]any) bool {
			names, ok := stringsAt(at(v, "AttachedPolicies"), "PolicyName")
			slices.Sort(names)

			return ok && slices.Equal(names, []string{"AdministratorAccess", "SignInLocalDevelopmentAccess"})
		}, "iam", "list-attached-user-policies", "--user-name", user),
		expectJSON("1-3 一時 IAM user の path が /", func(v map[string]any) bool {
			return at(v, "User", "Path") == "/"
		}, "iam", "get-user", "--user-name", user),
		expectJSON("1-3 一時 IAM user に MFA が在る", func(v map[string]any) bool {
			return arrayLen(at(v, "MFADevices")) >= 1
		}, "iam", "list-mfa-devices", "--user-name", user),
		expectJSON("1-3 一時 IAM user にアクセスキーが無い", func(v map[string]any) bool {
			return arrayLen(at(v, "AccessKeyMetadata")) == 0
		}, "iam", "list-access-keys", "--user-name", user),
		expectJSON("1-4 root にアクセスキーが無い", func(v map[string]any) bool {
			return at(v, "SummaryMap", "AccountAccessKeysPresent") == float64(0)
		}, "iam", "get-account-summary"),
		expectJSON("2-1 連絡先に氏名・住所・電話番号が在る", func(v map[string]any) bool {
			return nonEmpty(at(v, "ContactInformation", "FullName")) &&
				nonEmpty(at(v, "ContactInformation", "AddressLine1")) &&
				nonEmpty(at(v, "ContactInformation", "PhoneNumber"))
		}, "account", "get-contact-information"),
	}

	for _, typ := range []string{"BILLING", "OPERATIONS", "SECURITY"} {
		checks = append(checks, expectJSON("2-1 代替連絡先 "+typ+" が在る", func(v map[string]any) bool {
			return at(v, "AlternateContact", "AlternateContactType") != nil
		}, "account", "get-alternate-contact", "--alternate-contact-type", typ))
	}

	return append(checks,
		expectJSON("2-2 Cost Anomaly Detection の monitor が在る", func(v map[string]any) bool {
			return arrayLen(at(v, "AnomalyMonitors")) >= 1
		}, "ce", "get-anomaly-monitors"),
		expectJSON("2-2 Cost Anomaly Detection の通知が在る", func(v map[string]any) bool {
			return arrayLen(at(v, "AnomalySubscriptions")) >= 1
		}, "ce", "get-anomaly-subscriptions"),
		check{desc: "2-4 AWS services monitor に tag " + tagKey + " が在る", eval: func(q *query) (verdict, error) {
			arn, reason, err := serviceMonitor(q)
			if err != nil || reason != "" {
				return verdict{reason: reason}, err
			}

			return hasTag(q, arn, tagKey)
		}},
		check{desc: "2-4 AWS services monitor の通知に tag " + tagKey + " が在る", eval: func(q *query) (verdict, error) {
			monitor, reason, err := serviceMonitor(q)
			if err != nil || reason != "" {
				if reason != "" {
					reason = "monitor を特定できない: " + reason
				}

				return verdict{reason: reason}, err
			}

			arn, reason, err := monitorSubscription(q, monitor)
			if err != nil || reason != "" {
				return verdict{reason: reason}, err
			}

			return hasTag(q, arn, tagKey)
		}},
	)
}

// serviceMonitor は、get-anomaly-monitors を読み、anomaly.ServiceMonitor で決めた ARN か、決まらない理由を返す。
func serviceMonitor(q *query) (string, string, error) {
	v, reason, err := q.object("ce", "get-anomaly-monitors")
	if err != nil || reason != "" {
		return "", reason, err
	}

	var monitors []anomaly.Monitor

	for _, m := range arrayAt(at(v, "AnomalyMonitors")) {
		arn, _ := at(m, "MonitorArn").(string)
		dim, _ := at(m, "MonitorDimension").(string)
		monitors = append(monitors, anomaly.Monitor{Arn: arn, Dimension: dim})
	}

	arn, reason := anomaly.ServiceMonitor(monitors)

	return arn, reason, nil
}

// monitorSubscription は、get-anomaly-subscriptions を読み、anomaly.MonitorSubscription で決めた ARN か、決まらない理由を返す。
func monitorSubscription(q *query, monitor string) (string, string, error) {
	v, reason, err := q.object("ce", "get-anomaly-subscriptions")
	if err != nil || reason != "" {
		return "", reason, err
	}

	var subs []anomaly.Subscription

	for _, s := range arrayAt(at(v, "AnomalySubscriptions")) {
		monitors, _ := stringsAt(at(s, "MonitorArnList"), "")
		arn, _ := at(s, "SubscriptionArn").(string)
		subs = append(subs, anomaly.Subscription{Arn: arn, Monitors: monitors})
	}

	arn, reason := anomaly.MonitorSubscription(subs, monitor)

	return arn, reason, nil
}

// hasTag は、resource が key の tag を持てば ok とする。値は見ない。
func hasTag(q *query, arn, key string) (verdict, error) {
	v, reason, err := q.object("ce", "list-tags-for-resource", "--resource-arn", arn)
	if err != nil || reason != "" {
		return verdict{reason: reason}, err
	}

	keys, ok := stringsAt(at(v, "ResourceTags"), "Key")

	return verdict{ok: ok && slices.Contains(keys, key)}, nil
}

// checkAccount は、一時 IAM user の資格情報でアカウントの準備を読んで確かめる。
func checkAccount(o accountOptions, out io.Writer, d deps) error {
	profile, err := awsProfile(d.getenv)
	if err != nil {
		return err
	}

	q := newQuery(d.aws, o.region)

	r, err := q.call("text", "sts", "get-caller-identity", "--query", "Arn")
	if err != nil {
		return err
	}

	arn := ""
	if !r.failed() {
		arn = strings.TrimSpace(string(r.stdout))
	}

	m := tempUserARN.FindStringSubmatch(arn)
	if m == nil {
		if arn == "" {
			arn = "取得できず"
		}

		return xerrors.Wrapf(errNotTempUser, "sts get-caller-identity の Arn: %s", arn)
	}

	fmt.Fprintf(out, "# アカウントの準備（profile: %s、user: %s）\n", profile, m[1])

	var steps []step
	for _, c := range accountChecks(m[1], o.project) {
		steps = append(steps, checkStep(c))
	}

	return runChecks(q, out, steps)
}

// at は、JSON の object を keys の順にたどった値。途中が object でなければ nil。
func at(v any, keys ...string) any {
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}

		v = m[k]
	}

	return v
}

func arrayAt(v any) []any {
	a, _ := v.([]any)

	return a
}

// arrayLen は配列の長さ。配列でなければ -1。
func arrayLen(v any) int {
	a, ok := v.([]any)
	if !ok {
		return -1
	}

	return len(a)
}

// stringsAt は、配列の各要素の key の文字列（key が空なら要素そのもの）。配列でないか、文字列でない要素が
// あれば false。
func stringsAt(v any, key string) ([]string, bool) {
	a, ok := v.([]any)
	if !ok {
		return nil, false
	}

	out := make([]string, 0, len(a))

	for _, e := range a {
		if key != "" {
			e = at(e, key)
		}

		s, ok := e.(string)
		if !ok {
			return nil, false
		}

		out = append(out, s)
	}

	return out, true
}

func nonEmpty(v any) bool {
	s, ok := v.(string)

	return ok && s != ""
}
