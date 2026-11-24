// Package main は、Terraform を動かす入口（人の手元の .makefiles/terraform/operation/local.mk と、
// CI の ci.mk）が aws と terraform に渡す値を組み立てて検証するツール。
//
//	tf-local region <root>
//	    <root>/backend.tf の backend の region を1行で出す。<root>/providers.tf の provider の
//	    region と違えば失敗する（Bucket 名の region は provider の region から決まる）
//	tf-local bucket -project <p> -env <e> [-kind tfstate|tfplan|tfapply] -region <r> -identity <out>
//	    `aws sts get-caller-identity --query '[Account,Arn]' --output text` の出力から、
//	    state Bucket の名前を1行で出す
//	tf-local state-key -project <p> -env <e> -region <r> -identity <out> -aliases <json> -encryption <json> -missing <fail|sse-s3>
//	    `aws kms list-aliases` と `aws s3api get-bucket-encryption` の JSON から、init に渡す
//	    `-backend-config=kms_key_id=<state の鍵の ARN>` を1行で出す。Bucket の既定が SSE-KMS で鍵が違えば失敗する。
//	    alias が無ければ失敗し、-missing sse-s3 で既定が AES256 のときだけ空行を出す
//	tf-local env-unset
//	    aws と terraform の振る舞いを呼び出し元の環境変数で変えさせないために外す名前を、
//	    `env` に渡す `-u <名前>` の並びで出す
//	tf-local plan-header
//	    plan の log の1行目（plan した commit と、作業ツリーに未コミットの変更があるか）を出す
//	tf-local policy-result -namespace <ns> -rego <file> <conftest.json>
//	tf-local policy-result -policy <dir> <conftest.json>
//	    `conftest test -o json` の結果から、名前空間の規則の評価件数と違反を読む。違反が無いのに
//	    評価が rego の `deny contains` の数でなければ失敗する。-policy は dir の下の rego を走査して、
//	    package ごとに突き合わせる
//
//	tf-local account -identity <out>
//	    `aws sts get-caller-identity --query '[Account,Arn]' --output text` の出力から、アカウント ID を1行で出す
//	tf-local lock-key <root>
//	    <root>/backend.tf の key から、state の lock file の key を1行で出す
//	tf-local lock-etag -key <k> [-want absent|present] <list.json>
//	    `aws s3api list-objects-v2 --no-paginate` の応答から、lock file の ETag を1行で出す。lock が無ければ空行を出す。
//	    -want absent は lock があれば、-want present は lock が無ければ失敗する
//	tf-local lock-stale -etag <e> -meta <meta.json> <lock.json>
//	    `aws s3api get-object` の応答と lock file から、消してよい lock の説明を1行で出す。lock が apply の job の
//	    timeout と余裕を超えて古くなければ、説明を添えて失敗する
//	tf-local lock-held -etag <e> -meta <meta.json> <lock.json>
//	    lock を残す job が残した lock の説明と、lock の解除が消せるようになる時刻を出す。lock の解除が消す形の
//	    lock でなければ失敗する
//	tf-local lock-wait -pid <pid> [-interval <d>] [-limit <d>] <全文>
//	    裏で走る plan の全文に、lock を取った後の行が現れたら held を出す。現れる前に plan が終われば空行を出す。
//	    どちらも起きずに limit を過ぎたら失敗する
//
// 人の手元の入口の段（local-prepare・local-init・local-plan。local.go）と、CI の資格情報の段（ci-credentials・
// ci-assume-denied。cicred.go）は、1回の起動で1つの目的の道具（aws か terraform）だけを、固定の引数で起動する
// （ADR-0702 決定3・24）。CI の資格情報の段は、OIDC の token を GitHub Actions から自分で取る。
// そのほかのサブコマンドは aws も terraform も起動せず、値だけを返す。plan-header だけは git を起動して作業ツリーを
// 読む。policy-result・lock-key・lock-etag・lock-stale・lock-held・lock-wait は
// ファイルを読み、lock-stale と lock-held は時刻を読み、lock-wait は pid のプロセスが生きているかを見る。
// 値を決められない入力はすべて非ゼロで終わり、make はその時点で terraform を呼ばない。
// make のレシピに分岐を置かず、判断はすべてここのテストで固定する。
//
// backend block は式を持てないので、Bucket 名・state の鍵の alias 名・role 名の規則をここに写す。Bucket 名と alias 名の正本は
// modules/s3 と usecases/state-backend、role 名の正本は usecases/deployment-identity で、写しのずれはテストが
// env/admin/identity のテストの期待値と docs/setup/bootstrap.md の段1、deployment-identity の .tf に
// 突き合わせて見る（ADR-0702 決定17 の但し書き）。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// bucketFormat は、`<name>-<account_id>-<region>-an` に、usecases/state-backend の Bucket の name
// （`<project>-<env>-<種類>`）を入れた形。
const bucketFormat = "%s-%s-%s-%s-%s-an"

// bucketKinds は、usecases/state-backend の Bucket の name の末尾。state・plan の出力・apply の出力の順。
var bucketKinds = []string{"tfstate", "tfplan", "tfapply"}

const gitTimeout = 30 * time.Second

var (
	regionLine     = regexp.MustCompile(`^\s*region\s*=\s*"([^"]*)"`)
	regionPattern  = regexp.MustCompile(`^[a-z]{2}(-[a-z]+)+-[0-9]$`)
	accountPattern = regexp.MustCompile(`^[0-9]{12}$`)
	tokenPattern   = regexp.MustCompile(`^[a-z0-9]+$`)
	commitPattern  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

	// unsetPrefixes と unsetNames は、資格情報・引数・入力・対象の state・provider を変える変数と、
	// 資格情報を含みうる trace を出す変数と、CI の job の OIDC token を新たに要求できる変数。
	// TF_PLUGIN_CACHE_DIR は lock の hash で検証されるので残す。
	unsetPrefixes = []string{"AWS_", "TF_CLI_ARGS", "TF_VAR_", "TF_LOG"}
	unsetNames    = []string{"TF_WORKSPACE", "TF_DATA_DIR", "TF_CLI_CONFIG_FILE", "TF_REATTACH_PROVIDERS",
		"ACTIONS_ID_TOKEN_REQUEST_URL", "ACTIONS_ID_TOKEN_REQUEST_TOKEN"}

	errUsage            = xerrors.New("❌ usage: tf-local region <root> | bucket -project <p> -env <e> [-kind tfstate|tfplan|tfapply] -region <r> -identity <out> | state-key ... | env-unset | plan-header | policy-result -namespace <ns> -rego <file> <file> | policy-result -policy <dir> <file> | account -identity <out> | lock-key <root> | lock-etag -key <k> [-want absent|present] <file> | lock-stale -etag <e> -meta <file> <file> | lock-held -etag <e> -meta <file> <file> | lock-wait -pid <pid> [-interval <d>] [-limit <d>] <file> | local-prepare -project <p> -profile <p> -env <e> -root identity|workload -base <dir> [-missing fail|sse-s3] -out <file> | local-init -profile <p> -state <file> | local-plan -profile <p> -env <e> -root <r> -base <dir> -header <file> -log <file> | ci-credentials -project <p> -env <e> -role <r> -base <dir> -work <dir> | ci-assume-denied -project <p> -env <e> -base <dir> -work <dir> | ci-prepare -mode plan|apply -project <p> -env <e> -root <r> -profile <p> -base <dir> -header <file> -state <file> | ci-init|ci-plan|ci-upload|ci-finish -state <file> | ci-policy -state <file> -policy <dir> -decl <file> -layer <file> -namespace <ns>... | ci-apply -state <file> -timeout <d>")
	errBackendMissing   = xerrors.New("❌ backend.tf がありません")
	errProvidersMissing = xerrors.New("❌ providers.tf がありません")
	errRegionMismatch   = xerrors.New("❌ provider と backend の region が違います")
	errRegionNotOne     = xerrors.New("❌ region が1つではありません")
	errRegionMalformed  = xerrors.New("❌ region の形が不正です")
	errIdentity         = xerrors.New("❌ アカウント ID を取得できません（make aws-login を実行してください）")
	errRootCredentials  = xerrors.New("❌ root の資格情報では実行しません")
	errToken            = xerrors.New("❌ project と env は英小文字と数字だけです")
	errBucketKind       = xerrors.New("❌ Bucket の種類は tfstate・tfplan・tfapply のどれかです")
	errGit              = xerrors.New("❌ plan した commit を git から決められません")
	errEnvName          = xerrors.New("❌ 外す環境変数の名前が不正です")
)

type deps struct {
	readFile func(string) ([]byte, error)
	environ  []string
	git      func(args ...string) (string, error)
	now      func() time.Time
	alive    func(pid int) bool
	sleep    func(time.Duration)
	tool     toolFunc
	fetch    fetchFunc
	stderr   io.Writer
}

func main() {
	log.SetFlags(0)

	if err := run(os.Args[1:], os.Stdout, deps{readFile: os.ReadFile, environ: os.Environ(), git: gitOutput, now: time.Now, alive: processAlive, sleep: time.Sleep, tool: command, fetch: fetchOIDC, stderr: os.Stderr}); err != nil {
		log.Fatalf("%v", err)
	}
}

// argCounts は、位置引数だけを取るサブコマンドの、サブコマンド名を含む引数の数。
var argCounts = map[string]int{
	"region":      2,
	"env-unset":   1,
	"plan-header": 1,
	"lock-key":    2,
}

func run(args []string, out io.Writer, d deps) error {
	if len(args) == 0 {
		return errUsage
	}

	if n, ok := argCounts[args[0]]; ok && len(args) != n {
		return errUsage
	}

	arg := ""
	if len(args) > 1 {
		arg = args[1]
	}

	var (
		value string
		err   error
	)

	switch args[0] {
	case "region":
		value, err = rootRegion(arg, d.readFile)
	case "bucket":
		value, err = runBucket(args[1:])
	case "state-key":
		value, err = runStateKey(args[1:])
	case "env-unset":
		value, err = envUnset(d.environ)
	case "plan-header":
		value, err = planHeader(d.git)
	case "policy-result":
		value, err = runPolicyResult(args[1:], d.readFile)
	case "account":
		value, err = runAccount(args[1:])
	case "lock-key":
		value, err = lockKey(arg, d.readFile)
	case "lock-etag":
		value, err = runLockETag(args[1:], d.readFile)
	case "lock-stale":
		value, err = runLockStale(args[1:], d)
	case "lock-held":
		value, err = runLockHeld(args[1:], d)
	case "lock-wait":
		value, err = runLockWait(args[1:], d)
	case "local-prepare":
		value, err = localPrepare(args[1:], d, d.stderr)
	case "local-init":
		value, err = localInit(args[1:], d, out, d.stderr)
	case "local-plan":
		value, err = localPlan(args[1:], d, out, d.stderr)
	case "ci-credentials":
		value, err = ciCredentials(args[1:], d, out)
	case "ci-assume-denied":
		value, err = ciAssumeDenied(args[1:], d, out)
	case "ci-prepare":
		value, err = ciPrepareStep(args[1:], d, out)
	case "ci-init":
		value, err = ciInitStep(args[1:], d)
	case "ci-plan":
		value, err = ciPlanStep(args[1:], d, out)
	case "ci-policy":
		value, err = ciPolicyStep(args[1:], d, out)
	case "ci-apply":
		value, err = ciApplyStep(args[1:], d, out)
	case "ci-upload":
		value, err = ciUploadStep(args[1:], d, out)
	case "ci-finish":
		value, err = ciFinishStep(args[1:], d)
	default:
		return xerrors.Wrap(errUsage, args[0])
	}

	if err != nil {
		return err
	}

	_, err = fmt.Fprintln(out, value)

	return err
}

func runBucket(args []string) (string, error) {
	fs := flag.NewFlagSet("bucket", flag.ContinueOnError)
	project := fs.String("project", "", "")
	env := fs.String("env", "", "")
	kind := fs.String("kind", "tfstate", "")
	region := fs.String("region", "", "")
	identity := fs.String("identity", "", "")

	if err := fs.Parse(args); err != nil {
		return "", xerrors.Wrap(errUsage, err.Error())
	}

	if fs.NArg() > 0 {
		return "", xerrors.Wrap(errUsage, strings.Join(fs.Args(), " "))
	}

	account, err := accountOf(*identity)
	if err != nil {
		return "", err
	}

	return bucketName(*project, *env, *kind, account, *region)
}

func runAccount(args []string) (string, error) {
	fs := flag.NewFlagSet("account", flag.ContinueOnError)
	identity := fs.String("identity", "", "")

	if err := fs.Parse(args); err != nil {
		return "", xerrors.Wrap(errUsage, err.Error())
	}

	if fs.NArg() > 0 {
		return "", errUsage
	}

	return accountOf(*identity)
}

// accountOf は、callerAccount の失敗の文から入力の値を落とします。文は CI の公開されるログに出る。
func accountOf(identity string) (string, error) {
	account, err := callerAccount(identity)
	if errors.Is(err, errRootCredentials) {
		return "", errRootCredentials
	}

	if err != nil {
		return "", errIdentity
	}

	return account, nil
}

func rootRegion(root string, readFile func(string) ([]byte, error)) (string, error) {
	backendPath := filepath.Join(root, "backend.tf")

	backend, err := readFile(backendPath)
	if err != nil {
		return "", xerrors.Wrap(errBackendMissing, backendPath)
	}

	providersPath := filepath.Join(root, "providers.tf")

	providers, err := readFile(providersPath)
	if err != nil {
		return "", xerrors.Wrap(errProvidersMissing, providersPath)
	}

	region, err := backendRegion(string(backend))
	if err != nil {
		return "", xerrors.Wrap(err, backendPath)
	}

	providerRegion, err := backendRegion(string(providers))
	if err != nil {
		return "", xerrors.Wrap(err, providersPath)
	}

	if region != providerRegion {
		return "", xerrors.Wrap(errRegionMismatch, fmt.Sprintf("backend=%s provider=%s", region, providerRegion))
	}

	return region, nil
}

// backendRegion は、0件・複数件・形の誤りを、どれか1つを選ばずにエラーにします。選んだ値が実際の
// Bucket の region と違えば、別の名前の Bucket へ init することになるためです。
func backendRegion(content string) (string, error) {
	var regions []string

	for line := range strings.Lines(content) {
		if m := regionLine.FindStringSubmatch(line); m != nil {
			regions = append(regions, m[1])
		}
	}

	if len(regions) != 1 {
		return "", xerrors.Wrap(errRegionNotOne, fmt.Sprintf("%d 件", len(regions)))
	}

	if !regionPattern.MatchString(regions[0]) {
		return "", xerrors.Wrap(errRegionMalformed, regions[0])
	}

	return regions[0], nil
}

// callerAccount は、`<account>\t<arn>` の形の出力からアカウント ID を取り出します。
// root の資格情報（Arn が `:root` で終わる）は拒否します（docs/setup/bootstrap.md 2-0b）。
func callerAccount(identity string) (string, error) {
	fields := strings.Fields(identity)
	if len(fields) != 2 || !accountPattern.MatchString(fields[0]) {
		return "", xerrors.Wrap(errIdentity, fmt.Sprintf("%q", strings.TrimSpace(identity)))
	}

	if strings.HasSuffix(fields[1], ":root") {
		return "", xerrors.Wrap(errRootCredentials, fields[1])
	}

	return fields[0], nil
}

func bucketName(project, env, kind, account, region string) (string, error) {
	if !tokenPattern.MatchString(project) || !tokenPattern.MatchString(env) {
		return "", xerrors.Wrap(errToken, fmt.Sprintf("project=%q env=%q", project, env))
	}

	if !slices.Contains(bucketKinds, kind) {
		return "", xerrors.Wrap(errBucketKind, fmt.Sprintf("%q", kind))
	}

	if !regionPattern.MatchString(region) {
		return "", xerrors.Wrap(errRegionMalformed, region)
	}

	return fmt.Sprintf(bucketFormat, project, env, kind, account, region), nil
}

// envUnset は、environ のうち外す変数を `-u <名前>` の並びで返します。外す変数が無ければ空文字を返す。
// make はこの出力を単語分割して env に渡すので、外す変数の名前が変数名の形でなければ、
// 黙って残さずに失敗します。
func envUnset(environ []string) (string, error) {
	var names []string

	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")
		if !unset(name) {
			continue
		}

		if !envNamePattern.MatchString(name) {
			return "", xerrors.Wrap(errEnvName, fmt.Sprintf("%q", name))
		}

		names = append(names, name)
	}

	slices.Sort(names)

	flags := make([]string, 0, len(names))
	for _, name := range slices.Compact(names) {
		flags = append(flags, "-u "+name)
	}

	return strings.Join(flags, " "), nil
}

func unset(name string) bool {
	return slices.Contains(unsetNames, name) || slices.ContainsFunc(unsetPrefixes, func(p string) bool { return strings.HasPrefix(name, p) })
}

// planHeader は、どの commit に対する plan かを返します（ADR-0602 決定10 の、その commit に対する plan の
// 結果の記録）。git が答えられなければ、どの commit の plan かを残せないので失敗します。
func planHeader(git func(args ...string) (string, error)) (string, error) {
	commit, err := git("rev-parse", "HEAD")
	if err != nil {
		return "", xerrors.Wrap(errGit, err.Error())
	}

	status, err := git("status", "--porcelain")
	if err != nil {
		return "", xerrors.Wrap(errGit, err.Error())
	}

	commit = strings.TrimSpace(commit)
	if !commitPattern.MatchString(commit) {
		return "", xerrors.Wrap(errGit, fmt.Sprintf("rev-parse HEAD が %q を返しました", commit))
	}

	header := "commit: " + commit
	if strings.TrimSpace(status) != "" {
		header += " （作業ツリーに未コミットの変更あり）"
	}

	return header, nil
}

// processAlive は、pid のプロセスが在るかを返します。signal 0 は届くかを確かめるだけで、何も送らない。
func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

func gitOutput(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...) //nolint:gosec // 引数は planHeader が組んだ固定値
	cmd.Stderr = os.Stderr

	out, err := cmd.Output()
	if err != nil {
		return "", xerrors.Wrap(err, "git "+strings.Join(args, " "))
	}

	return string(out), nil
}
