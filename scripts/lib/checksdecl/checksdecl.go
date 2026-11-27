// Package checksdecl は、gate が呼ぶ検査と path の対応の宣言（`.github/settings/checks.json`）を読む
// （ADR-0603 決定17・19・20）。複数の道具がこの宣言を読む。各々が読むと、
// 一方だけが読める宣言が生まれ、もう一方は黙って検査の対象を失う。
package checksdecl

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// File は、宣言のリポジトリ直下からの位置。
const File = ".github/settings/checks.json"

// 検査の scope（tree: 木だけで決まる検査、change: 変更の範囲で決まる検査）と、走る base の役割。
const (
	ScopeTree   = "tree"
	ScopeChange = "change"

	BaseLine   = "line"
	BaseDeploy = "deploy"
)

var (
	// ErrDecl は、宣言を読めないか、形が誤っていることを表す。
	ErrDecl = xerrors.New("検査の宣言を解釈できません")
	// ErrNoChecks は、宣言に検査が1つも無いことを表す。gate が何も見ずに成功する形なので、合格と区別する。
	ErrNoChecks = xerrors.New("検査の宣言に検査がありません")

	nameRe     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	workflowRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*\.yaml$`)
	envRe      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
)

// Check は、gate が呼ぶ検査1つの宣言。Name は gate の workflow の caller の job id で、needs の key になる。
type Check struct {
	Name     string   `json:"name"`
	Workflow string   `json:"workflow"`
	Paths    []string `json:"paths"`
	Scope    string   `json:"scope"`
	Required *bool    `json:"required"`
	PlanEnv  string   `json:"plan_env,omitempty"`
	// Bases は、その検査が走る Pull Request の base の役割（line・deploy）。無ければ両方で走る。
	Bases []string `json:"bases,omitempty"`
	// Contexts は、plan_env を持つ検査が、その env のデプロイブランチの ruleset で直接 required とする context。
	Contexts []string `json:"contexts,omitempty"`
}

// Decl は宣言の全体。
type Decl struct {
	Checks []Check `json:"checks"`
}

// IsRequired は、gate の合否に数える検査かを返す。
func (c Check) IsRequired() bool { return c.Required != nil && *c.Required }

// Parse は宣言を読み、形を検める。未知のキー、欠けたキー、重複した名前、解釈できない path はどれも誤りとする。
// 読み飛ばすと、その検査が「走るべきでない」と判定されて gate が黙って範囲を縮める。
func Parse(raw []byte) (Decl, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var d Decl
	if err := dec.Decode(&d); err != nil {
		return Decl{}, xerrors.Wrapf(ErrDecl, "%v", err)
	}
	if dec.More() {
		return Decl{}, xerrors.Wrapf(ErrDecl, "宣言のあとに余分な値があります")
	}
	if len(d.Checks) == 0 {
		return Decl{}, ErrNoChecks
	}
	seen := map[string]bool{}
	for i, c := range d.Checks {
		at := func(format string, args ...any) error {
			return xerrors.Wrapf(ErrDecl, "checks[%d]（%s）: "+format, append([]any{i, c.Name}, args...)...)
		}
		switch {
		case !nameRe.MatchString(c.Name):
			return Decl{}, at("name は小文字・数字・ハイフンだけにします")
		case seen[c.Name]:
			return Decl{}, at("name が重複しています")
		case !workflowRe.MatchString(c.Workflow):
			return Decl{}, at("workflow は .github/workflows の下のファイル名（.yaml）にします")
		case c.Paths == nil:
			return Decl{}, at("paths がありません（常に走る検査は空の配列にします）")
		case c.Scope != ScopeTree && c.Scope != ScopeChange:
			return Decl{}, at("scope は %q か %q にします", ScopeTree, ScopeChange)
		case c.Required == nil:
			return Decl{}, at("required がありません")
		case c.PlanEnv != "" && !envRe.MatchString(c.PlanEnv):
			return Decl{}, at("plan_env の形が誤っています")
		case (c.PlanEnv == "") != (len(c.Contexts) == 0):
			return Decl{}, at("contexts は plan_env を持つ検査にだけ、1つ以上置きます")
		case c.Bases != nil && !validBases(c.Bases):
			return Decl{}, at("bases は %q と %q の重複の無い空でない配列にします", BaseLine, BaseDeploy)
		}
		for _, p := range c.Paths {
			if err := ValidateGlob(p); err != nil {
				return Decl{}, at("%v", err)
			}
		}
		seen[c.Name] = true
	}
	return d, nil
}

// Names は宣言の検査の名前を宣言の順に返す。
func (d Decl) Names() []string {
	out := make([]string, 0, len(d.Checks))
	for _, c := range d.Checks {
		out = append(out, c.Name)
	}
	return out
}

// CallerPrefix は、gate と線の検証の workflow で検査を呼ぶ caller の job の id の接頭辞。skip された caller の
// job はその id を名前とする check を作るので、検査の job と名前を分け、skipped（成功と数える）の check が同じ
// 名前の required の context に重ならないようにする（ADR-0603 決定38）。
const CallerPrefix = "call-"

// CallerJob は、検査 name を呼ぶ caller の job の id を返す。
func CallerJob(name string) string { return CallerPrefix + name }

// TreeChecks は、線の検証が走らせる検査（scope が tree で required、かつ plan_env を持たないもの）の名前を宣言の
// 順に返す。plan は AWS の状態に依り、木だけでは決まらない（ADR-0603 決定13）。
// デプロイブランチへ入る木の plan は、その ruleset が直接 required とする。
func (d Decl) TreeChecks() []string {
	var out []string
	for _, c := range d.Checks {
		if c.Scope == ScopeTree && c.IsRequired() && c.PlanEnv == "" {
			out = append(out, c.Name)
		}
	}
	return out
}

// JoinNames は、名前を報告のために繋ぐ。
func JoinNames(names []string) string { return strings.Join(names, ", ") }

func validBases(bases []string) bool {
	if len(bases) == 0 {
		return false
	}
	seen := map[string]bool{}
	for _, b := range bases {
		if (b != BaseLine && b != BaseDeploy) || seen[b] {
			return false
		}
		seen[b] = true
	}
	return true
}

// RunsOn は、base の役割 role の Pull Request でその検査が走るかを返す。
func (c Check) RunsOn(role string) bool {
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
