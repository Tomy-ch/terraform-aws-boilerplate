package main

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/cientry"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workflow"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const workflowsDir = ".github/workflows"

// isApplySuffix は、push で起動する種別の role の environment の接尾辞かを返します。push で起動しない種別が
// 同じ role を使う job は、接尾辞ではなく入口（sharedEnvEntry）で除く。
func isApplySuffix(suffix string) bool {
	return roleSuffixOf(suffix, cientry.EventPush)
}

// isPullRequestSuffix は、pull_request で起動する種別の role の environment の接尾辞かを返します。
func isPullRequestSuffix(suffix string) bool {
	return roleSuffixOf(suffix, cientry.EventPullRequest)
}

// roleSuffixOf は、suffix が、起動として event を許す種別のどれかの role の接尾辞 `-<role>` かを返します。
func roleSuffixOf(suffix, event string) bool {
	return slices.ContainsFunc(cientry.Kinds(), func(k cientry.Kind) bool {
		return slices.Contains(k.Events, event) &&
			(suffix == "-"+k.Role(cientry.RootIdentity) || suffix == "-"+k.Role(cientry.RootWorkload))
	})
}

// sharedEnvKinds は、push で起動しない種別のうち、push で起動する種別と role を共有するもの。その入口を走らせる
// job は push で起動する種別と同じ environment を使うが、push の起動条件の生成から除く。otherKinds はそのほかの種別。
var sharedEnvKinds, otherKinds = func() ([]cientry.Kind, []cientry.Kind) {
	var shared, other []cientry.Kind
	for _, k := range cientry.Kinds() {
		if !slices.Contains(k.Events, cientry.EventPush) &&
			(isApplySuffix("-"+k.Role(cientry.RootIdentity)) || isApplySuffix("-"+k.Role(cientry.RootWorkload))) {
			shared = append(shared, k)
		} else {
			other = append(other, k)
		}
	}
	return shared, other
}()

var (
	errTrigger      = xerrors.New("apply の workflow の起動条件を生成できません")
	errTriggerDrift = xerrors.New("apply の workflow の on.push.branches が branches.json からずれています（make repo-settings-apply）")

	anyEnvironment = regexp.MustCompile(`^\s+["']?environment["']?\s*:`)
	jobEnvironment = regexp.MustCompile(`^ {4}environment:[ \t]*(?:"([A-Za-z0-9._-]+)"|'([A-Za-z0-9._-]+)'|([A-Za-z0-9._-]+))[ \t]*(?:#.*)?$`)
	onKey          = regexp.MustCompile(`^(?:on|"on"|'on'):[ \t]*(?:#.*)?$`)
	pushKey        = regexp.MustCompile(`^ {2}push:[ \t]*(?:#.*)?$`)
	branchesKey    = regexp.MustCompile(`^ {4}branches:[ \t]*(?:#.*)?$`)
	branchItem     = regexp.MustCompile(`^ {6}- `)
	blankOrComment = regexp.MustCompile(`^\s*(?:#.*)?$`)
	sharedEnvEntry = cientry.MustPattern(`\bmake\b.*\bci-`, `-[a-z0-9-]+`, sharedEnvKinds...)
	otherEntry     = cientry.MustPattern(`\bmake\b.*\bci-`, `-[a-z0-9-]+`, otherKinds...)
	skipLine       = regexp.MustCompile(`^\s*#|^\s+(?:- )?name:`)
)

type workflowEdit struct {
	path     string
	current  string
	rendered string
}

// renderApplyTriggers は、apply の environment を使う job を持つ workflow の on.push.branches を、その env の
// デプロイブランチ1つへ生成し直します（ADR-0603 決定2-3）。env は job の environment の名前から決めるので、
// workflow に印は要らない。対象が1件も無いとき、1つの workflow が複数の env の apply を持つとき、env の
// デプロイブランチが無いとき、on.push.branches をブロック形式の一覧で持たないときは失敗する。書き込みはしない。
func renderApplyTriggers(root string, b branches) ([]workflowEdit, error) {
	dir := filepath.Join(root, workflowsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, xerrors.Wrapf(err, "%s の読み取り", workflowsDir)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	var edits []workflowEdit
	for _, path := range workflow.SelectFiles(names, dir) {
		src, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return nil, xerrors.Wrapf(err, "%s の読み取り", path)
		}
		rel, _ := filepath.Rel(root, path)
		envs, err := applyEnvs(string(src))
		if err != nil {
			return nil, xerrors.Wrapf(err, "%s", rel)
		}
		switch {
		case len(envs) == 0:
			continue
		case len(envs) > 1:
			return nil, xerrors.Wrapf(errTrigger, "%s: 複数の env の apply の job を持ちます（%s）", rel, strings.Join(envs, ", "))
		}
		d, ok := b.DeployForEnv(envs[0])
		if !ok {
			return nil, xerrors.Wrapf(errTrigger, "%s: env %s のデプロイブランチが branches.json にありません", rel, envs[0])
		}
		rendered, err := renderPushBranches(string(src), d.Branch)
		if err != nil {
			return nil, xerrors.Wrapf(err, "%s", rel)
		}
		edits = append(edits, workflowEdit{path: rel, current: string(src), rendered: rendered})
	}
	if len(edits) == 0 {
		return nil, xerrors.Wrapf(errTrigger, "%s に apply の environment を使う job がありません", workflowsDir)
	}
	return edits, nil
}

// applyEnvs は、workflow の job が使う apply の environment の env を、重複を除いて返します。job の
// environment: を字下げ4桁のスカラー以外（mapping、式、アンカー、別の字下げ）で書いた行があれば失敗する
// —— 読めない形を対象外と読むと、その workflow の起動条件が宣言から外れる（ADR-0702 決定14）。job の
// steps: の列（[workflow.StepsSpan]）の中の行は step の入力なので読まない。sharedEnvKinds の入口を走らせる
// job の environment は、読めることだけを確かめて数えない。その入口とほかの CI の入口を併せ持つ job は失敗する
// —— 数えないと apply の environment が起動条件の生成から黙って外れる。
func applyEnvs(src string) ([]string, error) {
	var envs []string
	for _, job := range workflow.SplitJobs(src).Jobs {
		shared := isSharedEnvJob(job)
		if shared && hasEntry(job, otherEntry) {
			return nil, xerrors.Wrapf(errTrigger, "job %s が lock を残す入口とほかの CI の入口を併せ持ちます。lock を残す job か apply の job かを決められません", job.ID)
		}
		from, to, hasSteps := workflow.StepsSpan(job)
		for i, l := range job.Lines {
			if (hasSteps && i >= from && i < to) || !anyEnvironment.MatchString(l.Text) {
				continue
			}
			m := jobEnvironment.FindStringSubmatch(l.Text)
			if m == nil {
				return nil, xerrors.Wrapf(errTrigger, "%d 行目の environment: を読めません。job の直下に名前1つで書いてください", l.Number)
			}
			env, suffix, ok := splitIdentity(m[1] + m[2] + m[3])
			if ok && !shared && isApplySuffix(suffix) && !slices.Contains(envs, env) {
				envs = append(envs, env)
			}
		}
	}
	return envs, nil
}

// isSharedEnvJob はコメントと step の name: の行を数えない。
func isSharedEnvJob(job workflow.Job) bool {
	return hasEntry(job, sharedEnvEntry)
}

func hasEntry(job workflow.Job, re *regexp.Regexp) bool {
	return slices.ContainsFunc(job.Lines, func(l workflow.Line) bool {
		return !skipLine.MatchString(l.Text) && re.MatchString(l.Text)
	})
}

// renderPushBranches は、on.push.branches のブロック形式の一覧を branch 1つへ置き換えた本文を返します。
func renderPushBranches(src, branch string) (string, error) {
	lines := strings.Split(src, "\n")
	start, end, err := pushBranchesItems(lines)
	if err != nil {
		return "", err
	}
	out := slices.Concat(lines[:start], []string{"      - " + branch}, lines[end:])
	return strings.Join(out, "\n"), nil
}

// pushBranchesItems は、on.push.branches の一覧の行の範囲 [start, end) を返します。範囲は最後の要素までで、
// 要素の間の空行とコメントを含む。一覧の中に要素でもコメントでもない行があれば失敗する。
func pushBranchesItems(lines []string) (int, int, error) {
	i := slices.IndexFunc(lines, onKey.MatchString)
	if i < 0 {
		return 0, 0, xerrors.Wrap(errTrigger, "on: がブロック形式でありません")
	}
	i = nextMatch(lines, i+1, 2, pushKey)
	if i < 0 {
		return 0, 0, xerrors.Wrap(errTrigger, "on.push がブロック形式でありません")
	}
	i = nextMatch(lines, i+1, 4, branchesKey)
	if i < 0 {
		return 0, 0, xerrors.Wrap(errTrigger, "on.push.branches がブロック形式の一覧でありません")
	}
	start := i + 1
	end := start
scan:
	for j := start; j < len(lines); j++ {
		l := lines[j]
		switch {
		case branchItem.MatchString(l):
			end = j + 1
		case blankOrComment.MatchString(l):
		case len(l)-len(strings.TrimLeft(l, " ")) < 6:
			break scan
		default:
			return 0, 0, xerrors.Wrapf(errTrigger, "on.push.branches の %d 行目が要素ではありません", j+1)
		}
	}
	if end == start {
		return 0, 0, xerrors.Wrap(errTrigger, "on.push.branches に要素がありません")
	}
	return start, end, nil
}

// nextMatch は、from から、字下げが depth より浅い行に当たるまでの間で re に当たる行の位置を返します。
// 空行とコメントは飛ばす。見つからなければ -1 を返す。
func nextMatch(lines []string, from, depth int, re *regexp.Regexp) int {
	for i := from; i < len(lines); i++ {
		l := lines[i]
		if blankOrComment.MatchString(l) {
			continue
		}
		if len(l)-len(strings.TrimLeft(l, " ")) < depth {
			return -1
		}
		if re.MatchString(l) {
			return i
		}
	}
	return -1
}
