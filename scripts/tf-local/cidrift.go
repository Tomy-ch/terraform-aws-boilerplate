package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/branchdecl"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/cientry"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/gitenv"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/ownedattrs"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workdir"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// CI の drift の検出の段（ci.mk の CI_DRIFT と、ci-target-drift・ci-report-drift）。ADR-0601 決定12 の検出を、env の
// デプロイブランチの先頭の木に対する plan で行う。
//
//	ci-drift-target は git だけを起動し、デプロイブランチの先頭の commit と、デフォルトブランチの木の drift の入口ごとに
//	デプロイブランチの木の ci.mk がその入口を持つか（root ごとの昇格が済んだか）を GITHUB_OUTPUT に書く。
//	ci-drift-lock は aws だけを起動し、plan の前と後に lock file の有無を見る。在れば apply と重なったとする。
//	ci-drift は道具を起動せず、plan の JSON の resource_changes と resource_drift から drift を判定する。resource_drift
//	からは ADR-0601 決定20 の一覧（lib/ownedattrs）の組だけを外す。
//	ci-drift-report は道具を起動せず、drift の job の結果（needs の JSON）と開いている issue から、issue の本文・開閉・
//	通知と、通知が届いた後に書く drift の集合の印を決める。
//
// 公開されるログと issue に、属性の値と address を出さない。伏せた address と最上位の属性の名前は job summary に
// だけ書く。値は読んで比べるだけで、どこにも書かない。

const (
	ciModeDrift = "drift"

	stageLockBefore = "lock-before"
	stageLockAfter  = "lock-after"
	stageDriftLock  = "drift-lock"
	stageDrift      = "drift"

	stepGitFetch    = "git-fetch-deploy"
	stepGitRevParse = "git-rev-parse-fetch-head"
	stepGitCatFile  = "git-cat-file-ci-mk"

	verdictClean        = "clean"
	verdictDrift        = "drift"
	verdictInconclusive = "inconclusive"

	// driftTitle は drift の報告の issue の title。開いている issue のうち title が一致するものを同じ報告とみなす
	// （.github/actions/upsert-report-issue）。
	driftTitle = "Terraform Drift Report"
	// issueMarker は、upsert-report-issue が本文の先頭に置く印。workflow は ci-drift-report の出力からこれを渡す。
	issueMarker = "<!-- terraform-drift-report -->"
	// issueAuthor は、GITHUB_TOKEN で作った issue の作者の、`gh issue list --json author` の login。
	issueAuthor = "app/github-actions"
	// issueListLimit は、terraform-drift.yaml の `gh issue list` の --limit。一覧がこの件数に達すれば、前回の issue を
	// 見落としうるので失敗する。
	issueListLimit = 1000
	// outputDelimiter は、GITHUB_OUTPUT の複数行の値の区切り。値がこの行を含めば書かない。
	outputDelimiter = "TF_LOCAL_DRIFT_OUTPUT_END"

	driftJobPrefix  = "drift-"
	targetJobPrefix = "drift-target-"
	gateJobPrefix   = "drift-infra-code-gate-"
	// promotedPrefix は、ci-drift-target が root ごとに書く出力の名前の接頭辞（promoted-<env>-<root>）。
	promotedPrefix = "promoted-"
)

var (
	sha1Pattern   = regexp.MustCompile(`^[0-9a-f]{40}$`)
	digestMarker  = regexp.MustCompile(`<!-- drift-digest: ([0-9a-f]{64}) -->`)
	digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	attrPattern   = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
	jobIDPattern  = regexp.MustCompile(`^[a-z0-9-]+$`)
	resultPattern = regexp.MustCompile(`^[a-z_]+$`)

	errDriftTarget       = xerrors.New("❌ デプロイブランチの先頭の commit を決められません")
	errDriftTargetEnv    = xerrors.New("❌ env にデプロイブランチがありません。デプロイブランチを持たない env（sandbox）の drift の木は、sandbox の apply の経路が決めます")
	errDriftJSON         = xerrors.New("❌ plan の JSON の resource_drift を読めません")
	errDriftOwned        = xerrors.New("❌ ADR-0601 決定20 の一覧を読めないので、drift を判定しません")
	errDriftFound        = xerrors.New("❌ drift があります（一覧は job summary）")
	errDriftInconclusive = xerrors.New("❌ plan の前か後に state の lock があり、apply と重なったので drift を判定しません")
	errDriftReport       = xerrors.New("❌ drift の job の結果を読めません")
)

// driftItem は値を持たない（address は伏せ、属性は名前だけを残す）。
type driftItem struct {
	Op      string   `json:"op"`
	Address string   `json:"address"`
	Attrs   []string `json:"attrs,omitempty"`
}

func (i driftItem) String() string {
	s := i.Op + " " + i.Address
	if len(i.Attrs) > 0 {
		s += ": " + strings.Join(i.Attrs, ", ")
	}

	return s
}

// gitStep は git の段を起動して stdout を返します。可変の値（refspec・object の名前）は引数ではなく stdin で渡し、
// 道具の引数はすべて固定にする（command）。
func gitStep(d deps, env []string, step, stdin string) ([]byte, error) {
	var out strings.Builder
	cmd := d.tool(context.Background(), d.root, env, step)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Stdout, cmd.Stderr = &out, d.stderr
	code, err := toolrun.Exec(cmd)
	if err != nil {
		return nil, xerrors.Wrap(err, step)
	}
	if code != 0 {
		return nil, xerrors.Wrapf(errDriftTarget, "%s: exit status %d", step, code)
	}

	return []byte(out.String()), nil
}

// catFileBlob は、`git cat-file --batch` の応答（`<oid> blob <size>` の行と中身）から中身を返します。
func catFileBlob(out []byte) ([]byte, error) {
	head, rest, ok := strings.Cut(string(out), "\n")
	fields := strings.Fields(head)
	if !ok || len(fields) != 3 || fields[1] != "blob" {
		return nil, xerrors.Wrapf(errDriftTarget, "%s を git から読めません", cientry.Makefile)
	}
	size, err := strconv.Atoi(fields[2])
	if err != nil || size < 0 || size+1 != len(rest) || !strings.HasSuffix(rest, "\n") {
		return nil, xerrors.Wrapf(errDriftTarget, "%s の大きさが git の応答と合いません", cientry.Makefile)
	}

	return []byte(rest[:size]), nil
}

// deployBranch は env のデプロイブランチの名前を、分岐の宣言から返します。
func deployBranch(root, env string) (string, error) {
	b, err := branchdecl.Read(filepath.Join(root, branchdecl.File))
	if err != nil {
		return "", xerrors.Wrap(errDriftTarget, err.Error())
	}
	dep, ok := b.DeployForEnv(env)
	if !ok {
		return "", xerrors.Wrapf(errDriftTargetEnv, "env %s", env)
	}
	if !branchPattern.MatchString(dep.Branch) {
		return "", xerrors.Wrap(errDriftTarget, "デプロイブランチの名前の形が不正です")
	}

	return dep.Branch, nil
}

// ciDriftTargetStep は ci-drift-target です。env のデプロイブランチの先頭を取り、デフォルトブランチの木（d.root）の ci.mk の
// env の drift の入口ごとに、デプロイブランチの木の ci.mk が同じ入口を持てば promoted-<env>-<root>=true とします。無い root
// （昇格の前）は false で成功し、その root の drift の job を走らせない。デフォルトブランチの木に env の drift の入口が無ければ
// 失敗する。
func ciDriftTargetStep(args []string, d deps, stdout io.Writer) (string, error) {
	fs := flag.NewFlagSet("ci-drift-target", flag.ContinueOnError)
	fs.SetOutput(d.stderr)
	env := fs.String("env", "", "")
	if err := fs.Parse(args); err != nil {
		return "", xerrors.Wrap(errUsage, err.Error())
	}
	if *env == "" || fs.NArg() > 0 {
		return "", errUsage
	}
	if !tokenPattern.MatchString(*env) {
		return "", errToken
	}
	branch, err := deployBranch(d.root, *env)
	if err != nil {
		return "", err
	}
	local, err := cientry.ParseRepo(d.root)
	if err != nil {
		return "", xerrors.Wrap(errDriftTarget, err.Error())
	}
	var wanted []cientry.Entry
	for _, e := range local {
		if e.Kind.Name == cientry.Drift && e.Env == *env {
			wanted = append(wanted, e)
		}
	}
	if len(wanted) == 0 {
		return "", xerrors.Wrapf(errDriftTarget, "%s に env %s の drift の入口がありません", cientry.Makefile, *env)
	}
	gitEnv, err := gitenv.Env(d.environ)
	if err != nil {
		return "", xerrors.Wrap(errDriftTarget, err.Error())
	}
	if _, err := gitStep(d, gitEnv, stepGitFetch, "refs/heads/"+branch+"\n"); err != nil {
		return "", err
	}
	out, err := gitStep(d, gitEnv, stepGitRevParse, "")
	if err != nil {
		return "", err
	}
	sha := strings.TrimSpace(string(out))
	if !sha1Pattern.MatchString(sha) {
		return "", xerrors.Wrap(errDriftTarget, "rev-parse が commit を返しません")
	}
	out, err = gitStep(d, gitEnv, stepGitCatFile, sha+":"+cientry.Makefile+"\n")
	if err != nil {
		return "", err
	}
	blob, err := catFileBlob(out)
	if err != nil {
		return "", err
	}
	entries, err := cientry.Parse(string(blob))
	if err != nil {
		return "", xerrors.Wrapf(errDriftTarget, "%s の %s を読めません: %v", branch, cientry.Makefile, err)
	}
	var outputs strings.Builder
	outputs.WriteString("sha=" + sha + "\n")
	lines := []string{fmt.Sprintf("%s のデプロイブランチ %s の先頭: %s", *env, branch, sha)}
	for _, w := range wanted {
		root := w.Env + "-" + string(w.Root)
		promoted := slices.ContainsFunc(entries, func(e cientry.Entry) bool { return e.Target == w.Target })
		fmt.Fprintf(&outputs, "%s%s=%t\n", promotedPrefix, root, promoted)
		lines = append(lines, fmt.Sprintf("- %s: drift の入口が%s", root, map[bool]string{true: "あります", false: "ありません。昇格の前なので drift を判定しません"}[promoted]))
	}
	if err := appendGitHub(envValue(d.environ, "GITHUB_OUTPUT"), outputs.String()); err != nil {
		return "", err
	}
	text := strings.Join(lines, "\n")
	if err := appendGitHub(envValue(d.environ, "GITHUB_STEP_SUMMARY"), "### drift の木\n\n"+text+"\n"); err != nil {
		return "", err
	}
	if _, err := fmt.Fprintln(stdout, text); err != nil {
		return "", err
	}

	return "✅ drift の木を決めました", nil
}

// ciDriftLockStep は ci-drift-lock です。-at before は init の後、-at after は plan の後に、lock file の有無を見ます。
// 前の段が終わっていなければ何もしません。lock の有無を確かめられなければ、判定に届かない失敗として残します。
func ciDriftLockStep(args []string, d deps, stdout io.Writer) (string, error) {
	var at string
	statePath, err := ciStepArgs("ci-drift-lock", args, d, func(fs *flag.FlagSet) {
		fs.StringVar(&at, "at", "", "")
	})
	if err != nil {
		return "", err
	}
	after, stage := map[string]string{"before": stageInit, "after": stagePlan}[at], map[string]string{"before": stageLockBefore, "after": stageLockAfter}[at]
	if after == "" {
		return "", xerrors.Wrap(errUsage, "ci-drift-lock は -at before か -at after です")
	}
	s, err := loadCIState(statePath)
	if err != nil {
		return "", err
	}
	if s.Mode != ciModeDrift {
		return "", xerrors.Wrap(errUsage, "drift の入口のほかでは lock を見ません")
	}
	if s.Stage != "" || !slices.Contains(s.Done, after) || len(s.InitArgs) == 0 {
		return "前の段が終わっていないので lock を見ません", nil
	}
	held, err := driftLockHeld(d, s)
	if err != nil {
		s.fail(stageDriftLock)
		s.Established = false
		if serr := s.save(statePath); serr != nil {
			return "", xerrors.Join(err, serr)
		}

		return "", err
	}
	s.Overlap = s.Overlap || held
	s.done(stage)
	line := "✅ lock はありません"
	if held {
		line = "⚠️ lock があります。apply と重なっているので、drift を判定しません"
	}
	if _, err := fmt.Fprintln(stdout, line); err != nil {
		return "", err
	}

	return "lock の確認（" + at + "）が終わりました", s.save(statePath)
}

// driftLockHeld は、state の Bucket に root の lock file が在るかを返します。
func driftLockHeld(d deps, s ciState) (bool, error) {
	l, err := ciHoldTarget(d, s)
	if err != nil {
		return false, err
	}
	work, cleanup, err := workdir.Temp("", "tf-local-drift-lock-")
	if err != nil {
		return false, xerrors.Wrap(err, "作業ディレクトリ")
	}
	defer cleanup()
	etag, err := l.etag(d, work)
	if err != nil {
		return false, err
	}

	return etag != "", nil
}

// ciDriftStep は ci-drift です。plan と、その前後の lock の確認が終わっていれば、plan の JSON から drift を判定して
// state に残します。判定の出力（job summary と GITHUB_OUTPUT）は ci-finish が書く。
func ciDriftStep(args []string, d deps, stdout io.Writer) (string, error) {
	statePath, err := ciStepArgs("ci-drift", args, d, nil)
	if err != nil {
		return "", err
	}
	s, err := loadCIState(statePath)
	if err != nil {
		return "", err
	}
	if s.Mode != ciModeDrift {
		return "", xerrors.Wrap(errUsage, "drift の入口のほかでは drift を判定しません")
	}
	if s.Stage != "" || !slices.Contains(s.Done, stageLockAfter) {
		return "前の段が終わっていないので drift を判定しません", nil
	}
	if err := ciRunDrift(&s, d); err != nil {
		s.fail(stageDrift)
		s.Established = false
		if serr := s.save(statePath); serr != nil {
			return "", xerrors.Join(err, serr)
		}

		return "", err
	}
	s.done(stageDrift)
	if _, err := fmt.Fprintf(stdout, "drift の判定: %s（%d 件）\n", s.Verdict, s.Count); err != nil {
		return "", err
	}

	return "✅ drift を判定しました", s.save(statePath)
}

func ciRunDrift(s *ciState, d deps) error {
	pairs, err := ownedattrs.Read(d.root)
	if err != nil {
		return xerrors.Join(errDriftOwned, err)
	}
	data, err := readInRoot(s.JSON)
	if err != nil {
		return xerrors.Wrap(errDriftJSON, "ファイルを読めません")
	}
	drifted, err := driftItems(data, ownedattrs.NewSet(pairs))
	if err != nil {
		return err
	}
	items := make([]driftItem, 0, len(s.Changes))
	for _, c := range s.Changes {
		op, address, _ := strings.Cut(c, " ")
		items = append(items, driftItem{Op: op, Address: address})
	}
	if s.PlanChanged && len(items) == 0 {
		items = append(items, driftItem{Op: "output", Address: "（root の output）"})
	}
	items = append(items, drifted...)
	s.Drift = items
	subjects := map[string]bool{}
	for _, i := range items {
		subjects[i.Address] = true
	}
	s.Count = len(subjects)
	s.Digest = itemsDigest(items)
	switch {
	case s.Overlap:
		s.Verdict = verdictInconclusive
	case len(items) > 0:
		s.Verdict = verdictDrift
	default:
		s.Verdict = verdictClean
	}

	return nil
}

type resourceDrift struct {
	Address string `json:"address"`
	Type    string `json:"type"`
	Change  *struct {
		Actions []string        `json:"actions"`
		Before  json.RawMessage `json:"before"`
		After   json.RawMessage `json:"after"`
	} `json:"change"`
}

// driftItems は、plan の JSON の resource_drift（前回の state と、refresh した実態の差）を項目にします。update の
// 項目のうち、変わった最上位の属性が1つ以上あり、そのすべてが owned の組（resource type と属性）であるものだけを外す。
// 削除（after が null）・置換・作成と、変わった属性を読めない update は外さない。no-op は変化ではないので数えない。
// 値は比べるだけで項目に入れない。
func driftItems(data []byte, owned ownedattrs.Set) ([]driftItem, error) {
	var p struct {
		FormatVersion string          `json:"format_version"`
		ResourceDrift []resourceDrift `json:"resource_drift"`
	}
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, errDriftJSON
	}
	// resource_drift の形は major の版が決める。読み方を確かめていない major は、0件と読まずに失敗する。
	if major, _, _ := strings.Cut(p.FormatVersion, "."); major != "1" {
		return nil, xerrors.Wrapf(errDriftJSON, "format_version %q は major が 1 ではありません", p.FormatVersion)
	}
	var out []driftItem
	for _, r := range p.ResourceDrift {
		if r.Address == "" || r.Change == nil || len(r.Change.Actions) == 0 {
			return nil, xerrors.Wrap(errDriftJSON, "address か change.actions の無い項目があります")
		}
		op := strings.Join(r.Change.Actions, "+")
		switch op {
		case "no-op":
			continue
		case "delete+create", "create+delete":
			op = "replace"
		}
		item := driftItem{Op: op, Address: maskAddress(r.Address)}
		if op == "update" {
			attrs, err := changedAttrs(r.Change.Before, r.Change.After)
			if err != nil {
				return nil, err
			}
			if len(attrs) > 0 && !slices.ContainsFunc(attrs, func(a string) bool { return !owned.Has(r.Type, a) }) {
				continue
			}
			// schema の属性の名前の形でない key は伏せる。
			for _, a := range attrs {
				if !attrPattern.MatchString(a) {
					a = "<伏せた属性>"
				}
				item.Attrs = append(item.Attrs, a)
			}
		}
		out = append(out, item)
	}

	return out, nil
}

// changedAttrs は、before と after の最上位の key のうち、値が違うものを名前の順で返します。どちらかが object で
// なければ失敗します（update の項目は、前後の両方を object で持つ）。
func changedAttrs(before, after json.RawMessage) ([]string, error) {
	var b, a map[string]any
	if json.Unmarshal(before, &b) != nil || json.Unmarshal(after, &a) != nil || b == nil || a == nil {
		return nil, xerrors.Wrap(errDriftJSON, "update の項目の before と after が object ではありません")
	}
	var out []string
	for k, v := range b {
		if w, ok := a[k]; !ok || !reflect.DeepEqual(v, w) {
			out = append(out, k)
		}
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			out = append(out, k)
		}
	}
	slices.Sort(out)

	return out, nil
}

// itemsDigest は、項目の集合の SHA-256 を返します。集合が同じなら順に依らず同じ値になる。項目が無ければ空文字。
func itemsDigest(items []driftItem) string {
	if len(items) == 0 {
		return ""
	}
	lines := make([]string, len(items))
	for i, item := range items {
		lines[i] = item.String()
	}
	slices.Sort(lines)
	sum := sha256.Sum256([]byte(strings.Join(slices.Compact(lines), "\n")))

	return hex.EncodeToString(sum[:])
}

var driftRequired = []string{stagePrepare, stageInit, stageLockBefore, stagePlan, stageLockAfter, stageDrift, stageUpload}

// ciFinishDrift は drift の入口の ci-finish です。すべての段が終わっていれば、job summary に判定と項目を、
// GITHUB_OUTPUT に result・count・digest を書き、drift と未確定なら判定として落ちる。段が終わっていなければ
// result を書かずに落ちる（読み手は空の result を失敗とする）。
func ciFinishDrift(s ciState, d deps) (string, error) {
	for _, step := range driftRequired {
		if !slices.Contains(s.Done, step) {
			s.fail(step)
		}
	}
	if s.Stage != "" {
		err := errCIUnfinish
		if slices.Contains(s.Done, stageUpload) {
			err = errCIFailed
		}
		if !s.Established {
			err = xerrors.Wrapf(errCINotEstablished, "%v", err)
		}

		return "", xerrors.Wrapf(err, "失敗の段: %s", s.Stage)
	}
	list := ""
	if len(s.Drift) > 0 {
		lines := make([]string, len(s.Drift))
		for i, item := range s.Drift {
			lines[i] = item.String()
		}
		list = "<pre>\n" + html.EscapeString(strings.Join(lines, "\n")) + "\n</pre>\n\n"
	}
	summary := fmt.Sprintf("### %s\n\ndrift の判定: %s（%d 件）\n\n%s\n\n%s- key: `%s`\n- sha256: `%s`\n- 版 ID: `%s`\n", s.Root, s.Verdict, s.Count, s.Summary, list, s.Key, s.SHA, s.Version)
	err := appendGitHub(envValue(d.environ, "GITHUB_STEP_SUMMARY"), summary)
	err = xerrors.Join(err, appendGitHub(envValue(d.environ, "GITHUB_OUTPUT"), fmt.Sprintf("result=%s\ncount=%d\ndigest=%s\n", s.Verdict, s.Count, s.Digest)))
	if err != nil {
		return "", err
	}
	switch s.Verdict {
	case verdictClean:
		return "✅ " + s.Root + " に drift はありません", nil
	case verdictDrift:
		return "", xerrors.Wrapf(errDriftFound, "%s: %d 件", s.Root, s.Count)
	case verdictInconclusive:
		return "", errDriftInconclusive
	}

	return "", xerrors.Wrap(errCIState, "drift の判定がありません")
}

// needsJob は、needs の JSON（`toJSON(needs)`）の job 1つです。
type needsJob struct {
	Result  string            `json:"result"`
	Outputs map[string]string `json:"outputs"`
}

// openIssue は、`gh issue list --json number,title,body,url,author` の1件です。
type openIssue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	URL    string `json:"url"`
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
}

type rootOutcome struct {
	root, job, kind, digest string
	count                   int
}

const (
	outcomeClean      = "clean"
	outcomeDrift      = "drift"
	outcomeUnpromoted = "unpromoted"
	outcomeFailure    = "failure"
)

// driftReport は報告の決定です。State と Body は報告の job が書く issue、MarkState と MarkBody は通知が届いた後に
// 書く issue（drift の集合の印を今回の値にした本文か、閉じること）。MarkState が空なら後で書かない。
type driftReport struct {
	State, Notify, Headline, Details, FailedJob, Body string
	MarkState, MarkBody                               string
}

// ciDriftReportStep は ci-drift-report です。needs の JSON は環境変数 CI_NEEDS で受ける。drift の job は ci.mk の
// drift の入口（`ci-drift-<env>-<root>`）ごとに `drift-<env>-<root>` の job で、env ごとに `drift-target-<env>` と
// `drift-infra-code-gate-<env>` を待つ。job の欠け・余りは、配線が崩れたとして失敗する。
func ciDriftReportStep(args []string, d deps) (string, error) {
	fs := flag.NewFlagSet("ci-drift-report", flag.ContinueOnError)
	fs.SetOutput(d.stderr)
	issuesPath := fs.String("issues", "", "")
	bodyPath := fs.String("body", "", "")
	if err := fs.Parse(args); err != nil {
		return "", xerrors.Wrap(errUsage, err.Error())
	}
	if *issuesPath == "" || *bodyPath == "" || fs.NArg() > 0 {
		return "", errUsage
	}
	entries, err := cientry.ParseRepo(d.root)
	if err != nil {
		return "", xerrors.Wrap(errDriftReport, err.Error())
	}
	var needs map[string]needsJob
	if err := json.Unmarshal([]byte(envValue(d.environ, "CI_NEEDS")), &needs); err != nil || len(needs) == 0 {
		return "", xerrors.Wrap(errDriftReport, "CI_NEEDS が needs の JSON ではありません")
	}
	outcomes, err := driftOutcomes(entries, needs)
	if err != nil {
		return "", err
	}
	raw, err := d.readFile(*issuesPath)
	if err != nil {
		return "", xerrors.Wrap(errDriftReport, "開いている issue の一覧を読めません")
	}
	var issues []openIssue
	if err := json.Unmarshal(raw, &issues); err != nil {
		return "", xerrors.Wrap(errDriftReport, "開いている issue の一覧が JSON ではありません")
	}
	if len(issues) >= issueListLimit {
		return "", xerrors.Wrapf(errDriftReport, "開いている issue の一覧が上限の %d 件に達しました。前回の報告を見落としうるので判定しません", issueListLimit)
	}
	// 題名が同じでも、GITHUB_TOKEN で作られ、upsert-report-issue の印で始まる本文だけを前回の報告とする。誰でも作れる
	// issue の本文の印で、通知を止めさせない。
	var prev *openIssue
	if i := slices.IndexFunc(issues, func(o openIssue) bool {
		return o.Title == driftTitle && o.Author.Login == issueAuthor && strings.HasPrefix(o.Body, issueMarker)
	}); i >= 0 {
		prev = &issues[i]
	}
	r := decideReport(outcomes, prev)
	if r.State == "open" {
		if err := os.MkdirAll(filepath.Dir(*bodyPath), 0o700); err != nil {
			return "", xerrors.Wrap(errDriftReport, err.Error())
		}
		if err := writeInRoot(*bodyPath, []byte(r.Body)); err != nil {
			return "", xerrors.Wrap(errDriftReport, err.Error())
		}
	}
	if strings.Contains(r.Details+r.MarkBody, outputDelimiter) || strings.Contains(r.Headline, "\n") {
		return "", xerrors.Wrap(errDriftReport, "通知の値を GITHUB_OUTPUT に書けません")
	}
	var out strings.Builder
	fmt.Fprintf(&out, "title=%s\nmarker=%s\nstate=%s\nnotify=%s\nheadline=%s\nfailed-job=%s\nbody=%s\nmark-state=%s\n",
		driftTitle, issueMarker, r.State, r.Notify, r.Headline, r.FailedJob, *bodyPath, r.MarkState)
	for _, v := range [][2]string{{"details", r.Details}, {"mark-body", r.MarkBody}} {
		fmt.Fprintf(&out, "%s<<%s\n%s\n%s\n", v[0], outputDelimiter, v[1], outputDelimiter)
	}
	if err := appendGitHub(envValue(d.environ, "GITHUB_OUTPUT"), out.String()); err != nil {
		return "", err
	}
	if err := appendGitHub(envValue(d.environ, "GITHUB_STEP_SUMMARY"), "### drift の報告\n\n"+reportSummary(outcomes, r)); err != nil {
		return "", err
	}

	return fmt.Sprintf("✅ drift の報告: issue は %s、通知は %s", r.State, r.Notify), nil
}

// driftOutcomes は、ci.mk の drift の入口ごとに、その job の結果を読みます。順は ci.mk の入口の順。
func driftOutcomes(entries []cientry.Entry, needs map[string]needsJob) ([]rootOutcome, error) {
	var want []string
	var out []rootOutcome
	for _, e := range entries {
		if e.Kind.Name != cientry.Drift {
			continue
		}
		root := e.Env + "-" + string(e.Root)
		job := driftJobPrefix + root
		want = append(want, job)
		o, err := readOutcome(e.Env, root, job, needs)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	if len(out) == 0 {
		return nil, xerrors.Wrapf(errDriftReport, "%s に drift の入口がありません", cientry.Makefile)
	}
	for id := range needs {
		if !jobIDPattern.MatchString(id) {
			return nil, xerrors.Wrap(errDriftReport, "needs の job の名前を読めません")
		}
		rest, ok := strings.CutPrefix(id, driftJobPrefix)
		if _, _, isRoot := cientry.SplitEnvRoot(rest); ok && isRoot && !slices.Contains(want, id) {
			return nil, xerrors.Wrapf(errDriftReport, "needs の %s に当たる drift の入口が %s にありません", id, cientry.Makefile)
		}
	}

	return out, nil
}

func readOutcome(env, root, job string, needs map[string]needsJob) (rootOutcome, error) {
	o := rootOutcome{root: root, job: job}
	target, gate := targetJobPrefix+env, gateJobPrefix+env
	for _, id := range []string{job, target, gate} {
		n, ok := needs[id]
		if !ok {
			return o, xerrors.Wrapf(errDriftReport, "needs に %s がありません", id)
		}
		if !resultPattern.MatchString(n.Result) {
			return o, xerrors.Wrapf(errDriftReport, "%s の result を読めません", id)
		}
	}
	// 検出の job の失敗は、昇格の済みに依らず失敗として読む。
	promoted := needs[target].Outputs[promotedPrefix+root]
	switch {
	case needs[target].Result != "success":
		o.kind, o.job = outcomeFailure, target
	case needs[gate].Result != "success":
		o.kind, o.job = outcomeFailure, gate
	case promoted == "false":
		o.kind = outcomeUnpromoted
	case promoted != "true":
		o.kind, o.job = outcomeFailure, target
	default:
		o.kind = jobOutcome(needs[job], &o)
	}

	return o, nil
}

// jobOutcome は drift の job の結果を読みます。result は肯定側から読み、clean と drift のほか（inconclusive・空・
// skip・cancel・job の結果との食い違い）はすべて失敗とします。
func jobOutcome(n needsJob, o *rootOutcome) string {
	result := n.Outputs["result"]
	count, err := strconv.Atoi(n.Outputs["count"])
	if err != nil || count < 0 || strconv.Itoa(count) != n.Outputs["count"] {
		return outcomeFailure
	}
	digest := n.Outputs["digest"]
	switch {
	case n.Result == "success" && result == verdictClean && count == 0 && digest == "":
		return outcomeClean
	case n.Result == "failure" && result == verdictDrift && count > 0 && digestPattern.MatchString(digest):
		o.count, o.digest = count, digest
		return outcomeDrift
	}

	return outcomeFailure
}

// decideReport は、root ごとの結果と前回の issue から、issue の開閉と通知を決めます。失敗が1つでもあれば issue を
// 動かさずに失敗を通知する。昇格の前の root を除いた集合で判定し、昇格済みの root が無ければ何もしない。drift の集合が
// 前回の issue の印と違うときだけ通知し、印を今回の値にする（集合が無くなれば閉じる）のは通知が届いた後である。届かなければ
// 印は前回のまま残り、次の回も通知する。
func decideReport(outcomes []rootOutcome, prev *openIssue) driftReport {
	for _, o := range outcomes {
		if o.kind == outcomeFailure {
			return driftReport{State: "keep", Notify: "failure", FailedJob: o.job}
		}
	}
	var lines, parts []string
	for _, o := range outcomes {
		if o.kind == outcomeUnpromoted {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s: %d 件", o.root, o.count))
		if o.kind == outcomeDrift {
			parts = append(parts, o.root+"="+o.digest)
		}
	}
	if len(lines) == 0 {
		return driftReport{State: "keep", Notify: "none"}
	}
	prevDigest, link := "", ""
	if prev != nil {
		if m := digestMarker.FindStringSubmatch(prev.Body); m != nil {
			prevDigest = m[1]
		}
		if strings.HasPrefix(prev.URL, "https://") {
			link = "- issue: " + prev.URL
		}
	}
	if len(parts) == 0 {
		if prev == nil {
			return driftReport{State: "close", Notify: "none"}
		}

		return driftReport{State: "keep", Notify: "drift", Headline: "Terraform の drift が解消しました", Details: link, MarkState: "close"}
	}
	slices.Sort(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	digest := hex.EncodeToString(sum[:])
	details := strings.Join(lines, "\n")
	body := func(digest string) string {
		b := "Terraform の定期 plan が、宣言か前回の state と実態の差（drift）を検出しました（ADR-0601 決定12）。\n" +
			"変わる resource と属性の名前は、下の実行の job summary にあります。解消は Pull Request で行います（同 決定13）。\n\n" + details + "\n"
		if digest != "" {
			b += "\n<!-- drift-digest: " + digest + " -->\n"
		}

		return b
	}
	if prev != nil && prevDigest == digest {
		return driftReport{State: "open", Notify: "none", Body: body(digest)}
	}

	return driftReport{State: "open", Notify: "drift", Headline: "Terraform の drift を検出しました", Details: strings.TrimSpace(details + "\n" + link),
		Body: body(prevDigest), MarkState: "open", MarkBody: body(digest)}
}

// reportSummary は、報告の job summary です。root ごとの結果と、issue と通知の決定を並べる。
func reportSummary(outcomes []rootOutcome, r driftReport) string {
	var b strings.Builder
	for _, o := range outcomes {
		fmt.Fprintf(&b, "- %s: %s", o.root, o.kind)
		if o.kind == outcomeDrift {
			fmt.Fprintf(&b, "（%d 件）", o.count)
		}
		if o.kind == outcomeFailure {
			fmt.Fprintf(&b, "（%s）", o.job)
		}
		b.WriteString("\n")
	}
	unpromoted := 0
	for _, o := range outcomes {
		if o.kind == outcomeUnpromoted {
			unpromoted++
		}
	}
	fmt.Fprintf(&b, "\n昇格の前の root: %d 件\n\nissue: %s、通知: %s、通知の後の issue: %s\n", unpromoted, r.State, r.Notify, map[string]string{"": "書かない"}[r.MarkState]+r.MarkState)

	return b.String()
}
