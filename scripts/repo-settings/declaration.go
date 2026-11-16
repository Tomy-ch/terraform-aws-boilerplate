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

// requiredDeployFrom は、deployment-identity の role が名指す environment の deploy_from を返します。
// plan の environment は保護ブランチと Pull Request を許し、ほかはその env のデプロイブランチだけを許す
// （ADR-0602 決定14）。
func requiredDeployFrom(name string) (string, bool) {
	_, suffix, ok := splitIdentity(name)
	switch {
	case !ok:
		return "", false
	case suffix == planSuffix:
		return deployFromProtectedPR, true
	default:
		return deployFromDeployBranch, true
	}
}

// rulesetIncludes は、保護設定の宣言の conditions.ref_name.include に置く値を返します。
// lines は階層をまたいで保護する。
func rulesetIncludes(b branches) []string {
	out := make([]string, 0, len(b.Deploy)+len(b.Lines))
	for _, name := range b.DeployNames() {
		out = append(out, "refs/heads/"+name)
	}
	for _, prefix := range b.Lines {
		out = append(out, "refs/heads/"+prefix+"**/*")
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
	out := make([]string, 0, len(b.Deploy)+len(b.Lines)+1)
	out = append(out, b.DeployNames()...)
	for _, prefix := range b.Lines {
		out = append(out, prefix+"*")
	}
	if e.DeployFrom == deployFromProtectedPR {
		out = append(out, pullRequestRef)
	}
	return out
}
