package main

import (
	"fmt"
	"os"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/cientry"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workflow"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var (
	errCITargetUnknown = xerrors.New("workflow が呼ぶ CI の入口が ci.mk にありません")
	errRootUnbound     = xerrors.New("ジョブが実行する root を、SSOT の root がそのジョブとして名指していません")
	errRootJobIdle     = xerrors.New("SSOT の root が名指すジョブが、その root を実行していません")
	// errCIOutsideJob は job の外（jobs: より前の env: など）に入口の名前がある workflow のエラー。
	// どの job がそれを実行するかを読めない。
	errCIOutsideJob = xerrors.New("workflow の job の外に CI の入口の名前があります")
)

var (
	// ciTokenRe は workflow の行を名前の字で切ったトークン。登録簿の種別の ci-<種別>- で始まるトークンを
	// CI の入口の呼び出しとみなし、make を介さない書き方でも拾う。ci.mk に無いもの（式で組んだ名前、
	// 名前に使わない字が続くもの）は、切り詰めずにそのまま未知の入口として落とす。
	ciTokenRe = regexp.MustCompile(`[A-Za-z0-9_-]+`)
	// stepNameLineRe は step の名前の行。名前に入口の名前が在っても、それを実行しているとは数えない。
	stepNameLineRe = regexp.MustCompile(`^\s+(?:- )?name:`)
)

var entryPrefixes = func() []string {
	kinds := cientry.Kinds()
	out := make([]string, len(kinds))
	for i, k := range kinds {
		out[i] = "ci-" + k.Name + "-"
	}
	return out
}()

// ciEntry は ci.mk の入口 1 件。terraform は、その入口が root で Terraform を実行するか（cientry.Kind.Terraform）。
type ciEntry struct {
	root      string
	terraform bool
}

// rootRuns は 1 ジョブが入口を通して触れる root ごとに、その root で Terraform を実行するかを持つ。
type rootRuns map[string]bool

// checkCIRoots は ci.mk と workflow を読み、ジョブが実行する root と SSOT の root の名指しを突き合わせ、
// ジョブごとの rootRuns を返す。names は .github/workflows の下のファイル名。
func (s *ssot) checkCIRoots(repo string, names []string) (map[string]rootRuns, error) {
	entries, err := cientry.ParseRepo(repo)
	if err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(repo)
	if err != nil {
		return nil, xerrors.Wrap(err, "open "+repo)
	}
	defer func() { _ = r.Close() }()
	ran, err := ranRoots(r, names, ciEntries(entries))
	if err != nil {
		return nil, err
	}
	if err := s.checkBindings(ran); err != nil {
		return nil, err
	}
	return ran, nil
}

// ciEntries は ci.mk の入口ごとに、それが実行する root のパスと Terraform を実行するかを返す。
func ciEntries(entries []cientry.Entry) map[string]ciEntry {
	targets := make(map[string]ciEntry, len(entries))
	for _, e := range entries {
		targets[e.Target] = ciEntry{root: rootPath(e.Env, e.Root), terraform: e.Kind.Terraform}
	}
	return targets
}

// rootPath は env と root の種類から root のディレクトリを返す。配置は ADR-0604 決定5 が決める。
func rootPath(env string, root cientry.Root) string {
	if root == cientry.RootIdentity {
		return "env/" + env + "/identity"
	}
	return "env/" + env
}

// ranRoots は workflow のジョブごとに、そのジョブが CI の入口を通して触れる root を返す。1 つの root を
// lock の解除と plan / apply / lock を残す入口の両方で触れるジョブは、その root で Terraform を実行するとみなす。
func ranRoots(repo *os.Root, names []string, targets map[string]ciEntry) (map[string]rootRuns, error) {
	ran := map[string]rootRuns{}
	for _, name := range names {
		f := path.Join(".github", "workflows", name)
		data, err := repo.ReadFile(f)
		if err != nil {
			return nil, xerrors.Wrap(err, "read "+f)
		}
		split := workflow.SplitJobs(string(data))
		for _, l := range split.Preamble {
			if toks := entryTokens(l); len(toks) > 0 {
				return nil, xerrors.Wrap(errCIOutsideJob, fmt.Sprintf("%s %d 行目: %s", name, l.Number, toks[0]))
			}
		}
		for _, job := range split.Jobs {
			key := name + ":" + job.ID
			for _, l := range job.Lines {
				for _, tok := range entryTokens(l) {
					e, ok := targets[tok]
					if !ok {
						return nil, xerrors.Wrap(errCITargetUnknown, fmt.Sprintf("%s %d 行目: %s（%s）", key, l.Number, tok, cientry.Makefile))
					}
					if ran[key] == nil {
						ran[key] = rootRuns{}
					}
					ran[key][e.root] = ran[key][e.root] || e.terraform
				}
			}
		}
	}
	return ran, nil
}

// entryTokens は 1 行のうち CI の入口の名前とみなすトークンを返す。コメントと step の名前の行は読まない。
func entryTokens(l workflow.Line) []string {
	if commentRe.MatchString(l.Text) || stepNameLineRe.MatchString(l.Text) {
		return nil
	}
	var toks []string
	for _, tok := range ciTokenRe.FindAllString(l.Text, -1) {
		if slices.ContainsFunc(entryPrefixes, func(p string) bool { return strings.HasPrefix(tok, p) }) {
			toks = append(toks, tok)
		}
	}
	return toks
}

// checkBindings は、ジョブが実際に実行する root と、SSOT の root が名指すジョブが一致することを見る。
// 宣言だけを起点にすると、名指したジョブが別の root も実行し始めたとき、その root の型は突き合わせを受けない。
func (s *ssot) checkBindings(ran map[string]rootRuns) error {
	keys := make([]string, 0, len(ran))
	for key := range ran {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		for _, dir := range sortedKeys(ran[key]) {
			if r, ok := s.roots[dir]; !ok || !slices.Contains(r.jobs, key) {
				return xerrors.Wrap(errRootUnbound, fmt.Sprintf("%s は %s を実行します（%s の root の jobs へ追加してください）", key, dir, ssotFile))
			}
		}
	}
	for _, name := range s.rootOrder {
		for _, key := range s.roots[name].jobs {
			if _, ok := ran[key][name]; !ok {
				return xerrors.Wrap(errRootJobIdle, fmt.Sprintf("root %s: %s（%s の入口を呼んでいません）", name, key, cientry.Makefile))
			}
		}
	}
	return nil
}
