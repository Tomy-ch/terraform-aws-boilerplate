// Package main は、Terraform を動かす入口（人の手元の .makefiles/terraform/operation/local.mk と、
// CI の ci.mk）が aws と terraform に渡す値を組み立てて検証するツール。
//
//	tf-local region <root>
//	    <root>/backend.tf の backend の region を1行で出す。<root>/providers.tf の provider の
//	    region と違えば失敗する（Bucket 名の region は provider の region から決まる）
//	tf-local bucket -project <p> -env <e> -region <r> -identity <out>
//	    `aws sts get-caller-identity --query '[Account,Arn]' --output text` の出力から、
//	    state Bucket の名前を1行で出す
//	tf-local env-unset
//	    aws と terraform の振る舞いを呼び出し元の環境変数で変えさせないために外す名前を、
//	    `env` に渡す `-u <名前>` の並びで出す
//	tf-local plan-header
//	    plan の log の1行目（plan した commit と、作業ツリーに未コミットの変更があるか）を出す
//	tf-local role-arn -project <p> -env <e> -role <plan|apply|apply-identity> -account <id>
//	    CI が assume する role の ARN を1行で出す
//	tf-local oidc-sub <token-file>
//	    GitHub Actions の OIDC token の sub を1行で出す
//	tf-local plan-summary -exitcode <0|2> <show.json>
//	    plan の終了コードと `terraform show -json` の変更を突き合わせ、件数の要約を1行で出す
//	tf-local plan-key (-pr <n> | -branch <b>) -run <id> -attempt <n> -root <root>
//	    plan の出力を置く object の key を1行で出す
//
// aws も terraform も起動しない。起動は make が行い、ここは値だけを返す（ADR-0702 決定3）。
// plan-header だけは git を起動して作業ツリーを読む。oidc-sub と plan-summary はファイルを読む。
// 値を決められない入力はすべて非ゼロで終わり、make はその時点で terraform を呼ばない。
// make のレシピに分岐を置かず、判断はすべてここのテストで固定する。
//
// backend block は式を持てないので、Bucket 名と role 名の規則をここに写す。正本は modules/s3 と
// usecases/state-backend で、写しのずれはテストが env/admin/identity のテストの期待値と
// docs/setup/bootstrap.md の段1 に突き合わせて見る（ADR-0702 決定17 の但し書き）。
// state-backend が組んだ名前を init の前に読めるようになったら、写しを消す。
package main

import (
	"context"
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
	"time"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// bucketFormat は、modules/s3 の `<name>-<account_id>-<region>-an` に、usecases/state-backend の
// name（`<project>-<env>-tfstate`）を入れた形。
const bucketFormat = "%s-%s-tfstate-%s-%s-an"

const gitTimeout = 30 * time.Second

var (
	regionLine     = regexp.MustCompile(`^\s*region\s*=\s*"([^"]*)"`)
	regionPattern  = regexp.MustCompile(`^[a-z]{2}(-[a-z]+)+-[0-9]$`)
	accountPattern = regexp.MustCompile(`^[0-9]{12}$`)
	tokenPattern   = regexp.MustCompile(`^[a-z0-9]+$`)
	commitPattern  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

	// unsetPrefixes と unsetNames は、資格情報・引数・入力・対象の state・provider を変える変数と、
	// 資格情報を含みうる trace を出す変数。TF_PLUGIN_CACHE_DIR は lock の hash で検証されるので残す。
	unsetPrefixes = []string{"AWS_", "TF_CLI_ARGS", "TF_VAR_", "TF_LOG"}
	unsetNames    = []string{"TF_WORKSPACE", "TF_DATA_DIR", "TF_CLI_CONFIG_FILE", "TF_REATTACH_PROVIDERS"}

	errUsage            = xerrors.New("❌ usage: tf-local region <root> | bucket -project <p> -env <e> -region <r> -identity <out> | env-unset | plan-header | role-arn ... | oidc-sub <file> | plan-summary -exitcode <c> <file> | plan-key ...")
	errBackendMissing   = xerrors.New("❌ backend.tf がありません")
	errProvidersMissing = xerrors.New("❌ providers.tf がありません")
	errRegionMismatch   = xerrors.New("❌ provider と backend の region が違います")
	errRegionNotOne     = xerrors.New("❌ region が1つではありません")
	errRegionMalformed  = xerrors.New("❌ region の形が不正です")
	errIdentity         = xerrors.New("❌ アカウント ID を取得できません（make aws-login を実行してください）")
	errRootCredentials  = xerrors.New("❌ root の資格情報では実行しません")
	errToken            = xerrors.New("❌ project と env は英小文字と数字だけです")
	errGit              = xerrors.New("❌ plan した commit を git から決められません")
	errEnvName          = xerrors.New("❌ 外す環境変数の名前が不正です")
)

type deps struct {
	readFile func(string) ([]byte, error)
	environ  []string
	git      func(args ...string) (string, error)
}

func main() {
	log.SetFlags(0)

	if err := run(os.Args[1:], os.Stdout, deps{readFile: os.ReadFile, environ: os.Environ(), git: gitOutput}); err != nil {
		log.Fatalf("%v", err)
	}
}

func run(args []string, out io.Writer, d deps) error {
	if len(args) == 0 {
		return errUsage
	}

	var (
		value string
		err   error
	)

	switch args[0] {
	case "region":
		if len(args) != 2 {
			return errUsage
		}

		value, err = rootRegion(args[1], d.readFile)
	case "bucket":
		value, err = runBucket(args[1:])
	case "env-unset":
		if len(args) != 1 {
			return errUsage
		}

		value, err = envUnset(d.environ)
	case "plan-header":
		if len(args) != 1 {
			return errUsage
		}

		value, err = planHeader(d.git)
	case "role-arn":
		value, err = runRoleARN(args[1:])
	case "oidc-sub":
		if len(args) != 2 {
			return errUsage
		}

		value, err = oidcSub(args[1], d.readFile)
	case "plan-summary":
		value, err = runPlanSummary(args[1:], d.readFile)
	case "plan-key":
		value, err = runPlanKey(args[1:])
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
	region := fs.String("region", "", "")
	identity := fs.String("identity", "", "")

	if err := fs.Parse(args); err != nil {
		return "", xerrors.Wrap(errUsage, err.Error())
	}

	if fs.NArg() > 0 {
		return "", xerrors.Wrap(errUsage, strings.Join(fs.Args(), " "))
	}

	account, err := callerAccount(*identity)
	if err != nil {
		return "", err
	}

	return bucketName(*project, *env, account, *region)
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

func bucketName(project, env, account, region string) (string, error) {
	if !tokenPattern.MatchString(project) || !tokenPattern.MatchString(env) {
		return "", xerrors.Wrap(errToken, fmt.Sprintf("project=%q env=%q", project, env))
	}

	if !regionPattern.MatchString(region) {
		return "", xerrors.Wrap(errRegionMalformed, region)
	}

	return fmt.Sprintf(bucketFormat, project, env, account, region), nil
}

// envUnset は、environ のうち外す変数を `-u <名前>` の並びで返します。外す変数が無ければ空文字を返す。
// make はこの出力を単語分割して env に渡すので、外す変数の名前が変数名の形でなければ、
// 黙って残さずに失敗します。
func envUnset(environ []string) (string, error) {
	var names []string

	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")
		if !slices.Contains(unsetNames, name) && !slices.ContainsFunc(unsetPrefixes, func(p string) bool { return strings.HasPrefix(name, p) }) {
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

// planHeader は、どの commit に対する plan かを返します（ADR-0602 決定10）。git が答えられなければ、
// どの commit の plan かを残せないので失敗します。
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
