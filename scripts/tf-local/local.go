package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/cientry"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// 人の手元の入口（local.mk）の段。1回の起動は1つの目的を持ち、その目的の道具だけを起動する（ADR-0702 決定24）。
//
//	local-prepare は aws だけを起動し、init に渡す値（Bucket 名と state の鍵）を算出して state のファイルに書く。
//	local-init は terraform だけを起動し、state のファイルの値で init する。
//	local-plan は terraform だけを起動し、lock を取らずに plan して、log を所有者だけが読める権限で作り直す。
//
// 起動の引数は段ごとに固定で、値は環境変数（AWS_REGION・TF_CLI_ARGS_init）と、aws の入力の JSON のファイルで渡す。

const (
	stepCallerIdentity = "sts-caller-identity"
	stepListAliases    = "kms-list-aliases"
	stepBucketEncrypt  = "s3api-get-bucket-encryption"
	stepLocalInit      = "terraform-init"
	stepLocalPlan      = "terraform-plan"
)

var (
	errLocalState = xerrors.New("❌ init の値の state を読めません")
	errLocalAWS   = xerrors.New("❌ aws の呼び出しが失敗しました")
	errLocalInit  = xerrors.New("❌ terraform init が失敗しました")
	errLocalPlan  = xerrors.New("❌ terraform plan が失敗しました")
	errLocalLog   = xerrors.New("❌ plan の log を書けません")
)

type toolFunc = toolrun.Func

// command は toolFunc の実物です。aws の可変の値は、作業ディレクトリに置いた input.json（--cli-input-json）と
// AWS_REGION で、terraform の可変の値は TF_CLI_ARGS_<サブコマンド> で渡します。
func command(ctx context.Context, dir string, env []string, step string, _ ...string) *exec.Cmd {
	var cmd *exec.Cmd
	switch step {
	case stepCallerIdentity:
		cmd = exec.CommandContext(ctx, "aws", "sts", "get-caller-identity", "--query", "[Account,Arn]", "--output", "text")
	case stepListAliases:
		cmd = exec.CommandContext(ctx, "aws", "kms", "list-aliases", "--output", "json")
	case stepBucketEncrypt:
		cmd = exec.CommandContext(ctx, "aws", "s3api", "get-bucket-encryption", "--cli-input-json", "file://input.json", "--output", "json")
	case stepAssumeRole:
		cmd = exec.CommandContext(ctx, "aws", "sts", "assume-role-with-web-identity", "--cli-input-json", "file://input.json", "--output", "json")
	case stepCIInit:
		cmd = exec.CommandContext(ctx, "terraform", "init", "-input=false", "-no-color", "-lockfile=readonly")
	case stepCIPlanNoLock:
		cmd = exec.CommandContext(ctx, "terraform", "plan", "-lock=false", "-input=false", "-no-color", "-detailed-exitcode")
	case stepCIPlanLock:
		cmd = exec.CommandContext(ctx, "terraform", "plan", "-input=false", "-no-color", "-detailed-exitcode")
	case stepCIShow:
		cmd = exec.CommandContext(ctx, "terraform", "show")
	case stepCIApply:
		cmd = exec.CommandContext(ctx, "terraform", "apply")
	case stepCIConftest:
		cmd = exec.CommandContext(ctx, "conftest", "test", "--combine", "--all-namespaces", "--no-color", "-o", "json",
			"--policy", "rego", "--data", "decl.json", "--data", "layer.json", "plan.json")
	case stepCIPutObject:
		cmd = exec.CommandContext(ctx, "aws", "s3api", "put-object", "--cli-input-json", "file://input.json", "--body", "body.txt", "--output", "json")
	case stepS3List:
		cmd = exec.CommandContext(ctx, "aws", "s3api", "list-objects-v2", "--cli-input-json", "file://input.json", "--no-paginate", "--output", "json")
	case stepS3GetObject:
		cmd = exec.CommandContext(ctx, "aws", "s3api", "get-object", "--cli-input-json", "file://input.json", "--output", "json", "lock.json")
	case stepS3Delete:
		cmd = exec.CommandContext(ctx, "aws", "s3api", "delete-object", "--cli-input-json", "file://input.json", "--output", "json")
	case stepCIPlanHold:
		cmd = exec.CommandContext(ctx, "terraform", "plan", "-input=false", "-no-color", "-parallelism=1")
	case stepLocalInit:
		cmd = exec.CommandContext(ctx, "terraform", "init", "-input=false", "-lockfile=readonly")
	default:
		cmd = exec.CommandContext(ctx, "terraform", "plan", "-lock=false", "-input=false", "-no-color")
	}
	cmd.Dir = dir
	cmd.Env = env

	return cmd
}

// localEnv は、aws と terraform に渡す環境です。資格情報・引数・対象の state・provider を変える変数を外し
// （envUnset と同じ集合）、bootstrap の profile と、provider の差し替えを封じる設定を足します。
// TF_CLI_CONFIG_FILE=/dev/null は dev_overrides による provider の差し替えを封じる（docs/setup/bootstrap.md 2-0b）。
func localEnv(environ []string, profile string, extra ...string) []string {
	env := make([]string, 0, len(environ)+4+len(extra))
	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")
		if !unset(name) {
			env = append(env, kv)
		}
	}
	env = append(env, "AWS_PROFILE="+profile, "TF_CLI_CONFIG_FILE=/dev/null", "AWS_EC2_METADATA_DISABLED=true", "CHECKPOINT_DISABLE=1")

	return append(env, extra...)
}

// localState は、local-prepare が書き、local-init が読む値です。アカウント ID を含むので、所有者だけが読める
// 権限で書きます。
type localState struct {
	Dir      string   `json:"dir"`
	Region   string   `json:"region"`
	InitArgs []string `json:"init_args"`
}

// localRoot は、env と root（identity か workload）から root のディレクトリを返します（ADR-0604 決定5）。
func localRoot(base, env, root string) (string, error) {
	if !tokenPattern.MatchString(env) {
		return "", errToken
	}
	switch r := cientry.Root(root); r {
	case cientry.RootIdentity, cientry.RootWorkload:
		return filepath.Join(base, filepath.FromSlash(r.Dir(env))), nil
	default:
		return "", xerrors.Wrap(errUsage, "root は identity か workload です")
	}
}

// runTool は、道具を起動して stdout を返します。stderr は人の端末へ出します。
func runTool(tool toolFunc, dir string, env []string, step string, stderr io.Writer) ([]byte, error) {
	var out bytes.Buffer
	cmd := tool(context.Background(), dir, env, step)
	cmd.Stdout, cmd.Stderr = &out, stderr
	code, err := toolrun.Exec(cmd)
	if err != nil {
		return nil, xerrors.Wrap(err, step)
	}
	if code != 0 {
		return nil, xerrors.Wrapf(errLocalAWS, "%s: exit status %d", step, code)
	}

	return out.Bytes(), nil
}

// localPrepare は、aws を起動して init に渡す値を算出し、out へ書きます。値を決められなければ何も書きません。
func localPrepare(args []string, d deps, stderr io.Writer) (string, error) {
	fs := flag.NewFlagSet("local-prepare", flag.ContinueOnError)
	fs.SetOutput(stderr)
	project := fs.String("project", "", "")
	profile := fs.String("profile", "", "")
	env := fs.String("env", "", "")
	root := fs.String("root", "", "")
	base := fs.String("base", "", "")
	missing := fs.String("missing", missingFail, "")
	out := fs.String("out", "", "")
	if err := fs.Parse(args); err != nil {
		return "", xerrors.Wrap(errUsage, err.Error())
	}
	if *project == "" || *profile == "" || *base == "" || *out == "" || fs.NArg() > 0 {
		return "", errUsage
	}
	dir, err := localRoot(*base, *env, *root)
	if err != nil {
		return "", err
	}
	region, err := rootRegion(dir, d.readFile)
	if err != nil {
		return "", err
	}
	work, err := os.MkdirTemp("", "tf-local-prepare-")
	if err != nil {
		return "", xerrors.Wrap(err, "作業ディレクトリ")
	}
	defer func() { _ = os.RemoveAll(work) }()
	awsEnv := localEnv(d.environ, *profile, "AWS_REGION="+region)

	identityOut, err := runTool(d.tool, work, awsEnv, stepCallerIdentity, stderr)
	if err != nil {
		return "", err
	}
	identity := strings.TrimSpace(string(identityOut))
	account, err := accountOf(identity)
	if err != nil {
		return "", err
	}
	bucket, err := bucketName(*project, *env, "tfstate", account, region)
	if err != nil {
		return "", err
	}
	aliases, err := runTool(d.tool, work, awsEnv, stepListAliases, stderr)
	if err != nil {
		return "", err
	}
	input, err := json.Marshal(map[string]string{"Bucket": bucket, "ExpectedBucketOwner": account})
	if err != nil {
		return "", xerrors.Wrap(err, "input.json")
	}
	if err := os.WriteFile(filepath.Join(work, "input.json"), input, 0o600); err != nil {
		return "", xerrors.Wrap(err, "input.json")
	}
	encryption, err := runTool(d.tool, work, awsEnv, stepBucketEncrypt, stderr)
	if err != nil {
		return "", err
	}
	kms, err := stateKey(*project, *env, region, account, aliases, encryption, *missing)
	if err != nil {
		return "", err
	}
	initArgs := []string{"-backend-config=bucket=" + bucket}
	if kms == "" {
		log.Print("⚠️ state の鍵が無いので、init に鍵を渡さず SSE-S3 で書きます（-missing sse-s3）")
	} else {
		initArgs = append(initArgs, kms)
	}
	raw, err := json.Marshal(localState{Dir: dir, Region: region, InitArgs: initArgs})
	if err != nil {
		return "", xerrors.Wrap(err, "state")
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o700); err != nil {
		return "", xerrors.Wrapf(err, "%s の作成", filepath.Dir(*out))
	}
	if err := os.WriteFile(filepath.Clean(*out), raw, 0o600); err != nil {
		return "", xerrors.Wrapf(err, "%s の書き込み", *out)
	}

	return "✅ init の値を " + *out + " に書きました", nil
}

// localInit は、local-prepare の値で terraform init を起動します。init の引数は TF_CLI_ARGS_init で渡します
// （外す変数に TF_CLI_ARGS を含めているので、渡すのはここで組んだ値だけです）。
func localInit(args []string, d deps, stdout, stderr io.Writer) (string, error) {
	fs := flag.NewFlagSet("local-init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	profile := fs.String("profile", "", "")
	statePath := fs.String("state", "", "")
	if err := fs.Parse(args); err != nil {
		return "", xerrors.Wrap(errUsage, err.Error())
	}
	if *profile == "" || *statePath == "" || fs.NArg() > 0 {
		return "", errUsage
	}
	raw, err := d.readFile(*statePath)
	if err != nil {
		return "", xerrors.Wrap(errLocalState, err.Error())
	}
	var s localState
	if err := json.Unmarshal(raw, &s); err != nil || s.Dir == "" || len(s.InitArgs) == 0 {
		return "", xerrors.Wrap(errLocalState, *statePath)
	}
	cmd := d.tool(context.Background(), s.Dir, localEnv(d.environ, *profile, "TF_CLI_ARGS_init="+strings.Join(s.InitArgs, " ")), stepLocalInit)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	code, err := toolrun.Exec(cmd)
	if err != nil {
		return "", xerrors.Wrap(err, "init")
	}
	if code != 0 {
		return "", xerrors.Wrapf(errLocalInit, "exit status %d", code)
	}

	return "✅ " + s.Dir + " を init しました", nil
}

// localPlan は、lock を取らずに plan を起動し（ADR-0604 決定8）、header と出力を log に書き、端末にも出します。
// log はアカウント ID を含むので、所有者だけが読める権限で毎回作り直します。
func localPlan(args []string, d deps, stdout, stderr io.Writer) (string, error) {
	fs := flag.NewFlagSet("local-plan", flag.ContinueOnError)
	fs.SetOutput(stderr)
	profile := fs.String("profile", "", "")
	env := fs.String("env", "", "")
	root := fs.String("root", "", "")
	base := fs.String("base", "", "")
	headerPath := fs.String("header", "", "")
	logPath := fs.String("log", "", "")
	if err := fs.Parse(args); err != nil {
		return "", xerrors.Wrap(errUsage, err.Error())
	}
	if *profile == "" || *base == "" || *headerPath == "" || *logPath == "" || fs.NArg() > 0 {
		return "", errUsage
	}
	dir, err := localRoot(*base, *env, *root)
	if err != nil {
		return "", err
	}
	header, err := d.readFile(*headerPath)
	if err != nil {
		return "", xerrors.Wrap(errLocalLog, err.Error())
	}
	if err := os.MkdirAll(filepath.Dir(*logPath), 0o700); err != nil {
		return "", xerrors.Wrap(errLocalLog, err.Error())
	}
	if err := os.Remove(*logPath); err != nil && !os.IsNotExist(err) {
		return "", xerrors.Wrap(errLocalLog, err.Error())
	}
	f, err := os.OpenFile(filepath.Clean(*logPath), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", xerrors.Wrap(errLocalLog, err.Error())
	}
	defer func() { _ = f.Close() }()
	w := io.MultiWriter(stdout, f)
	if _, err := fmt.Fprintf(w, "%s\n", strings.TrimSpace(string(header))); err != nil {
		return "", xerrors.Wrap(errLocalLog, err.Error())
	}
	cmd := d.tool(context.Background(), dir, localEnv(d.environ, *profile), stepLocalPlan)
	cmd.Stdout, cmd.Stderr = w, w
	code, err := toolrun.Exec(cmd)
	if err != nil {
		return "", xerrors.Wrap(err, "plan")
	}
	if code != 0 {
		return "", xerrors.Wrapf(errLocalPlan, "exit status %d", code)
	}

	return "✅ plan の出力を " + *logPath + " に残しました", nil
}
