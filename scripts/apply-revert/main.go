// Package main は、デプロイブランチへの merge の後の apply が失敗したとき、最後に成功した apply の commit へ
// 宣言を戻す revert のブランチを用意するかを決めるツール。
//
//	apply-revert plan -repo <owner/name> -line <branch> -default <branch> -head <sha> -run <id>
//	    -results "<job>=<result>:<outcome>:<failure> ..." -workflow <file> -branches <branches.json> -server <url> -output <file>
//
// -branches のほかの flag は、与えなければ revert-plan の job の環境変数から取る（envDefaults）。
//
// -line はデプロイブランチ、-default はデフォルトブランチの名前である。revert はデプロイブランチの上では作らず、
// 失敗した merge が運んだ線の commit（head の第2親）の上で作り、デフォルトブランチへ向ける（ADR-0601 決定17）。
//
// -results は apply の job ごとの結果で、root ごとに1つ渡す。最後に成功した apply の commit は、名指したすべての job が
// 成功した最も新しい run の commit である。
//
// 決めたことは -output のファイル（GITHUB_OUTPUT）へ `<鍵>=<値>` の行で書く。`decision` は次のどれか。
//
//	skip    revert の対象にしない失敗である。何も通知しない
//	none    対象だが、revert のブランチを用意できない。理由を通知する
//	local   用意できるが、GITHUB_TOKEN では push できない（.github/workflows/ を戻す）。人が手元で実行する
//	        1つのコマンドを通知する
//	push    revert のブランチを push する。push は書き込みの job が `make ci-revert-push` で行う
//	exists  同じ run の revert のブランチが既に在り、木が同じである。push しない
//
// 判定は gh と git の出力だけを読む。origin へは書かない —— この job は contents: read だけを持つ。戻す範囲は、
// 一時の worktree で実際に revert し、木が最後に成功した commit の木と一致することを確かめてから決める。
// push の入口はこの判定を繰り返さず、同じ commit を同じ順に1回の `git revert` へ渡して push するだけである。
// 規則は .github/workflows/README.md の「apply の失敗の revert」。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/branchdecl"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	commandTimeout = 60 * time.Second
	// runsPerPage は、最後に成功した apply を探す run の数。1ページだけを読み、見つからなければ用意しない。
	runsPerPage = 100
	// workflowsDir の下を変える commit は、GITHUB_TOKEN では push できない（workflows の権限を持てない）。
	workflowsDir = ".github/workflows/"
	// notAncestor は、`git merge-base --is-ancestor` が「祖先でない」ときに返す終了コード。
	notAncestor = 1
	// branchSuffix は、revert のブランチの名前の、接頭辞の後に置く部分の形。run の番号だけで決め、
	// 同じ run の再実行が同じ名前になるようにする。
	branchSuffix = "apply-%d"
)

// revertKinds は、revert の対象にする失敗の種類。commit の宣言を AWS に対して評価した後の失敗である。
// 種類は .makefiles/terraform/operation/ci.mk の CI_APPLY が書く。
var revertKinds = []string{"plan", "policy", "apply", "timeout"}

// skipOutcomes は、apply の step の結果（steps.<id>.outcome）のうち、revert の対象にしないもの。eligibility が読む。
var skipOutcomes = []string{"success", "cancelled", "skipped"}

// skipKinds は、対象にしない失敗の種類。原因が commit の中身に無い。applied は apply が終わった後の失敗である。
var skipKinds = []string{"prepare", "lock", "upload", "applied"}

// jobResults は、apply の job の結果（needs.<job>.result）。
var jobResults = []string{"success", "failure", "cancelled", "skipped"}

var (
	shaPattern  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	jobPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

	errUsage          = xerrors.New("❌ usage: apply-revert plan -repo <r> -line <b> -default <b> -head <sha> -run <id> -results \"<job>=<result>:<outcome>:<failure> ...\" -workflow <f> -branches <file> -server <url> -output <file>（-branches のほかは環境変数 GITHUB_REPOSITORY・GITHUB_REF_NAME・DEFAULT_BRANCH・GITHUB_SHA・GITHUB_RUN_ID・APPLY_REVERT_RESULTS・APPLY_REVERT_WORKFLOW・GITHUB_SERVER_URL・GITHUB_OUTPUT でもよい）")
	errNoOutcome      = xerrors.New("❌ apply の step の結果を受け取れません。apply の job の outputs が渡っていません")
	errNoKind         = xerrors.New("❌ apply の step が失敗したのに、失敗の種類を受け取れません")
	errUnknownKind    = xerrors.New("❌ 失敗の種類を知りません")
	errUnknownOutcome = xerrors.New("❌ apply の step の結果を知りません")
	errAPI            = xerrors.New("❌ GitHub API の応答を読めません")
	errGit            = xerrors.New("❌ git の出力を読めません")
	errBranches       = xerrors.New("❌ branches.json の revert の接頭辞を読めません")
	errNotDeploy      = xerrors.New("❌ -line は env を持つデプロイブランチではありません")
	errBranchTaken    = xerrors.New("❌ revert のブランチが既に在り、木が最後に成功した commit と違います")
)

// runner は、git と gh を起動する継ぎ目（scripts/README.md の Test Strategy「外界はその境界で差し替える」）。
type runner struct {
	output func(name string, args ...string) (string, error)
}

type exitCoder interface {
	ExitCode() int
}

func main() {
	log.SetFlags(0)

	if err := run(os.Args[1:], os.Getenv, os.Stdout, runner{output: output}); err != nil {
		log.Fatalf("%v", err)
	}
}

type options struct {
	repo, line, def, head, workflow, branches, server, output string
	results                                                   []jobResult
	jobs                                                      []string
	runID                                                     int64
}

// jobResult は、1つの apply の job の結果と、その apply の step の結果と、失敗の段。
type jobResult struct {
	job, result, outcome, failure string
}

// parseResults は、`<job>=<result>:<outcome>:<failure>` を空白で区切った並びを読みます。outcome と failure は
// 空でよく、その判定は eligibility が持つ。
func parseResults(value string) ([]jobResult, error) {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return nil, xerrors.Wrap(errUsage, "-results が空です")
	}
	out := make([]jobResult, 0, len(fields))
	for _, f := range fields {
		job, rest, ok := strings.Cut(f, "=")
		parts := strings.Split(rest, ":")
		if !ok || !jobPattern.MatchString(job) || len(parts) != 3 || !slices.Contains(jobResults, parts[0]) {
			return nil, xerrors.Wrapf(errUsage, "-results の %q", f)
		}
		if slices.ContainsFunc(out, func(r jobResult) bool { return r.job == job }) {
			return nil, xerrors.Wrapf(errUsage, "-results の job %q が2回あります", job)
		}
		out = append(out, jobResult{job: job, result: parts[0], outcome: parts[1], failure: parts[2]})
	}
	return out, nil
}

// envDefaults は、flag の名前と、与えないときに値を取る環境変数の名前の対。環境変数は revert-plan の job が渡す
// （.github/workflows/terraform-apply.yaml の revert-plan）。
var envDefaults = map[string]string{
	"repo":     "GITHUB_REPOSITORY",
	"line":     "GITHUB_REF_NAME",
	"default":  "DEFAULT_BRANCH",
	"head":     "GITHUB_SHA",
	"run":      "GITHUB_RUN_ID",
	"results":  "APPLY_REVERT_RESULTS",
	"workflow": "APPLY_REVERT_WORKFLOW",
	"server":   "GITHUB_SERVER_URL",
	"output":   "GITHUB_OUTPUT",
}

func parseOptions(args []string, getenv func(string) string) (options, error) {
	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	env := func(name string) string { return getenv(envDefaults[name]) }
	var o options
	fs.StringVar(&o.repo, "repo", env("repo"), "")
	fs.StringVar(&o.line, "line", env("line"), "")
	fs.StringVar(&o.def, "default", env("default"), "")
	fs.StringVar(&o.head, "head", env("head"), "")
	runID := fs.String("run", env("run"), "")
	results := fs.String("results", env("results"), "")
	fs.StringVar(&o.workflow, "workflow", env("workflow"), "")
	fs.StringVar(&o.branches, "branches", "", "")
	fs.StringVar(&o.server, "server", env("server"), "")
	fs.StringVar(&o.output, "output", env("output"), "")
	if err := fs.Parse(args); err != nil {
		return options{}, xerrors.Wrap(errUsage, err.Error())
	}
	id, err := strconv.ParseInt(*runID, 10, 64)
	if err != nil {
		return options{}, xerrors.Wrapf(errUsage, "-run の %q", *runID)
	}
	o.runID = id
	if fs.NArg() > 0 || !repoPattern.MatchString(o.repo) || o.line == "" || o.def == "" || !shaPattern.MatchString(o.head) || o.runID <= 0 ||
		o.workflow == "" || o.branches == "" || o.server == "" || o.output == "" {
		return options{}, errUsage
	}
	parsed, err := parseResults(*results)
	if err != nil {
		return options{}, err
	}
	o.results = parsed
	for _, r := range parsed {
		o.jobs = append(o.jobs, r.job)
	}
	return o, nil
}

// run は、サブコマンド plan とその flag を args で受けます。
func run(args []string, getenv func(string) string, out io.Writer, r runner) error {
	if len(args) == 0 || args[0] != "plan" {
		return errUsage
	}
	o, err := parseOptions(args[1:], getenv)
	if err != nil {
		return err
	}
	reason, err := resultsEligibility(o.results)
	if err != nil {
		return err
	}
	if reason != "" {
		fmt.Fprintf(out, "revert を用意しません: %s\n", reason)
		return writeOutputs(o.output, [][2]string{{"decision", "skip"}, {"reason", reason}})
	}

	d, err := decide(o, r)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s\n%s\n", d[1][1], d[2][1])
	return writeOutputs(o.output, d)
}

// decide が返す組の先頭3つは decision・headline・summary の順である（run が headline と summary を読む）。
func decide(o options, r runner) ([][2]string, error) {
	decl, err := branchdecl.Read(o.branches)
	if err != nil {
		return nil, xerrors.Wrap(errBranches, err.Error())
	}
	prefix := decl.Revert
	deploy, ok := decl.DeployForBranch(o.line)
	if !ok {
		return nil, xerrors.Wrapf(errNotDeploy, "%s", o.line)
	}
	none := func(reason string) [][2]string {
		return [][2]string{{"decision", "none"}, {"headline", "apply の失敗を戻す revert を用意できませんでした"}, {"summary", reason}}
	}

	base, err := lastSuccess(r, o.repo, o.line, o.workflow, o.jobs, o.runID)
	if err != nil {
		return nil, err
	}
	if base == "" {
		return none(fmt.Sprintf("%s で %s が成功した run が、直近の %d 件にありません", o.line, strings.Join(o.jobs, "・"), runsPerPage)), nil
	}

	commits, reason, err := revertRange(r, base, o.head)
	if err != nil {
		return nil, err
	}
	source := ""
	if reason == "" {
		source, reason, err = sourceHead(r, deploy, o.def, o.head)
		if err != nil {
			return nil, err
		}
	}
	if reason == "" {
		reason, err = verifyRevert(r, base, source, commits)
		if err != nil {
			return nil, err
		}
	}
	if reason != "" {
		return none(reason), nil
	}

	branch := prefix + fmt.Sprintf(branchSuffix, o.runID)
	link := compareURL(o.server, o.repo, o.def, branch, o.runID, base, commits)

	local, err := touchesWorkflows(r, base, o.head)
	if err != nil {
		return nil, err
	}
	if local {
		command := fmt.Sprintf("make apply-revert REVERT_HEAD=%s REVERT_COMMITS='%s' REVERT_BRANCH=%s",
			source, strings.Join(commits, " "), branch)
		return [][2]string{
			{"decision", "local"},
			{"headline", "apply の失敗を戻す revert は、手元で作る必要があります"},
			{"summary", fmt.Sprintf("戻す範囲が %s を含むので、CI は push できません。手元で `%s` を実行し、<%s|Pull Request を開いてください>", workflowsDir, command, link)},
		}, nil
	}

	tree, exists, err := existingBranch(r, o.repo, branch)
	if err != nil {
		return nil, err
	}
	if exists {
		want, err := r.output("git", "rev-parse", base+"^{tree}")
		if err != nil {
			return nil, xerrors.Wrap(errGit, err.Error())
		}
		if tree != strings.TrimSpace(want) {
			return nil, xerrors.Wrapf(errBranchTaken, "%s", branch)
		}
		return [][2]string{
			{"decision", "exists"},
			{"headline", "apply の失敗を戻す revert のブランチは用意済みです"},
			{"summary", fmt.Sprintf("<%s|Pull Request を開いてください>（%s）", link, branch)},
		}, nil
	}

	return [][2]string{
		{"decision", "push"},
		{"headline", "apply の失敗を戻す revert のブランチを用意しました"},
		{"summary", fmt.Sprintf("<%s|Pull Request を開いてください>（%s。%d 件の merge を戻し、%s の木に揃えます）", link, branch, len(commits), base[:7])},
		{"commits", strings.Join(commits, " ")},
		{"branch", branch},
		{"head", source},
	}, nil
}

// resultsEligibility は、apply の job ごとの結果から、revert の対象かを返します。どれか1つの job が対象なら
// 空の理由を返す。failure でない job は戻すものを持たない。どれか1つでも判定できない job があれば、
// ほかが対象でもエラーにする。
func resultsEligibility(results []jobResult) (string, error) {
	if len(results) == 0 {
		return "", xerrors.Wrap(errUsage, "apply の job の結果がありません")
	}
	target := false
	var reasons []string
	for _, r := range results {
		if r.result != "failure" {
			reasons = append(reasons, fmt.Sprintf("%s の結果が %s です", r.job, r.result))
			continue
		}
		reason, err := eligibility(r.outcome, r.failure)
		if err != nil {
			return "", xerrors.Wrapf(err, "%s", r.job)
		}
		if reason == "" {
			target = true
			continue
		}
		reasons = append(reasons, r.job+": "+reason)
	}
	if target {
		return "", nil
	}
	return strings.Join(reasons, "。"), nil
}

// eligibility は、apply の step の結果と失敗の種類から、revert の対象かを返します。対象なら空の理由を、
// 対象でなければ理由を返す。結果か種類を受け取れていないとき、および skipOutcomes・skipKinds・revertKinds の
// どれにも無い値のときはエラーにする —— 知らない値を「対象でない」と読むと、機構が黙って働かなくなる。
func eligibility(outcome, failure string) (string, error) {
	switch {
	case outcome == "":
		return "", errNoOutcome
	case slices.Contains(skipOutcomes, outcome):
		return fmt.Sprintf("apply の step の結果が %s です（失敗していないか、途中で止まりました）", outcome), nil
	case outcome != "failure":
		return "", xerrors.Wrapf(errUnknownOutcome, "%q", outcome)
	}
	switch {
	case failure == "":
		return "", errNoKind
	case slices.Contains(revertKinds, failure):
		return "", nil
	case slices.Contains(skipKinds, failure):
		return fmt.Sprintf("失敗の種類が %s です。原因が commit の中身に無いので戻しません", failure), nil
	default:
		return "", xerrors.Wrapf(errUnknownKind, "%q", failure)
	}
}

type workflowRuns struct {
	TotalCount   *int `json:"total_count"`
	WorkflowRuns []struct {
		ID      int64  `json:"id"`
		HeadSHA string `json:"head_sha"`
	} `json:"workflow_runs"`
}

type runJobs struct {
	TotalCount *int `json:"total_count"`
	Jobs       []struct {
		Name       string `json:"name"`
		Conclusion string `json:"conclusion"`
		HeadSHA    string `json:"head_sha"`
	} `json:"jobs"`
}

// lastSuccess は、line への push で起動した workflow の run のうち、jobs のすべてが成功した最も新しい run の
// commit を返します。見つからなければ空を返す。run と job の記録だけを読む（理由は .github/workflows/README.md の
// 「apply の失敗の revert」）。
func lastSuccess(r runner, repo, line, workflow string, jobs []string, current int64) (string, error) {
	query := url.Values{"branch": {line}, "event": {"push"}, "status": {"completed"}, "per_page": {strconv.Itoa(runsPerPage)}}
	raw, err := r.output("gh", "api", fmt.Sprintf("repos/%s/actions/workflows/%s/runs?%s", repo, url.PathEscape(workflow), query.Encode()))
	if err != nil {
		return "", xerrors.Wrap(errAPI, err.Error())
	}
	var runs workflowRuns
	if err := json.Unmarshal([]byte(raw), &runs); err != nil || runs.TotalCount == nil {
		return "", xerrors.Wrap(errAPI, "workflow の run の一覧")
	}
	for _, run := range runs.WorkflowRuns {
		if run.ID == current {
			continue
		}
		raw, err := r.output("gh", "api", fmt.Sprintf("repos/%s/actions/runs/%d/jobs?filter=latest&per_page=100", repo, run.ID))
		if err != nil {
			return "", xerrors.Wrap(errAPI, err.Error())
		}
		var listed runJobs
		if err := json.Unmarshal([]byte(raw), &listed); err != nil || listed.TotalCount == nil {
			return "", xerrors.Wrapf(errAPI, "run %d の job の一覧", run.ID)
		}
		succeeded := map[string]bool{}
		for _, j := range listed.Jobs {
			if slices.Contains(jobs, j.Name) && j.Conclusion == "success" && j.HeadSHA == run.HeadSHA {
				succeeded[j.Name] = true
			}
		}
		if len(succeeded) == len(jobs) && shaPattern.MatchString(run.HeadSHA) {
			return run.HeadSHA, nil
		}
	}
	return "", nil
}

// revertRange は、base から head までの first-parent の merge を、revert する順（新しい順）に返します。
// 用意できない理由があれば、commit の代わりに理由を返す。
func revertRange(r runner, base, head string) ([]string, string, error) {
	if _, err := r.output("git", "merge-base", "--is-ancestor", base, head); err != nil {
		var code exitCoder
		if xerrors.As(err, &code) && code.ExitCode() == notAncestor {
			return nil, fmt.Sprintf("最後に成功した commit %s が、%s の first-parent の祖先ではありません", base[:7], head[:7]), nil
		}
		return nil, "", xerrors.Wrap(errGit, err.Error())
	}
	trees, err := r.output("git", "rev-parse", base+"^{tree}", head+"^{tree}")
	if err != nil {
		return nil, "", xerrors.Wrap(errGit, err.Error())
	}
	t := strings.Fields(trees)
	if len(t) != 2 {
		return nil, "", xerrors.Wrapf(errGit, "木の SHA が2つではありません: %q", trees)
	}
	if t[0] == t[1] {
		return nil, fmt.Sprintf("宣言は最後に成功した commit %s と同じです。失敗の原因は commit の中身にありません", base[:7]), nil
	}
	list, err := r.output("git", "rev-list", "--first-parent", "--parents", base+".."+head)
	if err != nil {
		return nil, "", xerrors.Wrap(errGit, err.Error())
	}
	var commits []string
	firstParent := ""
	for line := range strings.Lines(list) {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if !shaPattern.MatchString(f[0]) {
			return nil, "", xerrors.Wrapf(errGit, "rev-list の行 %q", line)
		}
		if len(f) != 3 {
			return nil, fmt.Sprintf("%s から %s までに merge でない commit %s があります", base[:7], head[:7], f[0][:7]), nil
		}
		commits = append(commits, f[0])
		firstParent = f[1]
	}
	if len(commits) == 0 {
		return nil, "", xerrors.Wrap(errGit, "木が違うのに、戻す merge が0件です")
	}
	// 祖先の判定は first-parent に限らない。first-parent の鎖が base に届かなければ、全部を戻しても base の木にならない。
	if firstParent != base {
		return nil, fmt.Sprintf("%s の first-parent の鎖が、最後に成功した commit %s に届きません", head[:7], base[:7]), nil
	}
	return commits, "", nil
}

// sourceHead は、デプロイブランチの merge head が運んだ線の commit（第2親）を返します。判定は
// deploy-source-check（ADR-0603 決定31）と同じで、成り立たなければ commit の代わりに用意しない理由を返す。
func sourceHead(r runner, d branchdecl.DeployBranch, def, head string) (string, string, error) {
	if !d.FromLine(def) {
		return "", fmt.Sprintf("デフォルトブランチ %s は %s へ変更を運ぶ線ではありません（hotfix の間など）", def, d.Branch), nil
	}
	parents, err := r.output("git", "rev-list", "--parents", "-n", "1", head)
	if err != nil {
		return "", "", xerrors.Wrap(errGit, err.Error())
	}
	f := strings.Fields(parents)
	if len(f) != 3 || !shaPattern.MatchString(f[2]) {
		return "", head[:7] + " は線から運んだ merge ではありません", nil
	}
	source := f[2]
	if _, err := r.output("git", "merge-base", "--is-ancestor", source, "refs/remotes/origin/"+def); err != nil {
		var code exitCoder
		if xerrors.As(err, &code) && code.ExitCode() == notAncestor {
			return "", fmt.Sprintf("%s が運んだ %s は、デフォルトブランチ %s から到達できません", head[:7], source[:7], def), nil
		}
		return "", "", xerrors.Wrap(errGit, err.Error())
	}
	trees, err := r.output("git", "rev-parse", source+"^{tree}", head+"^{tree}")
	if err != nil {
		return "", "", xerrors.Wrap(errGit, err.Error())
	}
	t := strings.Fields(trees)
	if len(t) != 2 {
		return "", "", xerrors.Wrapf(errGit, "木の SHA が2つではありません: %q", trees)
	}
	if t[0] != t[1] {
		return "", fmt.Sprintf("%s の木が、運んだ %s の木と一致しません", head[:7], source[:7]), nil
	}
	return source, "", nil
}

// verifyRevert は、start を一時の worktree へ取り出して commits をこの順に1回の `git revert` で戻し、木が base の
// 木と一致するかを確かめます。push の入口（apply-revert.mk）と同じ起動の形である。revert が衝突するか木が一致
// しなければ、用意しない理由を返す。worktree は戻る前に外す。
func verifyRevert(r runner, base, start string, commits []string) (string, error) {
	dir, err := os.MkdirTemp("", "apply-revert-")
	if err != nil {
		return "", xerrors.Wrap(err, "一時の worktree の場所を作れません")
	}
	if _, err := r.output("git", "worktree", "add", "--quiet", "--detach", dir, start); err != nil {
		_ = os.Remove(dir)
		return "", xerrors.Wrap(errGit, err.Error())
	}
	defer func() { _, _ = r.output("git", "worktree", "remove", "--force", dir) }()

	args := append([]string{"-C", dir, "-c", "user.name=apply-revert", "-c", "user.email=apply-revert@invalid",
		"revert", "-m", "1", "--no-edit", "--"}, commits...)
	if _, err := r.output("git", args...); err != nil {
		var code exitCoder
		if xerrors.As(err, &code) && code.ExitCode() == 1 {
			short := make([]string, len(commits))
			for i, c := range commits {
				short[i] = c[:7]
			}
			return fmt.Sprintf("merge %s の revert のいずれかが衝突するか、空になります", strings.Join(short, " ")), nil
		}
		return "", xerrors.Wrap(errGit, err.Error())
	}
	if _, err := r.output("git", "-C", dir, "diff", "--quiet", base, "HEAD"); err != nil {
		var code exitCoder
		if xerrors.As(err, &code) && code.ExitCode() == 1 {
			return fmt.Sprintf("revert した木が、最後に成功した commit %s の木と一致しません", base[:7]), nil
		}
		return "", xerrors.Wrap(errGit, err.Error())
	}
	return "", nil
}

func touchesWorkflows(r runner, base, head string) (bool, error) {
	names, err := r.output("git", "diff", "--name-only", "--no-renames", "-z", base, head)
	if err != nil {
		return false, xerrors.Wrap(errGit, err.Error())
	}
	for name := range strings.SplitSeq(names, "\x00") {
		if strings.HasPrefix(name, workflowsDir) {
			return true, nil
		}
	}
	return false, nil
}

// existingBranch は、revert のブランチが在れば、その先頭の commit の木を返します。
func existingBranch(r runner, repo, branch string) (string, bool, error) {
	raw, err := r.output("gh", "api", fmt.Sprintf("repos/%s/git/matching-refs/heads/%s", repo, branch))
	if err != nil {
		return "", false, xerrors.Wrap(errAPI, err.Error())
	}
	var refs []struct {
		Ref    string `json:"ref"`
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := json.Unmarshal([]byte(raw), &refs); err != nil || refs == nil {
		return "", false, xerrors.Wrap(errAPI, "ref の一覧")
	}
	for _, ref := range refs {
		if ref.Ref != "refs/heads/"+branch {
			continue
		}
		raw, err := r.output("gh", "api", fmt.Sprintf("repos/%s/git/commits/%s", repo, ref.Object.SHA))
		if err != nil {
			return "", false, xerrors.Wrap(errAPI, err.Error())
		}
		var c struct {
			Tree struct {
				SHA string `json:"sha"`
			} `json:"tree"`
		}
		if err := json.Unmarshal([]byte(raw), &c); err != nil || !shaPattern.MatchString(c.Tree.SHA) {
			return "", false, xerrors.Wrapf(errAPI, "commit %s", ref.Object.SHA)
		}
		return c.Tree.SHA, true, nil
	}
	return "", false, nil
}

// compareURL は、人が1回押して Pull Request を開く URL を返します。宛先 target はデフォルトブランチである。
// 本文は commit の SHA と run の URL だけを持ち、commit の題は持たない —— 通知へ届く文字を、ここで
// 組み立てた値に限る。
func compareURL(server, repo, target, branch string, runID int64, base string, commits []string) string {
	run := fmt.Sprintf("%s/%s/actions/runs/%d", server, repo, runID)
	var b strings.Builder
	fmt.Fprintf(&b, "apply の失敗（%s）を受けて、最後に成功した apply の commit `%s` へ宣言を戻します。merge の後、デプロイブランチへ運びます。\n\n戻す merge（新しい順）:\n\n", run, base)
	for _, c := range commits {
		fmt.Fprintf(&b, "- %s\n", c)
	}
	q := url.Values{"quick_pull": {"1"}, "title": {fmt.Sprintf("Revert: run %d の apply の失敗を戻す", runID)}, "body": {b.String()}}
	return fmt.Sprintf("%s/%s/compare/%s...%s?%s", server, repo, target, branch, q.Encode())
}

// writeOutputs は、鍵と値の組を GITHUB_OUTPUT の形で追記します。値は1行に限る。
func writeOutputs(path string, kv [][2]string) error {
	var b strings.Builder
	for _, p := range kv {
		if strings.ContainsAny(p[1], "\r\n") {
			return xerrors.Newf("❌ %s の値が改行を含みます", p[0])
		}
		fmt.Fprintf(&b, "%s=%s\n", p[0], p[1])
	}
	f, err := os.OpenFile(filepath.Clean(path), os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return xerrors.Wrapf(err, "%s を開けません", path)
	}
	if _, err := f.WriteString(b.String()); err != nil {
		_ = f.Close()
		return xerrors.Wrapf(err, "%s へ書けません", path)
	}
	return xerrors.Wrapf(f.Close(), "%s を閉じられません", path)
}

func output(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, name, args...).Output() //nolint:gosec // 引数はこのファイルで組み立てた固定の形
	if err != nil {
		return "", xerrors.Wrap(err, name+" "+strings.Join(args, " "))
	}
	return string(out), nil
}
