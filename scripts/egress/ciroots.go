package main

import (
	"fmt"
	"os"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workflow"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// ciMakefile は CI が root の AWS に触れる入口（make ci-plan-* / ci-apply-* / ci-unlock-*）を持つ。
const ciMakefile = ".makefiles/terraform/operation/ci.mk"

var (
	errCIMake          = xerrors.New("ci.mk の CI の入口を解釈できません")
	errCITargetUnknown = xerrors.New("workflow が呼ぶ CI の入口が ci.mk にありません")
	errRootUnbound     = xerrors.New("ジョブが実行する root を、SSOT の root がそのジョブとして名指していません")
	errRootJobIdle     = xerrors.New("SSOT の root が名指すジョブが、その root を実行していません")
	// errCIOutsideJob は job の外（jobs: より前の env: など）に入口の名前がある workflow のエラー。
	// どの job がそれを実行するかを読めない。
	errCIOutsideJob = xerrors.New("workflow の job の外に CI の入口の名前があります")
)

var (
	// ciTokenRe は workflow の行を名前の字で切ったトークン。ci-plan- / ci-apply- / ci-unlock- で始まるトークンを
	// CI の入口の呼び出しとみなし、make を介さない書き方でも拾う。ci.mk に無いもの（式で組んだ名前、
	// 名前に使わない字が続くもの）は、切り詰めずにそのまま未知の入口として落とす。
	ciTokenRe = regexp.MustCompile(`[A-Za-z0-9_-]+`)
	// stepNameLineRe は step の名前の行。名前に入口の名前が在っても、それを実行しているとは数えない。
	stepNameLineRe = regexp.MustCompile(`^\s+(?:- )?name:`)
	// ciRuleRe / ciCallRe は ci.mk の入口の見出しと、その次の行の呼び出し。
	ciRuleRe = regexp.MustCompile(`^(ci-(plan|apply|unlock)-[a-z0-9]+(?:-[a-z0-9]+)*):$`)
	ciCallRe = regexp.MustCompile(`^\t@\$\(call CI_(PLAN|APPLY|UNLOCK),([a-z0-9]+),(identity|workload)\)$`)
	// ciCallAnyRe は呼び出しの字面。読めた入口の件数と突き合わせる（ADR-0702 決定15）。
	ciCallAnyRe = regexp.MustCompile(`\$\(call CI_(?:PLAN|APPLY|UNLOCK),`)
)

// checkCIRoots は ci.mk と workflow を読み、ジョブが実行する root と SSOT の root の名指しを突き合わせる。
// names は .github/workflows の下のファイル名。
func (s *ssot) checkCIRoots(repo string, names []string) error {
	r, err := os.OpenRoot(repo)
	if err != nil {
		return xerrors.Wrap(err, "open "+repo)
	}
	defer func() { _ = r.Close() }()
	mk, err := r.ReadFile(ciMakefile)
	if err != nil {
		return xerrors.Wrap(err, "read "+ciMakefile)
	}
	targets, err := parseCIMake(string(mk))
	if err != nil {
		return xerrors.Wrap(err, ciMakefile)
	}
	ran, err := ranRoots(r, names, targets)
	if err != nil {
		return err
	}
	return s.checkBindings(ran)
}

// parseCIMake は ci.mk の入口ごとに、それが実行する root のパスを返す。
func parseCIMake(data string) (map[string]string, error) {
	lines := strings.Split(data, "\n")
	targets := map[string]string{}
	for i, line := range lines {
		m := ciRuleRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if _, ok := targets[m[1]]; ok {
			return nil, xerrors.Wrap(errCIMake, fmt.Sprintf("%d 行目: %s が重複しています", i+1, m[1]))
		}
		var call []string
		if i+1 < len(lines) {
			call = ciCallRe.FindStringSubmatch(lines[i+1])
		}
		if call == nil || call[1] != strings.ToUpper(m[2]) {
			return nil, xerrors.Wrap(errCIMake, fmt.Sprintf("%d 行目: %s の次の行が $(call CI_%s,<env>,<identity|workload>) ではありません", i+1, m[1], strings.ToUpper(m[2])))
		}
		targets[m[1]] = rootPath(call[2], call[3])
	}
	if len(targets) == 0 {
		return nil, xerrors.Wrap(errCIMake, "入口が 1 件もありません")
	}
	if n := len(ciCallAnyRe.FindAllString(data, -1)); n != len(targets) {
		return nil, xerrors.Wrap(errCIMake, fmt.Sprintf("読めた入口 %d 件 / 呼び出しの字面 %d 件", len(targets), n))
	}
	return targets, nil
}

// rootPath は env と root の種類から root のディレクトリを返す。配置は ADR-0604 決定5 が決める。
func rootPath(env, kind string) string {
	if kind == "identity" {
		return "env/" + env + "/identity"
	}
	return "env/" + env
}

// ranRoots は workflow のジョブごとに、そのジョブが CI の入口を通して実行する root を返す。
func ranRoots(repo *os.Root, names []string, targets map[string]string) (map[string][]string, error) {
	ran := map[string][]string{}
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
					dir, ok := targets[tok]
					if !ok {
						return nil, xerrors.Wrap(errCITargetUnknown, fmt.Sprintf("%s %d 行目: %s（%s）", key, l.Number, tok, ciMakefile))
					}
					if !slices.Contains(ran[key], dir) {
						ran[key] = append(ran[key], dir)
					}
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
		if strings.HasPrefix(tok, "ci-plan-") || strings.HasPrefix(tok, "ci-apply-") || strings.HasPrefix(tok, "ci-unlock-") {
			toks = append(toks, tok)
		}
	}
	return toks
}

// checkBindings は、ジョブが実際に実行する root と、SSOT の root が名指すジョブが一致することを見る。
// 宣言だけを起点にすると、名指したジョブが別の root も実行し始めたとき、その root の型は突き合わせを受けない。
func (s *ssot) checkBindings(ran map[string][]string) error {
	keys := make([]string, 0, len(ran))
	for key := range ran {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		for _, dir := range ran[key] {
			if r, ok := s.roots[dir]; !ok || !slices.Contains(r.jobs, key) {
				return xerrors.Wrap(errRootUnbound, fmt.Sprintf("%s は %s を実行します（%s の root の jobs へ追加してください）", key, dir, ssotFile))
			}
		}
	}
	for _, name := range s.rootOrder {
		for _, key := range s.roots[name].jobs {
			if !slices.Contains(ran[key], name) {
				return xerrors.Wrap(errRootJobIdle, fmt.Sprintf("root %s: %s（%s の入口を呼んでいません）", name, key, ciMakefile))
			}
		}
	}
	return nil
}
