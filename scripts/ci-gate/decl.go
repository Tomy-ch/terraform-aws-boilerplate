package main

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// declFile は、検査と path の対応の宣言のリポジトリ直下からの位置。
const declFile = ".github/settings/checks.json"

const (
	scopeTree   = "tree"
	scopeChange = "change"

	baseLine   = "line"
	baseDeploy = "deploy"
)

var (
	// errDecl は、宣言を読めないか、形が誤っていることを表す。
	errDecl = xerrors.New("検査の宣言を解釈できません")
	// errNoChecks は、宣言に検査が1つも無いことを表す。gate が何も見ずに成功する形なので、合格と区別する。
	errNoChecks = xerrors.New("検査の宣言に検査がありません")

	nameRe     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	workflowRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*\.yaml$`)
	envRe      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
)

// check は、gate が呼ぶ検査1つの宣言。Name は gate の workflow の caller の job id で、needs の key になる。
type check struct {
	Name     string   `json:"name"`
	Workflow string   `json:"workflow"`
	Paths    []string `json:"paths"`
	Scope    string   `json:"scope"`
	Required *bool    `json:"required"`
	PlanEnv  string   `json:"plan_env,omitempty"`
	// Bases は、その検査が走る Pull Request の base の役割（line・deploy）。無ければ両方で走る。
	Bases []string `json:"bases,omitempty"`
}

type decl struct {
	Checks []check `json:"checks"`
}

func (c check) required() bool { return c.Required != nil && *c.Required }

// parseDecl は宣言を読み、形を検める。未知のキー、欠けたキー、重複した名前、解釈できない path はどれも誤りとする。
// 読み飛ばすと、その検査が「走るべきでない」と判定されて gate が黙って範囲を縮める。
func parseDecl(raw []byte) (decl, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var d decl
	if err := dec.Decode(&d); err != nil {
		return decl{}, xerrors.Wrapf(errDecl, "%v", err)
	}
	if dec.More() {
		return decl{}, xerrors.Wrapf(errDecl, "宣言のあとに余分な値があります")
	}
	if len(d.Checks) == 0 {
		return decl{}, errNoChecks
	}
	seen := map[string]bool{}
	for i, c := range d.Checks {
		at := func(format string, args ...any) error {
			return xerrors.Wrapf(errDecl, "checks[%d]（%s）: "+format, append([]any{i, c.Name}, args...)...)
		}
		switch {
		case !nameRe.MatchString(c.Name):
			return decl{}, at("name は小文字・数字・ハイフンだけにします")
		case seen[c.Name]:
			return decl{}, at("name が重複しています")
		case !workflowRe.MatchString(c.Workflow):
			return decl{}, at("workflow は .github/workflows の下のファイル名（.yaml）にします")
		case c.Paths == nil:
			return decl{}, at("paths がありません（常に走る検査は空の配列にします）")
		case c.Scope != scopeTree && c.Scope != scopeChange:
			return decl{}, at("scope は %q か %q にします", scopeTree, scopeChange)
		case c.Required == nil:
			return decl{}, at("required がありません")
		case c.PlanEnv != "" && !envRe.MatchString(c.PlanEnv):
			return decl{}, at("plan_env の形が誤っています")
		case c.Bases != nil && !validBases(c.Bases):
			return decl{}, at("bases は %q と %q の重複の無い空でない配列にします", baseLine, baseDeploy)
		}
		for _, p := range c.Paths {
			if err := validateGlob(p); err != nil {
				return decl{}, at("%v", err)
			}
		}
		seen[c.Name] = true
	}
	return d, nil
}

// names は宣言の検査の名前を宣言の順に返す。
func (d decl) names() []string {
	out := make([]string, 0, len(d.Checks))
	for _, c := range d.Checks {
		out = append(out, c.Name)
	}
	return out
}

func joinNames(names []string) string { return strings.Join(names, ", ") }

func validBases(bases []string) bool {
	if len(bases) == 0 {
		return false
	}
	seen := map[string]bool{}
	for _, b := range bases {
		if (b != baseLine && b != baseDeploy) || seen[b] {
			return false
		}
		seen[b] = true
	}
	return true
}

// runsOn は、base の役割 role の Pull Request でその検査が走るかを返す。
func (c check) runsOn(role string) bool {
	if c.Bases == nil {
		return true
	}
	for _, b := range c.Bases {
		if b == role {
			return true
		}
	}
	return false
}
