package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/cientry"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/policyresult"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// CI の plan と apply の入口の段（ci.mk の CI_PLAN と CI_APPLY）。1回の起動は1つの目的を持ち、その目的の道具だけを
// 固定の引数で起動する（ADR-0702 決定3・24）。段の間の値は state のファイル（所有者だけが読める）が運ぶ。
//
//	ci-prepare は aws だけを起動し、init の値・出力の Bucket・key を求め、全文の1行目を書く。
//	ci-init は terraform init、ci-plan は terraform plan と show、ci-apply は terraform apply を起動する。
//	ci-policy は conftest を起動し、plan に入口の名前空間の Policy を掛ける。
//	ci-upload は aws だけを起動し、全文を出力の Bucket に置く。
//	ci-finish は道具を起動せず、job summary と GITHUB_OUTPUT を書いて、state の判定で終わる。
//
// prepare と init が落ちれば、make はそこで止まる（全文はランナーの外に出さない）。それより後の段は make の `-` で
// 続き、前の段の失敗を state で知って何もしない。upload は必ず走り、finish が最後に落とす。全文は公開される
// ログに出さず、ログには終了コード・件数・key・SHA-256・版 ID と Policy の違反の文だけを出す（ADR-0602 決定13）。
// plan と apply の段の失敗の種類（state の stage）は scripts/apply-revert の revertKinds と skipKinds が読む。

const (
	stepCIInit       = "ci-terraform-init"
	stepCIPlanNoLock = "ci-terraform-plan-nolock"
	stepCIPlanLock   = "ci-terraform-plan"
	stepCIShow       = "ci-terraform-show"
	stepCIApply      = "ci-terraform-apply"
	stepCIConftest   = "ci-conftest"
	stepCIPutObject  = "s3api-put-object"

	ciModePlan  = "plan"
	ciModeApply = "apply"

	stagePrepare = "prepare"
	stageInit    = "init"
	stagePlan    = "plan"
	stagePolicy  = "policy"
	stageApply   = "apply"
	stageUpload  = "upload"

	// applyTimeout は、apply を SIGINT で打ち切るまでの時間。apply の job の timeout-minutes（applyJobTimeout）より、
	// init・plan と、打ち切りの後に terraform が state を書いて lock を外し、全文を置くまでの分だけ短くする。job の
	// timeout が先に来ると全文を置けない。
	applyTimeout = 20 * time.Minute
)

var (
	errCIState    = xerrors.New("❌ CI の入口の state を読めません")
	errCIInit     = xerrors.New("❌ init が失敗しました。全文はランナーの外に出しません")
	errCIHeader   = xerrors.New("❌ plan した commit の行を読めません")
	errCIPolicy   = xerrors.New("❌ Policy を通りません（理由は上の行）")
	errCIUpload   = xerrors.New("❌ 全文を出力の Bucket に置けません")
	errCIUnfinish = xerrors.New("❌ 終わっていない段があります")
	errCIFailed   = xerrors.New("❌ 全文は上の key にあります")
	errCIOutput   = xerrors.New("❌ job summary か GITHUB_OUTPUT に書けません")
)

// ciState は、段の間で運ぶ値です。アカウント ID を含むので、所有者だけが読める権限で書きます。
type ciState struct {
	Mode      string   `json:"mode"`
	Root      string   `json:"root"`
	Dir       string   `json:"dir"`
	Profile   string   `json:"profile"`
	Region    string   `json:"region"`
	Account   string   `json:"account"`
	InitArgs  []string `json:"init_args"`
	OutBucket string   `json:"out_bucket"`
	Key       string   `json:"key"`
	Out       string   `json:"out"`
	JSON      string   `json:"json"`
	TFPlan    string   `json:"tfplan"`
	Done      []string `json:"done"`
	// Stage は失敗した段の種類。空なら失敗していない。
	Stage   string `json:"stage"`
	Applied bool   `json:"applied"`
	Summary string `json:"summary"`
	SHA     string `json:"sha"`
	Version string `json:"version"`
}

func loadCIState(path string) (ciState, error) {
	raw, err := readInRoot(path)
	if err != nil {
		return ciState{}, xerrors.Wrap(errCIState, err.Error())
	}
	var s ciState
	if err := json.Unmarshal(raw, &s); err != nil || (s.Mode != ciModePlan && s.Mode != ciModeApply && s.Mode != ciModeHold) || s.Dir == "" || s.Out == "" {
		return ciState{}, xerrors.Wrap(errCIState, path)
	}

	return s, nil
}

// readInRoot は、path をそのディレクトリの外へ出ない形で読みます（名前空間から組んだ名前を含むため）。
func readInRoot(path string) ([]byte, error) {
	r, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()

	return r.ReadFile(filepath.Base(path))
}

// openInRoot は、path を所有者だけが読める権限で、そのディレクトリの外へ出ない形で開きます。
func openInRoot(path string, flag int) (*os.File, error) {
	r, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()

	return r.OpenFile(filepath.Base(path), flag, 0o600)
}

func (s ciState) save(path string) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return xerrors.Wrap(errCIState, err.Error())
	}

	return xerrors.Wrap(writeFileAtomic(path, raw), "state")
}

func (s *ciState) done(step string) {
	if !slices.Contains(s.Done, step) {
		s.Done = append(s.Done, step)
	}
}

// fail は失敗の種類を残します。先に残した種類を上書きしません。
func (s *ciState) fail(stage string) {
	if s.Stage == "" {
		s.Stage = stage
	}
}

// remove は、全文・plan の JSON・plan のファイルを消します。全文はアカウント ID を含む。
func (s ciState) remove() {
	for _, p := range []string{s.Out, s.JSON, s.TFPlan} {
		if p != "" {
			_ = os.Remove(p)
		}
	}
}

// writeFailure は、apply の入口の失敗の種類を GITHUB_OUTPUT の failure に書きます。GITHUB_OUTPUT が無ければ書きません。
func writeFailure(environ []string, stage string) error {
	return appendGitHub(envValue(environ, "GITHUB_OUTPUT"), fmt.Sprintf("failure=%s\n", stage))
}

func appendGitHub(path, text string) error {
	if path == "" {
		return nil
	}
	f, err := os.OpenFile(filepath.Clean(path), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return xerrors.Wrap(errCIOutput, err.Error())
	}
	if _, err := f.WriteString(text); err != nil {
		_ = f.Close()

		return xerrors.Wrap(errCIOutput, err.Error())
	}

	return xerrors.Wrap(f.Close(), "GitHub のファイル")
}

// ciStatePaths は、state のファイルの名前から、全文・plan の JSON・plan のファイルの置き場を決めます。
func ciStatePaths(state string) (string, string, string) {
	base := strings.TrimSuffix(state, ".json")

	return base + ".txt", base + ".plan.json", base + ".tfplan"
}

// ciOutputBucketKind は、出力の Bucket の種類です。plan の全文と identity root の apply の全文は plan の出力の
// Bucket、ほかの root の apply の全文は apply の出力の Bucket に置く（.github/workflows/README.md の「apply の job」）。
func ciOutputBucketKind(mode, root string) string {
	if mode == ciModeApply && root == "workload" {
		return "tfapply"
	}

	return "tfplan"
}

// ciPrepareStep は ci-prepare です。値を決められなければ state を書かず、apply の入口なら failure に prepare を書く。
func ciPrepareStep(args []string, d deps, stdout io.Writer) (string, error) {
	fs := flag.NewFlagSet("ci-prepare", flag.ContinueOnError)
	fs.SetOutput(d.stderr)
	mode := fs.String("mode", "", "")
	project := fs.String("project", "", "")
	env := fs.String("env", "", "")
	root := fs.String("root", "", "")
	profile := fs.String("profile", "", "")
	base := fs.String("base", "", "")
	header := fs.String("header", "", "")
	statePath := fs.String("state", "", "")
	if err := fs.Parse(args); err != nil {
		return "", xerrors.Wrap(errUsage, err.Error())
	}
	if (*mode != ciModePlan && *mode != ciModeApply && *mode != ciModeHold) || *project == "" || *profile == "" || *base == "" ||
		*header == "" || *statePath == "" || fs.NArg() > 0 {
		return "", errUsage
	}
	// terraform は root のディレクトリで起動するので、plan のファイルの置き場は絶対パスで渡す。
	abs, err := filepath.Abs(*statePath)
	if err != nil {
		return "", xerrors.Wrap(errCIState, err.Error())
	}
	s, err := ciPrepareState(d, stdout, *mode, *project, *env, *root, *profile, *base, *header, abs)
	if err != nil {
		s.remove()
		if *mode == ciModeApply {
			if werr := writeFailure(d.environ, stagePrepare); werr != nil {
				return "", errors.Join(err, werr)
			}
		}

		return "", err
	}

	return "✅ " + s.Root + " の init の値と全文の置き場を決めました", nil
}

func ciPrepareState(d deps, stdout io.Writer, mode, project, env, root, profile, base, header, statePath string) (ciState, error) {
	out, jsonPath, tfplan := ciStatePaths(statePath)
	s := ciState{Mode: mode, Root: env + "-" + root, Profile: profile, Out: out, JSON: jsonPath, TFPlan: tfplan}
	for _, p := range []string{out, jsonPath, tfplan} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return s, xerrors.Wrap(errCIState, err.Error())
		}
	}
	dir, err := localRoot(base, env, root)
	if err != nil {
		return s, err
	}
	s.Dir = dir
	run, attempt := envValue(d.environ, "GITHUB_RUN_ID"), envValue(d.environ, "GITHUB_RUN_ATTEMPT")
	switch mode {
	case ciModePlan:
		s.Key, err = planKey(envValue(d.environ, "CI_PR"), "", run, attempt, s.Root)
	case ciModeApply:
		s.Key, err = applyKey(run, attempt, s.Root)
	}
	if err != nil {
		return s, err
	}
	line, err := d.readFile(header)
	if err != nil || strings.TrimSpace(string(line)) == "" {
		return s, errCIHeader
	}
	text := strings.TrimSpace(string(line)) + "\n"
	if err := os.MkdirAll(filepath.Dir(out), 0o700); err != nil {
		return s, xerrors.Wrap(errCIState, err.Error())
	}
	if err := os.WriteFile(filepath.Clean(out), []byte(text), 0o600); err != nil {
		return s, xerrors.Wrap(errCIState, err.Error())
	}
	if _, err := io.WriteString(stdout, text); err != nil {
		return s, err
	}
	if s.Region, err = rootRegion(dir, d.readFile); err != nil {
		return s, err
	}
	work, err := os.MkdirTemp(filepath.Dir(out), "prepare-")
	if err != nil {
		return s, xerrors.Wrap(errCIState, err.Error())
	}
	defer func() { _ = os.RemoveAll(work) }()
	awsEnv := localEnv(d.environ, profile, "AWS_REGION="+s.Region)
	identity, err := runTool(d.tool, work, awsEnv, stepCallerIdentity, d.stderr)
	if err != nil {
		return s, err
	}
	if s.Account, err = accountOf(strings.TrimSpace(string(identity))); err != nil {
		return s, err
	}
	bucket, err := bucketName(project, env, "tfstate", s.Account, s.Region)
	if err != nil {
		return s, err
	}
	if mode != ciModeHold {
		if s.OutBucket, err = bucketName(project, env, ciOutputBucketKind(mode, root), s.Account, s.Region); err != nil {
			return s, err
		}
	}
	aliases, err := runTool(d.tool, work, awsEnv, stepListAliases, d.stderr)
	if err != nil {
		return s, err
	}
	input, err := json.Marshal(map[string]string{"Bucket": bucket, "ExpectedBucketOwner": s.Account})
	if err != nil {
		return s, xerrors.Wrap(err, "input.json")
	}
	if err := os.WriteFile(filepath.Join(work, "input.json"), input, 0o600); err != nil {
		return s, xerrors.Wrap(err, "input.json")
	}
	encryption, err := runTool(d.tool, work, awsEnv, stepBucketEncrypt, d.stderr)
	if err != nil {
		return s, err
	}
	kms, err := stateKey(project, env, s.Region, s.Account, aliases, encryption, missingFail)
	if err != nil {
		return s, err
	}
	s.InitArgs = []string{"-backend-config=bucket=" + bucket, kms}
	s.done(stagePrepare)

	return s, s.save(statePath)
}

// ciStepArgs は、-state だけを取る段の引数を読みます。
func ciStepArgs(name string, args []string, d deps, more func(*flag.FlagSet)) (string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(d.stderr)
	statePath := fs.String("state", "", "")
	if more != nil {
		more(fs)
	}
	if err := fs.Parse(args); err != nil {
		return "", xerrors.Wrap(errUsage, err.Error())
	}
	if *statePath == "" || fs.NArg() > 0 {
		return "", errUsage
	}

	return *statePath, nil
}

// appendOut は、全文に追記するファイルを開きます。
func appendOut(s ciState) (*os.File, error) {
	f, err := openInRoot(s.Out, os.O_WRONLY|os.O_APPEND)

	return f, xerrors.Wrap(err, "全文")
}

// ciInitStep は ci-init です。init の出力は全文にだけ書きます。落ちれば全文を消し、apply の入口なら failure に
// prepare を書きます。
func ciInitStep(args []string, d deps) (string, error) {
	statePath, err := ciStepArgs("ci-init", args, d, nil)
	if err != nil {
		return "", err
	}
	s, err := loadCIState(statePath)
	if err != nil {
		return "", err
	}
	if err := ciRunInit(s, d); err != nil {
		s.remove()
		if s.Mode == ciModeApply {
			if werr := writeFailure(d.environ, stagePrepare); werr != nil {
				return "", errors.Join(err, werr)
			}
		}

		return "", err
	}
	s.done(stageInit)

	return "✅ init が終わりました", s.save(statePath)
}

func ciRunInit(s ciState, d deps) error {
	if !slices.Contains(s.Done, stagePrepare) || len(s.InitArgs) == 0 {
		return xerrors.Wrap(errCIState, "prepare が終わっていません")
	}
	f, err := appendOut(s)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	cmd := d.tool(s.Dir, localEnv(d.environ, s.Profile, "TF_CLI_ARGS_init="+strings.Join(s.InitArgs, " ")), stepCIInit)
	cmd.Stdout, cmd.Stderr = f, f
	if err := cmd.Run(); err != nil {
		return errCIInit
	}

	return nil
}

// exitCode は、道具の終了コードを返します。起動できなければ失敗します。
func exitCode(err error) (int, error) {
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0, nil
	case errors.As(err, &exit):
		return exit.ExitCode(), nil
	default:
		return 0, err
	}
}

// failureKind は、plan か apply の段の失敗の種類を返します。種類は revert の対象かの判定（scripts/apply-revert）が
// 読む。plan と apply は state の lock を取るので、終了コード 1 で全文が lock の取得の失敗を述べていれば lock とする。
// apply は、0 なら applied（apply は終わり、それより後の失敗は commit の中身に依らない）、打ち切りなら timeout とする。
func failureKind(step string, code int, out []byte) string {
	switch {
	case step == stageApply && code == 0:
		return "applied"
	case step == stageApply && code == applyTimedOut:
		return "timeout"
	case code == 1 && bytes.Contains(out, []byte(lockError)):
		return "lock"
	}

	return step
}

// ciPlanStep は ci-plan です。lock の扱いは登録簿の種別が持つ。apply の入口の plan は lock を取る —— ADR-0604
// 決定8 の plan は plan の job の plan である。plan が失敗しても show を試し、判定は planSummary が持つ。
func ciPlanStep(args []string, d deps, stdout io.Writer) (string, error) {
	statePath, err := ciStepArgs("ci-plan", args, d, nil)
	if err != nil {
		return "", err
	}
	s, err := loadCIState(statePath)
	if err != nil {
		return "", err
	}
	if s.Stage != "" || !slices.Contains(s.Done, stageInit) {
		return "前の段が終わっていないので plan しません", nil
	}
	summary, err := ciRunPlan(&s, d, stdout)
	if err != nil {
		if serr := s.save(statePath); serr != nil {
			return "", errors.Join(err, serr)
		}

		return "", err
	}
	s.Summary = summary
	s.done(stagePlan)

	return summary, s.save(statePath)
}

func ciRunPlan(s *ciState, d deps, stdout io.Writer) (string, error) {
	s.fail(stagePlan)
	f, err := appendOut(*s)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	// lock を取らない種別（plan の入口）は -lock=false で plan する（ADR-0604 決定8）。
	step := stepCIPlanLock
	if k, ok := cientry.Lookup(s.Mode); ok && k.Lock == cientry.LockSkip {
		step = stepCIPlanNoLock
	}
	cmd := d.tool(s.Dir, localEnv(d.environ, s.Profile, "TF_CLI_ARGS_plan=-out="+s.TFPlan), step)
	cmd.Stdout, cmd.Stderr = f, f
	code, err := exitCode(cmd.Run())
	if err != nil {
		return "", xerrors.Wrapf(errLocalTF, "plan: %v", err)
	}
	if _, err := fmt.Fprintf(stdout, "plan の終了コード: %d\n", code); err != nil {
		return "", err
	}
	show, err := openInRoot(s.JSON, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return "", xerrors.Wrap(err, "plan の JSON")
	}
	cmd = d.tool(s.Dir, localEnv(d.environ, s.Profile, "TF_CLI_ARGS_show=-json "+s.TFPlan), stepCIShow)
	cmd.Stdout, cmd.Stderr = show, f
	_ = cmd.Run()
	if err := show.Close(); err != nil {
		return "", xerrors.Wrap(err, "plan の JSON")
	}
	out, err := readInRoot(s.Out)
	if err != nil {
		return "", xerrors.Wrap(err, "全文")
	}
	s.Stage = ""
	s.fail(failureKind(stagePlan, code, out))
	if code == 1 {
		return "", errPlanFailed
	}
	data, err := readInRoot(s.JSON)
	if err != nil {
		return "", xerrors.Wrap(errPlanJSON, "ファイルを読めません")
	}
	summary, err := planSummary(strconv.Itoa(code), data)
	if err != nil {
		return "", err
	}
	s.Stage = ""

	return summary, nil
}

// ciPolicyStep は ci-policy です。入口の名前空間の Policy を1つずつ plan に掛け、違反か評価の欠けがあれば失敗します。
// conftest は作業ディレクトリへ写した Policy の木・宣言・plan に対して、固定の引数で1回だけ起動し、名前空間ごとの
// 判定は policyresult が持つ。rego のファイルは名前空間から `plan.` を除いた名前である。作業ディレクトリ（-work）は
// リポジトリの中に置く —— mise の shim は作業ディレクトリから mise.toml を探して版を決める。
func ciPolicyStep(args []string, d deps, stdout io.Writer) (string, error) {
	var policy, decl, layer, work string
	var namespaces stringList
	statePath, err := ciStepArgs("ci-policy", args, d, func(fs *flag.FlagSet) {
		fs.StringVar(&work, "work", "", "")
		fs.StringVar(&policy, "policy", "", "")
		fs.StringVar(&decl, "decl", "", "")
		fs.StringVar(&layer, "layer", "", "")
		fs.Var(&namespaces, "namespace", "")
	})
	if err != nil {
		return "", err
	}
	if work == "" || policy == "" || decl == "" || layer == "" || len(namespaces) == 0 {
		return "", xerrors.Wrap(errUsage, "ci-policy は -work・-policy・-decl・-layer と1つ以上の -namespace が要ります")
	}
	s, err := loadCIState(statePath)
	if err != nil {
		return "", err
	}
	if s.Stage != "" || !slices.Contains(s.Done, stagePlan) {
		return "前の段が終わっていないので Policy を掛けません", nil
	}
	if err := ciRunPolicy(s, d, stdout, work, policy, decl, layer, namespaces); err != nil {
		s.fail(stagePolicy)
		if serr := s.save(statePath); serr != nil {
			return "", errors.Join(err, serr)
		}

		return "", err
	}
	s.done(stagePolicy)

	return fmt.Sprintf("✅ %d 個の名前空間の Policy を通りました", len(namespaces)), s.save(statePath)
}

// stringList は、繰り返し渡せるフラグです。
type stringList []string

func (l *stringList) String() string { return strings.Join(*l, " ") }

func (l *stringList) Set(v string) error {
	*l = append(*l, v)

	return nil
}

func ciRunPolicy(s ciState, d deps, stdout io.Writer, parent, policy, decl, layer string, namespaces []string) error {
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return xerrors.Wrap(err, "作業ディレクトリ")
	}
	work, err := os.MkdirTemp(parent, "policy-")
	if err != nil {
		return xerrors.Wrap(err, "作業ディレクトリ")
	}
	defer func() { _ = os.RemoveAll(work) }()
	if err := copyTree(policy, filepath.Join(work, "rego")); err != nil {
		return xerrors.Wrap(errPolicyRules, err.Error())
	}
	for src, dst := range map[string]string{decl: "decl.json", layer: "layer.json", s.JSON: "plan.json"} {
		if err := copyFile(src, filepath.Join(work, dst)); err != nil {
			return xerrors.Wrap(errPolicyJSON, err.Error())
		}
	}
	var result bytes.Buffer
	cmd := d.tool(work, localEnv(d.environ, s.Profile), stepCIConftest)
	cmd.Stdout, cmd.Stderr = &result, d.stderr
	// 違反があれば conftest は非ゼロで終わる。判定は結果の JSON で行う。
	if _, err := exitCode(cmd.Run()); err != nil {
		return xerrors.Wrapf(errLocalTF, "conftest: %v", err)
	}
	entries, err := policyresult.Parse(result.Bytes())
	if err != nil {
		return err
	}
	var failed []string
	for _, ns := range namespaces {
		rego, err := readInRoot(filepath.Join(policy, strings.TrimPrefix(ns, "plan.")+".rego"))
		if err != nil {
			return xerrors.Wrapf(errPolicyRules, "%s の rego を読めません", ns)
		}
		n := policyresult.CountRules(rego)
		if n == 0 {
			return xerrors.Wrapf(errPolicyRules, "%s に deny contains が1つもありません", ns)
		}
		mine := slices.DeleteFunc(slices.Clone(entries), func(e policyresult.Entry) bool { return e.Namespace != ns })
		line, err := policyresult.Judge(map[string]int{ns: n}, mine)
		if err != nil {
			if _, werr := fmt.Fprintln(stdout, err.Error()); werr != nil {
				return werr
			}
			failed = append(failed, ns)

			continue
		}
		if _, err := fmt.Fprintln(stdout, line); err != nil {
			return err
		}
	}
	if len(failed) > 0 {
		return xerrors.Wrap(errCIPolicy, strings.Join(failed, " "))
	}

	return nil
}

func copyFile(src, dst string) error {
	raw, err := readInRoot(src)
	if err != nil {
		return err
	}

	return writeInRoot(dst, raw)
}

// writeInRoot は、path を所有者だけが読める権限で、そのディレクトリの外へ出ない形で書きます。
func writeInRoot(path string, data []byte) error {
	f, err := openInRoot(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()

		return err
	}

	return f.Close()
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if e.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o700)
		}

		return copyFile(path, filepath.Join(dst, rel))
	})
}

// ciApplyStep は ci-apply です。Policy を通った plan だけを apply し（ADR-0601 決定6、ADR-0604 決定11）、limit を
// 過ぎれば SIGINT を送って打ち切る。SIGINT を受けた terraform は state を書いて lock を外してから止まる。
func ciApplyStep(args []string, d deps, stdout io.Writer) (string, error) {
	var limit time.Duration
	statePath, err := ciStepArgs("ci-apply", args, d, func(fs *flag.FlagSet) {
		fs.DurationVar(&limit, "timeout", applyTimeout, "")
	})
	if err != nil {
		return "", err
	}
	if limit <= 0 {
		return "", xerrors.Wrap(errUsage, "ci-apply は正の -timeout が要ります")
	}
	s, err := loadCIState(statePath)
	if err != nil {
		return "", err
	}
	if s.Mode != ciModeApply {
		return "", xerrors.Wrap(errUsage, "plan の入口は apply しません")
	}
	if s.Stage != "" || !slices.Contains(s.Done, stagePolicy) {
		return "前の段が終わっていないので apply しません", nil
	}
	s.fail(stageApply)
	if err := s.save(statePath); err != nil {
		return "", err
	}
	code, err := ciRunApply(s, d, limit)
	if err != nil {
		return "", err
	}
	if _, err := fmt.Fprintf(stdout, "apply の終了コード: %d\n", code); err != nil {
		return "", err
	}
	out, err := readInRoot(s.Out)
	if err != nil {
		return "", xerrors.Wrap(err, "全文")
	}
	s.Stage = ""
	kind := failureKind(stageApply, code, out)
	if kind != "applied" {
		s.fail(kind)
		if serr := s.save(statePath); serr != nil {
			return "", serr
		}
		if code == applyTimedOut {
			return "", errApplyTimeout
		}

		return "", xerrors.Wrapf(errApplyFailed, "終了コード %d", code)
	}
	s.Applied = true
	s.done(stageApply)

	return "✅ apply が終わりました", s.save(statePath)
}

func ciRunApply(s ciState, d deps, limit time.Duration) (int, error) {
	f, err := appendOut(s)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	cmd := d.tool(s.Dir, localEnv(d.environ, s.Profile, "TF_CLI_ARGS_apply=-input=false -no-color "+s.TFPlan), stepCIApply)
	cmd.Stdout, cmd.Stderr = f, f
	if err := cmd.Start(); err != nil {
		return 0, xerrors.Wrapf(errLocalTF, "apply: %v", err)
	}
	var timedOut atomic.Bool
	timer := time.AfterFunc(limit, func() {
		timedOut.Store(true)
		_ = cmd.Process.Signal(os.Interrupt)
	})
	code, err := exitCode(cmd.Wait())
	timer.Stop()
	if err != nil {
		return 0, xerrors.Wrapf(errLocalTF, "apply: %v", err)
	}
	if timedOut.Load() {
		return applyTimedOut, nil
	}

	return code, nil
}

// ciUploadStep は ci-upload です。prepare が終わっていれば、前の段の成否に関わらず全文を置きます。S3 は SHA-256 を
// 突き合わせ、違えば書き込みを拒む。apply が終わった後に置けなければ、失敗の種類は upload である。
func ciUploadStep(args []string, d deps, stdout io.Writer) (string, error) {
	statePath, err := ciStepArgs("ci-upload", args, d, nil)
	if err != nil {
		return "", err
	}
	s, err := loadCIState(statePath)
	if err != nil {
		return "", err
	}
	if err := ciRunUpload(&s, d, stdout); err != nil {
		s.fail(stageUpload)
		if serr := s.save(statePath); serr != nil {
			return "", errors.Join(err, serr)
		}

		return "", err
	}
	s.done(stageUpload)

	return "全文の版 ID: " + s.Version, s.save(statePath)
}

func ciRunUpload(s *ciState, d deps, stdout io.Writer) error {
	sha, err := fileSHA256(s.Out, os.ReadFile)
	if err != nil {
		return xerrors.Wrap(errCIUpload, err.Error())
	}
	s.SHA = sha
	if _, err := fmt.Fprintf(stdout, "全文: key=%s sha256=%s\n", s.Key, sha); err != nil {
		return err
	}
	work, err := os.MkdirTemp("", "tf-local-upload-")
	if err != nil {
		return xerrors.Wrap(errCIUpload, err.Error())
	}
	defer func() { _ = os.RemoveAll(work) }()
	if err := copyFile(s.Out, filepath.Join(work, "body.txt")); err != nil {
		return xerrors.Wrap(errCIUpload, err.Error())
	}
	input, err := json.Marshal(map[string]string{"Bucket": s.OutBucket, "Key": s.Key, "ChecksumSHA256": sha, "ExpectedBucketOwner": s.Account})
	if err != nil {
		return xerrors.Wrap(errCIUpload, err.Error())
	}
	if err := writeInRoot(filepath.Join(work, "input.json"), input); err != nil {
		return xerrors.Wrap(errCIUpload, err.Error())
	}
	out, err := runTool(d.tool, work, localEnv(d.environ, s.Profile, "AWS_REGION="+s.Region), stepCIPutObject, d.stderr)
	if err != nil {
		return xerrors.Wrap(errCIUpload, err.Error())
	}
	var resp struct {
		VersionID string `json:"VersionId"`
	}
	if err := json.Unmarshal(out, &resp); err != nil || resp.VersionID == "" {
		return xerrors.Wrap(errCIUpload, "応答に版 ID がありません")
	}
	s.Version = resp.VersionID

	return nil
}

// ciRequired は、入口ごとに終わっていなければならない段です。
func ciRequired(mode string) []string {
	steps := []string{stagePrepare, stageInit, stagePlan, stagePolicy}
	if mode == ciModeApply {
		steps = append(steps, stageApply)
	}

	return append(steps, stageUpload)
}

// ciFinishStep は ci-finish です。失敗した段があれば、apply の入口は failure にその種類を書いて落ちる。すべての段が
// 終わっていれば job summary を書き、apply の入口は outputs の plan に要約を書く。それらを書けなければ、apply の
// 入口の失敗の種類は applied である。全文・plan の JSON・plan のファイルは消す。
func ciFinishStep(args []string, d deps) (string, error) {
	statePath, err := ciStepArgs("ci-finish", args, d, nil)
	if err != nil {
		return "", err
	}
	s, err := loadCIState(statePath)
	if err != nil {
		return "", err
	}
	defer s.remove()
	for _, step := range ciRequired(s.Mode) {
		if !slices.Contains(s.Done, step) {
			s.fail(step)
		}
	}
	if s.Stage != "" {
		err := errCIUnfinish
		if slices.Contains(s.Done, stageUpload) {
			err = errCIFailed
		}
		if s.Mode == ciModeApply {
			if werr := writeFailure(d.environ, s.Stage); werr != nil {
				return "", errors.Join(err, werr)
			}
		}

		return "", xerrors.Wrapf(err, "失敗の段: %s", s.Stage)
	}
	done := ""
	if s.Mode == ciModeApply {
		done = "apply が終わりました\n\n"
	}
	summary := fmt.Sprintf("### %s\n\n%s\n\n%s- key: `%s`\n- sha256: `%s`\n- 版 ID: `%s`\n", s.Root, s.Summary, done, s.Key, s.SHA, s.Version)
	err = appendGitHub(envValue(d.environ, "GITHUB_STEP_SUMMARY"), summary)
	if s.Mode == ciModeApply {
		err = errors.Join(err, appendGitHub(envValue(d.environ, "GITHUB_OUTPUT"), "plan="+s.Summary+"\n"))
		if err != nil {
			if werr := writeFailure(d.environ, "applied"); werr != nil {
				err = errors.Join(err, werr)
			}
		}
	}
	if err != nil {
		return "", err
	}

	return "✅ " + s.Root + " の " + s.Mode + " が終わりました", nil
}
