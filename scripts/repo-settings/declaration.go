package main

import (
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/branchdecl"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// 分岐のパターンの単一宣言（ADR-0603 決定1）。読み方と検証は branchdecl が持つ。
type branches = branchdecl.Branches

// environment は、デプロイの environment 1件の宣言（ADR-0603 決定4）。
// パターンは持たず、deploy_from が種類を名指す。展開は branches から行う（同 決定3）。
type environment struct {
	Name       string `json:"name"`
	DeployFrom string `json:"deploy_from"`
	// CanAdminsBypass は省略を許さないためにポインタで受ける。
	CanAdminsBypass *bool `json:"can_admins_bypass"`
}

const (
	deployFromProtected    = "protected"
	deployFromProtectedPR  = "protected-and-pull-requests"
	deployFromDeployBranch = "deploy-branch"
	deployFromAny          = "any"
)

// pullRequestRef は、plan の environment が Pull Request の job を通すために許す ref。
// 宣言の値ではなく GitHub が Pull Request の run に与える ref の形である。
const pullRequestRef = "refs/pull/*/merge"

var (
	errDeclaration      = branchdecl.ErrDeclaration
	errEmptyDeclaration = branchdecl.ErrEmpty
)

var environmentKeys = map[string]bool{"name": true, "deploy_from": true, "can_admins_bypass": true}

// vulnerabilityReporting は、Private Vulnerability Reporting の宣言（ADR-0603 決定4）。
type vulnerabilityReporting struct {
	// Enabled は省略と null を許さないためにポインタで受ける。
	Enabled *bool `json:"enabled"`
}

var vulnerabilityReportingKeys = map[string]bool{"enabled": true}

func readVulnerabilityReporting(path string) (bool, error) {
	var v vulnerabilityReporting
	if err := branchdecl.DecodeStrict(path, &v, vulnerabilityReportingKeys); err != nil {
		return false, err
	}
	if v.Enabled == nil {
		return false, xerrors.Wrapf(errDeclaration, "%s: enabled がありません", path)
	}
	return *v.Enabled, nil
}

func readEnvironments(path string) ([]environment, error) {
	var envs []environment
	if err := branchdecl.DecodeStrict(path, &envs, environmentKeys); err != nil {
		return nil, err
	}
	if err := validateEnvironments(envs); err != nil {
		return nil, xerrors.Wrapf(err, "%s", path)
	}
	return envs, nil
}

// validateEnvironments は、名前の接尾辞と deploy_from の対応（ADR-0602 決定14）を宣言の側で固定します。
func validateEnvironments(envs []environment) error {
	if len(envs) == 0 {
		return errEmptyDeclaration
	}
	seen := map[string]bool{}
	for _, e := range envs {
		if e.Name == "" {
			return xerrors.Wrap(errDeclaration, "name が空の environment があります")
		}
		if seen[e.Name] {
			return xerrors.Wrapf(errDeclaration, "environment %q が重複しています", e.Name)
		}
		seen[e.Name] = true
		switch e.DeployFrom {
		case deployFromProtected, deployFromProtectedPR, deployFromAny:
		case deployFromDeployBranch:
			if _, ok := identityBase(e.Name); !ok {
				return xerrors.Wrapf(errDeclaration, "environment %q は deployment-identity の env の名前ではないので deploy_from %q を使えません", e.Name, e.DeployFrom)
			}
		default:
			return xerrors.Wrapf(errDeclaration, "environment %q の deploy_from %q", e.Name, e.DeployFrom)
		}
		if e.CanAdminsBypass == nil || *e.CanAdminsBypass {
			return xerrors.Wrapf(errDeclaration, "environment %q の can_admins_bypass は false でなければなりません", e.Name)
		}
		if want, ok := requiredDeployFrom(e.Name); ok && e.DeployFrom != want {
			return xerrors.Wrapf(errDeclaration, "environment %q の deploy_from は %q でなければなりません", e.Name, want)
		}
	}
	return nil
}

// requiredDeployFrom は、deployment-identity の role が名指す environment の deploy_from を返します
// （ADR-0602 決定14）。
func requiredDeployFrom(name string) (string, bool) {
	_, suffix, ok := splitIdentity(name)
	switch {
	case !ok:
		return "", false
	case isPullRequestSuffix(suffix):
		return deployFromProtectedPR, true
	default:
		return deployFromDeployBranch, true
	}
}

// rulesetIncludes は、保護対象の全集合（common の conditions.ref_name.include に置く値）を返します。
// 線は階層をまたいで保護する。
func rulesetIncludes(b branches) []string {
	out := make([]string, 0, len(b.Deploy)+len(b.Lines.Prefixes()))
	for _, name := range b.DeployNames() {
		out = append(out, "refs/heads/"+name)
	}
	for _, prefix := range b.Lines.Prefixes() {
		out = append(out, "refs/heads/"+prefix+"**/*")
	}
	return out
}

// workflow の印 `# branch-set: <集合>` が名指す集合。
const (
	// setRuleset は、ruleset が保護する集合（デプロイブランチと各線）。
	setRuleset = "ruleset"
	setDeploy  = "deploy"
	setLines   = "lines"
)

// triggerPatterns は、workflow の on.<event>.branches に置く集合 set の値を返します。線は ruleset と同じく
// 階層をまたぐ（<prefix>**）。
func triggerPatterns(b branches, set string) ([]string, error) {
	var out []string
	switch set {
	case setRuleset:
		out = append(out, b.DeployNames()...)
		out = append(out, lineGlobs(b)...)
	case setDeploy:
		out = append(out, b.DeployNames()...)
	case setLines:
		out = append(out, lineGlobs(b)...)
	default:
		return nil, xerrors.Wrapf(errBranchSet, "未知の集合 %q（%s / %s / %s）", set, setRuleset, setDeploy, setLines)
	}
	if len(out) == 0 {
		return nil, xerrors.Wrapf(errBranchSet, "集合 %s の展開が0件です", set)
	}
	return out, nil
}

func lineGlobs(b branches) []string {
	prefixes := b.Lines.Prefixes()
	out := make([]string, 0, len(prefixes))
	for _, p := range prefixes {
		out = append(out, p+"**")
	}
	return out
}

// environmentPatterns は、environment の deploy_from を deployment branch policy のパターンへ展開します。
// any は policy を持たないので nil を返す。deploy-branch は、名前の env のデプロイブランチ1つだけを許す。
// env のデプロイブランチが宣言に在ることは checkDeployEnvs が先に確かめる。
func environmentPatterns(b branches, e environment) []string {
	switch e.DeployFrom {
	case deployFromAny:
		return nil
	case deployFromDeployBranch:
		env, _ := identityBase(e.Name)
		if d, ok := b.DeployForEnv(env); ok {
			return []string{d.Branch}
		}
		return []string{}
	}
	out := make([]string, 0, len(b.Deploy)+len(b.Lines.Prefixes())+1)
	out = append(out, b.DeployNames()...)
	for _, prefix := range b.Lines.Prefixes() {
		out = append(out, prefix+"*")
	}
	if e.DeployFrom == deployFromProtectedPR {
		out = append(out, pullRequestRef)
	}
	return out
}
