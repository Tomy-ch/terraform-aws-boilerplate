package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/checksdecl"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// 役割ごとの ruleset の宣言の置き場（ADR-0603 決定35）。common.json と lines.json は人が書き、include の区間だけを
// 生成する。deploy-<branch>.json はファイル全体を生成する。
const (
	rulesetsDir       = ".github/settings/rulesets"
	commonRulesetFile = rulesetsDir + "/common.json"
	linesRulesetFile  = rulesetsDir + "/lines.json"
	deployRulesetStem = "deploy-"
	// legacyRulesetFile は、役割ごとの ruleset と並べて適用し突き合わせる単一の ruleset の宣言。在るときだけ読む。
	legacyRulesetFile = ".github/settings/branch-protection.json"
)

// ruleset の役割。役割が、Static Analysis で課す規則と include の生成の仕方を決める。
const (
	roleCommon = "common"
	roleLines  = "lines"
	roleDeploy = "deploy"
	roleLegacy = "legacy"
)

// env を持つデプロイブランチの ruleset が、plan の前と後に required とする context（ADR-0603 決定31・37）。
const (
	contextDeploySource = "deploy-source-check"
	contextLineGate     = "line-gate"
)

var (
	errStaleRuleset  = xerrors.New("宣言に無い ruleset のファイルがあります。消える ruleset の宣言なので、ファイルを消してください")
	errDeployRuleset = xerrors.New("デプロイブランチの ruleset を組めません")
)

// rulesetSpec は ruleset の宣言1件の置き場と役割。name はファイル名から .json を除いたもので、宣言の name と等しい。
type rulesetSpec struct {
	path string
	name string
	role string
}

func specFor(file, role string) rulesetSpec {
	return rulesetSpec{path: file, name: strings.TrimSuffix(path.Base(file), ".json"), role: role}
}

// rulesetDecl は、ruleset の宣言1件の、ファイルの今の内容と生成し直した内容。current が nil ならファイルが無い。
type rulesetDecl struct {
	spec     rulesetSpec
	current  []byte
	rendered []byte
}

// readRulesets は ruleset の宣言をすべて読み、検証し、生成し直します。順は common、lines、デプロイブランチの宣言の順の
// deploy-*、在れば branch-protection です。書き込みはしません。
func readRulesets(root string, b branches) ([]rulesetDecl, error) {
	checks, err := readChecks(root)
	if err != nil {
		return nil, err
	}
	var out []rulesetDecl
	for _, s := range []struct {
		spec     rulesetSpec
		includes []string
	}{
		{specFor(commonRulesetFile, roleCommon), rulesetIncludes(b)},
		{specFor(linesRulesetFile, roleLines), linesIncludes(b)},
	} {
		src, err := os.ReadFile(filepath.Clean(filepath.Join(root, s.spec.path)))
		if err != nil {
			return nil, xerrors.Wrapf(err, "%s の読み取り", s.spec.path)
		}
		d, err := renderAuthored(s.spec, src, s.includes)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	deploys, err := deployRulesets(root, b, checks)
	if err != nil {
		return nil, err
	}
	out = append(out, deploys...)
	legacy, err := os.ReadFile(filepath.Clean(filepath.Join(root, legacyRulesetFile)))
	switch {
	case err == nil:
		d, err := renderAuthored(specFor(legacyRulesetFile, roleLegacy), legacy, rulesetIncludes(b))
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	case !xerrors.Is(err, os.ErrNotExist):
		return nil, xerrors.Wrapf(err, "%s の読み取り", legacyRulesetFile)
	}
	if err := checkStaleRulesets(root, out); err != nil {
		return nil, err
	}
	if err := checkCoverage(out, rulesetIncludes(b)); err != nil {
		return nil, err
	}
	return out, nil
}

func readChecks(root string) (checksdecl.Decl, error) {
	raw, err := os.ReadFile(filepath.Clean(filepath.Join(root, checksdecl.File)))
	if err != nil {
		return checksdecl.Decl{}, xerrors.Wrapf(err, "%s の読み取り", checksdecl.File)
	}
	checks, err := checksdecl.Parse(raw)
	if err != nil {
		return checksdecl.Decl{}, xerrors.Wrapf(err, "%s", checksdecl.File)
	}
	return checks, nil
}

// renderAuthored は人が書く宣言を検証し、include の区間を includes で生成し直します。
func renderAuthored(spec rulesetSpec, src []byte, includes []string) (rulesetDecl, error) {
	if err := checkRuleset(src, spec); err != nil {
		return rulesetDecl{}, xerrors.Wrapf(err, "%s", spec.path)
	}
	rendered, err := renderInclude(src, includes)
	if err != nil {
		return rulesetDecl{}, xerrors.Wrapf(err, "%s", spec.path)
	}
	if err := verifyInclude(rendered, includes); err != nil {
		return rulesetDecl{}, xerrors.Wrapf(err, "%s", spec.path)
	}
	return rulesetDecl{spec: spec, current: src, rendered: rendered}, nil
}

// linesIncludes は、線の ruleset の include を返します。env を持たないデプロイブランチを宣言の順に並べ、各線を続けます
// （ADR-0603 決定36）。順は rulesetIncludes と揃える。
func linesIncludes(b branches) []string {
	names := deployNamesByEnv(b, false)
	out := make([]string, 0, len(names)+len(b.Lines.Prefixes()))
	for _, name := range names {
		out = append(out, "refs/heads/"+name)
	}
	for _, prefix := range b.Lines.Prefixes() {
		out = append(out, "refs/heads/"+prefix+"**/*")
	}
	return out
}

// deployRulesets は、env を持つデプロイブランチごとの ruleset を生成し、今のファイルの内容と並べます。checks.json の
// plan_env が、env を持つデプロイブランチの無い env を名指していれば失敗します —— その plan はどこでも required に
// ならない。
func deployRulesets(root string, b branches, checks checksdecl.Decl) ([]rulesetDecl, error) {
	for _, c := range checks.Checks {
		if c.PlanEnv == "" {
			continue
		}
		if _, ok := b.DeployForEnv(c.PlanEnv); !ok {
			return nil, xerrors.Wrapf(errDeployRuleset, "%s の %s の plan_env %q の env を持つデプロイブランチがありません", checksdecl.File, c.Name, c.PlanEnv)
		}
	}
	var out []rulesetDecl
	for _, d := range b.Deploy {
		if d.Env == "" {
			continue
		}
		spec := specFor(rulesetsDir+"/"+deployRulesetStem+d.Branch+".json", roleDeploy)
		contexts, err := deployContexts(d.Env, checks)
		if err != nil {
			return nil, xerrors.Wrapf(err, "%s", spec.path)
		}
		rendered, err := renderDeployRuleset(spec.name, d.Branch, contexts)
		if err != nil {
			return nil, xerrors.Wrapf(err, "%s", spec.path)
		}
		if err := checkRuleset(rendered, spec); err != nil {
			return nil, xerrors.Wrapf(err, "%s", spec.path)
		}
		current, err := os.ReadFile(filepath.Clean(filepath.Join(root, spec.path)))
		if err != nil && !xerrors.Is(err, os.ErrNotExist) {
			return nil, xerrors.Wrapf(err, "%s の読み取り", spec.path)
		}
		out = append(out, rulesetDecl{spec: spec, current: current, rendered: rendered})
	}
	return out, nil
}

// deployContexts は、env のデプロイブランチの ruleset が required とする context を、決定31 の検査、checks.json の
// 宣言の順のその env の plan、線の検証の順に返します（ADR-0603 決定12・35）。plan の context が決定31 の検査か
// 線の検証と同じ名前か、2つの plan の検査が同じ context を名指すときは失敗します —— 1つの check が2つの
// required を満たし、片方の検査が走らなくても ruleset は通る。
func deployContexts(env string, checks checksdecl.Decl) ([]string, error) {
	owner := map[string]string{}
	for _, c := range checks.Checks {
		for _, ctx := range c.Contexts {
			switch prev, ok := owner[ctx]; {
			case ctx == contextDeploySource || ctx == contextLineGate:
				return nil, xerrors.Wrapf(errDeployRuleset, "%s の %s の context %q は、デプロイブランチの ruleset が別に required とする名前です", checksdecl.File, c.Name, ctx)
			case ok && prev != c.Name:
				return nil, xerrors.Wrapf(errDeployRuleset, "%s の %s と %s が同じ context %q を名指しています", checksdecl.File, prev, c.Name, ctx)
			}
			owner[ctx] = c.Name
		}
	}
	out := []string{contextDeploySource}
	for _, c := range checks.Checks {
		if c.PlanEnv == env {
			out = append(out, c.Contexts...)
		}
	}
	return append(out, contextLineGate), nil
}

// deployRulesetBody は deploy-<branch>.json の形。キーの順は人が書く宣言と揃える。
type deployRulesetBody struct {
	Name        string `json:"name"`
	Target      string `json:"target"`
	SourceType  string `json:"source_type"`
	Enforcement string `json:"enforcement"`
	Conditions  struct {
		RefName struct {
			Exclude []string `json:"exclude"`
			Include []string `json:"include"`
		} `json:"ref_name"`
	} `json:"conditions"`
	Rules        []deployRule `json:"rules"`
	BypassActors []any        `json:"bypass_actors"`
}

type deployRule struct {
	Type       string             `json:"type"`
	Parameters statusChecksParams `json:"parameters"`
}

type statusChecksParams struct {
	Strict               bool          `json:"strict_required_status_checks_policy"`
	DoNotEnforceOnCreate bool          `json:"do_not_enforce_on_create"`
	Checks               []statusCheck `json:"required_status_checks"`
}

type statusCheck struct {
	Context       string `json:"context"`
	IntegrationID int    `json:"integration_id"`
}

// renderDeployRuleset は、branch だけを対象に contexts を required とする ruleset の宣言を返します。2つの空白で
// 字下げし、末尾に改行を置きます。
func renderDeployRuleset(name, branch string, contexts []string) ([]byte, error) {
	body := deployRulesetBody{Name: name, Target: "branch", SourceType: "Repository", Enforcement: "active", BypassActors: []any{}}
	body.Conditions.RefName.Exclude = []string{}
	body.Conditions.RefName.Include = []string{"refs/heads/" + branch}
	params := statusChecksParams{Checks: make([]statusCheck, 0, len(contexts))}
	for _, c := range contexts {
		params.Checks = append(params.Checks, statusCheck{Context: c, IntegrationID: githubActionsAppID})
	}
	body.Rules = []deployRule{{Type: "required_status_checks", Parameters: params}}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(body); err != nil {
		return nil, xerrors.Wrapf(errDeployRuleset, "%s の生成: %v", name, err)
	}
	return buf.Bytes(), nil
}

// checkStaleRulesets は、rulesets/ にある .json が宣言の ruleset のファイルだけであることを確かめます。残ったファイルは
// 適用も突合もされず、消えたはずの保護が宣言に在るように読める。
func checkStaleRulesets(root string, decls []rulesetDecl) error {
	declared := map[string]bool{}
	for _, d := range decls {
		declared[d.spec.path] = true
	}
	entries, err := os.ReadDir(filepath.Join(root, rulesetsDir))
	if err != nil {
		return xerrors.Wrapf(err, "%s の読み取り", rulesetsDir)
	}
	var stale []string
	for _, e := range entries {
		p := rulesetsDir + "/" + e.Name()
		if strings.HasSuffix(e.Name(), ".json") && !declared[p] {
			stale = append(stale, p)
		}
	}
	if len(stale) > 0 {
		return xerrors.Wrapf(errStaleRuleset, "%s", strings.Join(stale, ", "))
	}
	return nil
}

// checkCoverage は、common の include が保護対象の全集合と一致し、ほかの ruleset の include がその内にあることを
// 確かめます（ADR-0603 決定35）。生成し直した内容を、生成とは別の経路で読み直して比べる。
func checkCoverage(decls []rulesetDecl, protected []string) error {
	var common []string
	found := false
	others := map[string][]string{}
	for _, d := range decls {
		include, err := decodeInclude(d.rendered)
		if err != nil {
			return xerrors.Wrapf(err, "%s", d.spec.path)
		}
		if d.spec.role == roleCommon {
			common, found = include, true
			continue
		}
		others[d.spec.path] = include
	}
	if !found {
		return xerrors.Wrap(errRulesetPolicy, "common の ruleset がありません")
	}
	if !reflect.DeepEqual(sortedCopy(common), sortedCopy(protected)) {
		return xerrors.Wrapf(errRulesetPolicy, "%s の include が保護対象の全集合と一致しません", commonRulesetFile)
	}
	paths := make([]string, 0, len(others))
	for p := range others {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		for _, ref := range others[p] {
			if !slices.Contains(common, ref) {
				return xerrors.Wrapf(errRulesetPolicy, "%s の include %s が %s の include にありません", p, ref, commonRulesetFile)
			}
		}
	}
	return nil
}

func decodeInclude(src []byte) ([]string, error) {
	var decl struct {
		Conditions struct {
			RefName struct {
				Include []string `json:"include"`
			} `json:"ref_name"`
		} `json:"conditions"`
	}
	if err := json.Unmarshal(src, &decl); err != nil {
		return nil, xerrors.Wrapf(errInclude, "include の解釈: %v", err)
	}
	return decl.Conditions.RefName.Include, nil
}

func sortedCopy(s []string) []string {
	out := append([]string{}, s...)
	sort.Strings(out)
	return out
}
