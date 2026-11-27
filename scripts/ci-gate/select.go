package main

import (
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/branchdecl"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/checksdecl"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var (
	errPlanEnv = xerrors.New("plan_env の env を持つデプロイブランチがありません")
	// errBase は、Pull Request の base が線でもデプロイブランチでもないことを表す。gate は保護対象宛の Pull Request
	// でだけ走るので、ここへ来るのは宣言と起動条件のずれである。
	errBase = xerrors.New("base が線でもデプロイブランチでもありません")
)

// selectChecks は、変更したファイルと Pull Request の base のブランチから、走るべき検査の名前を宣言の順に返す。
//
//   - paths が空の検査は常に走る。
//   - そうでなければ、変更したファイルのどれかが paths のどれかに当たるときに走る。
//   - plan_env を持つ検査は、さらに base がその env へ変更を運ぶ線（宣言の from）のときだけ走る。
//   - bases を持つ検査は、base の役割（線か、デプロイブランチか）がそこに含まれるときだけ走る。
//
// 変更したファイルが0件の Pull Request でも、常に走る検査は走る。
func selectChecks(d checksdecl.Decl, files []string, baseRef string, b branchdecl.Branches) ([]string, error) {
	role, err := baseRole(b, baseRef)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, c := range d.Checks {
		if !c.RunsOn(role) {
			continue
		}
		if c.PlanEnv != "" {
			feeds, err := feedsEnv(b, c.PlanEnv, baseRef)
			if err != nil {
				return nil, xerrors.Wrapf(err, "%s", c.Name)
			}
			if !feeds {
				continue
			}
		}
		if len(c.Paths) == 0 || touches(c.Paths, files) {
			out = append(out, c.Name)
		}
	}
	return out, nil
}

func touches(patterns, files []string) bool {
	for _, f := range files {
		for _, p := range patterns {
			if checksdecl.MatchGlob(p, f) {
				return true
			}
		}
	}
	return false
}

// feedsEnv は、base のブランチ baseRef が env へ変更を運ぶ線かを返す。env を持つデプロイブランチが宣言に無ければ
// errPlanEnv にする —— 黙って「運ばない」と読むと、その plan が二度と走らない。
func feedsEnv(b branchdecl.Branches, env, baseRef string) (bool, error) {
	for _, d := range b.Deploy {
		if d.Env != env {
			continue
		}
		for _, prefix := range d.From {
			if strings.HasPrefix(baseRef, prefix) && b.IsLine(baseRef) {
				return true, nil
			}
		}
		return false, nil
	}
	return false, xerrors.Wrapf(errPlanEnv, "%q", env)
}

func baseRole(b branchdecl.Branches, baseRef string) (string, error) {
	if b.IsLine(baseRef) {
		return checksdecl.BaseLine, nil
	}
	for _, name := range b.DeployNames() {
		if name == baseRef {
			return checksdecl.BaseDeploy, nil
		}
	}
	return "", xerrors.Wrapf(errBase, "%q", baseRef)
}
