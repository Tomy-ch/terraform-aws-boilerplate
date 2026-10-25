// Package main は、人が手元から Terraform を動かす入口（.makefiles/terraform/operation/local.mk）が
// terraform に渡す値を組み立てて検証するツール。
//
//	tf-local region <root>
//	    <root>/backend.tf の backend の region を1行で出す。<root>/providers.tf の provider の
//	    region と違えば失敗する（Bucket 名の region は provider の region から決まる）
//	tf-local bucket -project <p> -env <e> -region <r>
//	    標準入力の `aws sts get-caller-identity --query '[Account,Arn]' --output text` の出力から、
//	    state Bucket の名前を1行で出す
//
// aws も terraform も起動しない。起動は make が行い、ここは値だけを返す（ADR-0702 決定3）。
// 値を決められない入力はすべて非ゼロで終わり、make はその時点で terraform を呼ばない。
//
// backend block は式を持てないので、Bucket 名の規則をここに写す。正本は modules/s3 と
// usecases/state-backend で、写しのずれは make tf-local-test が env/admin/identity のテストの
// 期待値と突き合わせて見る。state-backend が組んだ名前を init の前に読めるようになったら、写しを消す。
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

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var (
	regionLine     = regexp.MustCompile(`^\s*region\s*=\s*"([^"]*)"`)
	regionPattern  = regexp.MustCompile(`^[a-z]{2}(-[a-z]+)+-[0-9]$`)
	accountPattern = regexp.MustCompile(`^[0-9]{12}$`)
	tokenPattern   = regexp.MustCompile(`^[a-z0-9]+$`)

	errUsage            = xerrors.New("❌ usage: tf-local region <root> | tf-local bucket -project <p> -env <e> -region <r>")
	errBackendMissing   = xerrors.New("❌ backend.tf がありません")
	errProvidersMissing = xerrors.New("❌ providers.tf がありません")
	errRegionMismatch   = xerrors.New("❌ provider と backend の region が違います")
	errRegionNotOne     = xerrors.New("❌ backend.tf の region が1つではありません")
	errRegionMalformed  = xerrors.New("❌ region の形が不正です")
	errIdentity         = xerrors.New("❌ アカウント ID を取得できません（make aws-login を実行してください）")
	errRootCredentials  = xerrors.New("❌ root の資格情報では実行しません")
	errToken            = xerrors.New("❌ project と env は英小文字と数字だけです")
)

func main() {
	log.SetFlags(0)

	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.ReadFile); err != nil {
		log.Fatalf("%v", err)
	}
}

func run(args []string, in io.Reader, out io.Writer, readFile func(string) ([]byte, error)) error {
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

		value, err = rootRegion(args[1], readFile)
	case "bucket":
		value, err = runBucket(args[1:], in)
	default:
		return xerrors.Wrap(errUsage, args[0])
	}

	if err != nil {
		return err
	}

	_, err = fmt.Fprintln(out, value)

	return err
}

func runBucket(args []string, in io.Reader) (string, error) {
	fs := flag.NewFlagSet("bucket", flag.ContinueOnError)
	project := fs.String("project", "", "")
	env := fs.String("env", "", "")
	region := fs.String("region", "", "")

	if err := fs.Parse(args); err != nil {
		return "", xerrors.Wrap(errUsage, err.Error())
	}

	if fs.NArg() > 0 {
		return "", xerrors.Wrap(errUsage, strings.Join(fs.Args(), " "))
	}

	identity, err := io.ReadAll(in)
	if err != nil {
		return "", xerrors.Wrap(err, "標準入力の読み取り")
	}

	account, err := callerAccount(string(identity))
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

// backendRegion は、0件・複数件・形の誤りを、どれか1つを選ばずにエラーにします。選んだ値が実際の Bucket の region と違えば、別の名前の
// Bucket へ init することになるためです。
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

// bucketName は、state Bucket の完全な名前を組みます（modules/s3 の `<name>-<account_id>-<region>-an`、
// name は usecases/state-backend の `<project>-<env>-tfstate`）。
func bucketName(project, env, account, region string) (string, error) {
	if !tokenPattern.MatchString(project) || !tokenPattern.MatchString(env) {
		return "", xerrors.Wrap(errToken, fmt.Sprintf("project=%q env=%q", project, env))
	}

	if !regionPattern.MatchString(region) {
		return "", xerrors.Wrap(errRegionMalformed, region)
	}

	return fmt.Sprintf("%s-%s-tfstate-%s-%s-an", project, env, account, region), nil
}
