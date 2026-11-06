package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// policyNamespace は、CI の plan と apply が掛ける Policy です（ADR-0604 決定11）。ほかの package は
// mock の入力を前提にしているので、実 provider の plan には掛けない。
const policyNamespace = "plan.stateful_protection"

var (
	errTool       = xerrors.New("❌ 道具を起動できません")
	errInit       = xerrors.New("❌ init が失敗しました。全文はランナーの外に出しません")
	errPlanBucket = xerrors.New("❌ plan の出力の Bucket の名前を state から読めません")
	errPlanFailed = xerrors.New("❌ plan が失敗しました")
	errShow       = xerrors.New("❌ plan を JSON にできません")
	errPutObject  = xerrors.New("❌ plan の全文を plan の出力の Bucket に置けません")
	errPolicy     = xerrors.New("❌ ADR-0604 決定11・12 の Policy を通りません（理由は上の行）")
	errApply      = xerrors.New("❌ apply が失敗しました。全文はランナーの外に出しません")
)

// ciConfig は、make が渡す CI の入口の設定です。道具は名前か path で受け、PATH から解きます。
type ciConfig struct {
	project, env, root, profile string
	base, dir                   string
	terraform, aws, conftest    string
	policyDir, decl             string
	pr, runID, attempt          string
}

// ciRun は、1回の CI の plan か apply が使う値と、作業ファイルです。
type ciRun struct {
	cfg             ciConfig
	d               deps
	out             io.Writer
	name, rootDir   string
	log, json, plan string
	region, account string
}

func parseCI(name string, args []string, apply bool) (ciConfig, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var c ciConfig

	for p, n := range map[*string]string{
		&c.project: "project", &c.env: "env", &c.root: "root", &c.profile: "profile", &c.base: "base", &c.dir: "dir",
		&c.terraform: "terraform", &c.aws: "aws", &c.conftest: "conftest", &c.policyDir: "policy", &c.decl: "decl",
	} {
		fs.StringVar(p, n, "", "")
	}

	if !apply {
		fs.StringVar(&c.pr, "pr", "", "")
		fs.StringVar(&c.runID, "run", "", "")
		fs.StringVar(&c.attempt, "attempt", "", "")
	}

	if err := fs.Parse(args); err != nil {
		return ciConfig{}, xerrors.Wrap(errUsage, err.Error())
	}

	required := []string{c.project, c.env, c.root, c.profile, c.base, c.dir, c.terraform, c.aws, c.conftest, c.policyDir, c.decl}
	if !apply {
		required = append(required, c.pr, c.runID, c.attempt)
	}

	if fs.NArg() != 0 || c.root != "identity" && c.root != "workload" || !tokenPattern.MatchString(c.project) || !tokenPattern.MatchString(c.env) {
		return ciConfig{}, xerrors.Wrap(errUsage, name)
	}

	for _, v := range required {
		if v == "" {
			return ciConfig{}, xerrors.Wrap(errUsage, name)
		}
	}

	return c, nil
}

// runCI は、CI の plan か apply を1回行います。init までの準備は両者で同じにし、root の名前は decl.json の
// allow の鍵と同じ `<env>-<root>` にする。
func runCI(name string, args []string, out io.Writer, d deps) error {
	apply := name == "ci-apply"

	cfg, err := parseCI(name, args, apply)
	if err != nil {
		return err
	}

	r := &ciRun{cfg: cfg, d: d, out: out, name: cfg.env + "-" + cfg.root, rootDir: filepath.Join(cfg.base, "env", cfg.env)}
	if cfg.root == "identity" {
		r.rootDir = filepath.Join(r.rootDir, "identity")
	}

	if err := ciPrepare(r); err != nil {
		return err
	}

	if apply {
		return ciApply(r)
	}

	return ciPlan(r)
}

// ciPrepare は、作業ファイルを所有者だけが読める形で作り直し、plan した commit を出してから init します。
func ciPrepare(r *ciRun) error {
	if err := os.MkdirAll(r.cfg.dir, 0o700); err != nil {
		return xerrors.Wrap(errTool, err.Error())
	}

	r.log = filepath.Join(r.cfg.dir, r.name+".txt")
	r.json = filepath.Join(r.cfg.dir, r.name+".json")
	r.plan = filepath.Join(r.cfg.dir, r.name+".tfplan")

	for _, f := range []string{r.log, r.json, r.plan} {
		if err := os.Remove(f); err != nil && !errors.Is(err, os.ErrNotExist) {
			return xerrors.Wrap(errTool, err.Error())
		}
	}

	header, err := planHeader(r.d.git)
	if err != nil {
		return err
	}

	if err := os.WriteFile(r.log, []byte(header+"\n"), 0o600); err != nil {
		return xerrors.Wrap(errTool, err.Error())
	}

	fmt.Fprintln(r.out, header)

	if r.region, err = rootRegion(r.rootDir, r.d.readFile); err != nil {
		return err
	}

	var identity bytes.Buffer
	if rc, err := ciTool(r, r.cfg.aws, []string{"sts", "get-caller-identity", "--region", r.region, "--query", "[Account,Arn]", "--output", "text"}, &identity, r.d.stderr); err != nil || rc != 0 {
		return xerrors.Wrap(errIdentity, "get-caller-identity")
	}

	if r.account, err = callerAccount(identity.String()); err != nil {
		return err
	}

	bucket, err := bucketName(r.cfg.project, r.cfg.env, r.account, r.region)
	if err != nil {
		return err
	}

	return withLog(r, func(log io.Writer) error {
		rc, err := ciTool(r, r.cfg.terraform, []string{"-chdir=" + r.rootDir, "init", "-input=false", "-no-color", "-lockfile=readonly", "-backend-config=bucket=" + bucket}, log, log)
		if err != nil || rc != 0 {
			return errInit
		}

		return nil
	})
}

// ciPlan は lock を取らずに plan し（ADR-0604 決定8）、plan が失敗しても、Bucket に書けるなら全文を置いてから
// 失敗します。公開されるログには、終了コード・件数・object の key・SHA-256・版 ID と Policy の違反の文だけを
// 出す（ADR-0602 決定13）。
func ciPlan(r *ciRun) error {
	var bucket bytes.Buffer

	err := withLog(r, func(log io.Writer) error {
		if rc, err := ciTool(r, r.cfg.terraform, []string{"-chdir=" + r.rootDir, "output", "-raw", "plan_output_bucket_name"}, &bucket, log); err != nil || rc != 0 {
			return errPlanBucket
		}

		return nil
	})
	if err != nil {
		return err
	}

	rc, err := ciTerraformPlan(r, "-lock=false")
	if err != nil {
		return err
	}

	if rc != 1 && ciShow(r) != nil {
		rc = 1
	}

	key, err := planKey(r.cfg.pr, "", r.cfg.runID, r.cfg.attempt, r.name)
	if err != nil {
		return err
	}

	body, err := os.ReadFile(r.log)
	if err != nil {
		return xerrors.Wrap(errTool, err.Error())
	}

	sum := sha256.Sum256(body)
	sha := base64.StdEncoding.EncodeToString(sum[:])
	fmt.Fprintf(r.out, "plan の全文: key=%s sha256=%s\n", key, sha)

	var version bytes.Buffer
	if prc, err := ciTool(r, r.cfg.aws, []string{"s3api", "put-object", "--region", r.region, "--bucket", strings.TrimSpace(bucket.String()),
		"--key", key, "--body", r.log, "--checksum-sha256", sha, "--expected-bucket-owner", r.account, "--query", "VersionId", "--output", "text"},
		&version, r.d.stderr); err != nil || prc != 0 {
		return errPutObject
	}

	fmt.Fprintf(r.out, "plan の全文の版 ID: %s\n", strings.TrimSpace(version.String()))

	if err := ciRemove(r.plan, r.log); err != nil {
		return err
	}

	if rc == 1 {
		return xerrors.Wrap(errPlanFailed, "全文は上の key にあります")
	}

	summary, err := ciPolicy(r, rc)
	if err != nil {
		return err
	}

	return ciStepSummary(r, fmt.Sprintf("### %s\n\n%s\n\n- key: `%s`\n- sha256: `%s`\n- 版 ID: `%s`\n", r.name, summary, key, sha, strings.TrimSpace(version.String())))
}

// ciApply は、apply する commit で plan し直し、Policy を通った plan だけを apply します（ADR-0601 決定6、
// ADR-0604 決定11）。plan は lock を取る —— ADR-0604 決定8 の plan は plan の job の plan である。変更の無い
// plan も apply し、経路と lock の取得を毎回同じにする。全文はランナーの外に出さない。
func ciApply(r *ciRun) error {
	rc, err := ciTerraformPlan(r)
	if err != nil {
		return err
	}

	if rc == 1 {
		return xerrors.Wrap(errPlanFailed, "全文はランナーの外に出しません")
	}

	if err := ciShow(r); err != nil {
		return err
	}

	summary, err := ciPolicy(r, rc)
	if err != nil {
		return err
	}

	var arc int

	err = withLog(r, func(log io.Writer) error {
		var err error

		arc, err = ciTool(r, r.cfg.terraform, []string{"-chdir=" + r.rootDir, "apply", "-input=false", "-no-color", r.plan}, log, log)

		return err
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(r.out, "apply の終了コード: %d\n", arc)

	if err := ciRemove(r.plan, r.log); err != nil {
		return err
	}

	if arc != 0 {
		return errApply
	}

	return ciStepSummary(r, fmt.Sprintf("### %s\n\n%s\n\napply の終了コード: %d\n", r.name, summary, arc))
}

// ciTerraformPlan は、plan の終了コードを出して返します。終了コードの意味は呼び出し側が決める。
func ciTerraformPlan(r *ciRun, extra ...string) (int, error) {
	args := append([]string{"-chdir=" + r.rootDir, "plan"}, extra...)
	args = append(args, "-input=false", "-no-color", "-detailed-exitcode", "-out="+r.plan)

	var rc int

	err := withLog(r, func(log io.Writer) error {
		var err error

		rc, err = ciTool(r, r.cfg.terraform, args, log, log)

		return err
	})
	if err != nil {
		return 0, err
	}

	fmt.Fprintf(r.out, "plan の終了コード: %d\n", rc)

	return rc, nil
}

func ciShow(r *ciRun) error {
	f, err := os.OpenFile(r.json, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return xerrors.Wrap(errTool, err.Error())
	}
	defer func() { _ = f.Close() }()

	return withLog(r, func(log io.Writer) error {
		rc, err := ciTool(r, r.cfg.terraform, []string{"-chdir=" + r.rootDir, "show", "-json", r.plan}, f, log)
		if err != nil || rc != 0 {
			return errShow
		}

		return nil
	})
}

// ciPolicy は、plan の終了コードと JSON の変更を突き合わせた要約を出し、Policy を掛けます。conftest の
// 終了コードは見ず、評価した規則が rego の `deny contains` の数でなければ失敗する（policyResult）。
func ciPolicy(r *ciRun, rc int) (string, error) {
	data, err := r.d.readFile(r.json)
	if err != nil {
		return "", xerrors.Wrap(errPlanJSON, "ファイルを読めません")
	}

	summary, err := planSummary(strconv.Itoa(rc), data)
	if err != nil {
		return "", err
	}

	fmt.Fprintln(r.out, summary)

	rego, err := r.d.readFile(filepath.Join(r.cfg.policyDir, "stateful_protection.rego"))
	if err != nil {
		return "", xerrors.Wrap(errPolicy, err.Error())
	}

	var result bytes.Buffer
	if _, err := ciTool(r, r.cfg.conftest, []string{"test", "--combine", "--no-color", "-o", "json", "--namespace", policyNamespace,
		"--policy", r.cfg.policyDir, "--data", r.cfg.decl, r.json}, &result, r.d.stderr); err != nil {
		return "", err
	}

	line, err := policyResult(policyNamespace, countRules(rego), result.Bytes())
	if err != nil {
		fmt.Fprintln(r.out, err.Error())

		return "", errPolicy
	}

	fmt.Fprintln(r.out, line)

	return summary, nil
}

// countRules は、rego の `deny contains` で始まる行を数えます。
func countRules(rego []byte) int {
	n := 0

	for line := range strings.SplitSeq(string(rego), "\n") {
		if strings.HasPrefix(line, "deny contains") {
			n++
		}
	}

	return n
}

// ciTool は、道具を AWS_* などを外した環境で起動し、終了コードを返します。起動できなければ失敗します。
func ciTool(r *ciRun, tool string, args []string, stdout, stderr io.Writer) (int, error) {
	cmd := exec.CommandContext(context.Background(), tool, args...) //nolint:gosec // 道具と引数は make と ciRun が組む
	cmd.Env = childEnv(r.d.environ, r.cfg.profile)
	cmd.Stdout, cmd.Stderr = stdout, stderr

	err := cmd.Run()

	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), nil
	}

	if err != nil {
		return 0, xerrors.Wrap(errTool, filepath.Base(tool))
	}

	return 0, nil
}

// childEnv は、envUnset が外す変数を除き、CI の profile だけで aws と terraform を動かす環境を返します。
// TF_CLI_CONFIG_FILE=/dev/null は dev_overrides による provider の差し替えを封じる。
func childEnv(environ []string, profile string) []string {
	out := make([]string, 0, len(environ)+4)

	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")
		if !unset(name) {
			out = append(out, kv)
		}
	}

	return append(out, "AWS_PROFILE="+profile, "TF_CLI_CONFIG_FILE=/dev/null", "AWS_EC2_METADATA_DISABLED=true", "CHECKPOINT_DISABLE=1")
}

// withLog は、terraform の出力を全文のファイルへ足します。
func withLog(r *ciRun, fn func(io.Writer) error) error {
	f, err := os.OpenFile(r.log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return xerrors.Wrap(errTool, err.Error())
	}

	if err := fn(f); err != nil {
		_ = f.Close()

		return err
	}

	if err := f.Close(); err != nil {
		return xerrors.Wrap(errTool, err.Error())
	}

	return nil
}

func ciRemove(paths ...string) error {
	for _, p := range paths {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return xerrors.Wrap(errTool, err.Error())
		}
	}

	return nil
}

// ciStepSummary は、GITHUB_STEP_SUMMARY が在るときだけ、要約をそこへ足します。
func ciStepSummary(r *ciRun, text string) error {
	path := ""

	for _, kv := range r.d.environ {
		if v, ok := strings.CutPrefix(kv, "GITHUB_STEP_SUMMARY="); ok {
			path = v
		}
	}

	if path == "" {
		return nil
	}

	f, err := os.OpenFile(filepath.Clean(path), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return xerrors.Wrap(errTool, err.Error())
	}

	if _, err := io.WriteString(f, text); err != nil {
		_ = f.Close()

		return xerrors.Wrap(errTool, err.Error())
	}

	if err := f.Close(); err != nil {
		return xerrors.Wrap(errTool, err.Error())
	}

	return nil
}
