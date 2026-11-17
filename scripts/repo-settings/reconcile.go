package main

import (
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strings"
)

type desired struct {
	// Ruleset は保護設定の宣言そのもの（GitHub へ送る body でもある）。
	Ruleset map[string]any
	Envs    []desiredEnv
}

// Patterns が nil なら deployment branch policy を持たない。
type desiredEnv struct {
	Name     string
	Patterns []string
}

type live struct {
	// Ruleset は同名の ruleset の内容。無ければ nil。
	Ruleset   map[string]any
	RulesetID int64
	Envs      map[string]liveEnv
}

type liveEnv struct {
	// CanAdminsBypass は応答に無ければ nil。
	CanAdminsBypass *bool
	// RulesMissing は protection_rules が応答に無いこと。そのとき Reviewers と WaitTimer は観測していない。
	RulesMissing bool
	Reviewers    int
	WaitTimer    int
	// Policy は deployment branch policy。持たなければ nil。
	Policy *branchPolicy
	// Patterns は custom の branch policy。名前から id を引く。type が branch でないものは名前に `type:` を前置する。
	Patterns map[string]int64
}

type branchPolicy struct {
	ProtectedBranches    bool `json:"protected_branches"`
	CustomBranchPolicies bool `json:"custom_branch_policies"`
}

var customOnly = branchPolicy{ProtectedBranches: false, CustomBranchPolicies: true}

// diff は宣言と実態の差分。
type diff struct {
	Ruleset rulesetDiff
	Envs    []envDiff
	// Extra は実態にだけ在る environment。sync は消さない（消すと secret ごと失われる）。
	Extra []string
	// Unobserved は読み取りの権限で観測できず、突合しなかったもの。
	Unobserved []string
}

type rulesetDiff struct {
	Missing  bool
	ID       int64
	Findings []string
}

type envDiff struct {
	Name           string
	Put            bool
	AddPatterns    []string
	RemovePatterns []int64
	Findings       []string
}

func (d diff) findings() []string {
	var out []string
	for _, f := range d.Ruleset.Findings {
		out = append(out, "ruleset: "+f)
	}
	for _, e := range d.Envs {
		for _, f := range e.Findings {
			out = append(out, fmt.Sprintf("environment %s: %s", e.Name, f))
		}
	}
	for _, name := range d.Extra {
		out = append(out, fmt.Sprintf("environment %s: 宣言に無い environment が実在します", name))
	}
	return out
}

func computeDiff(want desired, got live) diff {
	d := diff{Ruleset: diffRuleset(want.Ruleset, got)}
	if got.Ruleset != nil {
		if _, ok := got.Ruleset["bypass_actors"]; !ok {
			d.Unobserved = append(d.Unobserved, "ruleset の bypass_actors（write 権限の無い読み取りには返らない）")
		}
	}
	declared := map[string]bool{}
	for _, e := range want.Envs {
		declared[e.Name] = true
		ed := diffEnv(e, got.Envs)
		if len(ed.Findings) > 0 {
			d.Envs = append(d.Envs, ed)
		}
	}
	for name := range got.Envs {
		if !declared[name] {
			d.Extra = append(d.Extra, name)
		}
	}
	sort.Strings(d.Extra)
	return d
}

// diffRuleset は ruleset の差分を計算します。宣言が持つキーだけを比べ、応答にだけ在るキー（id 等）は見ません。
func diffRuleset(want map[string]any, got live) rulesetDiff {
	if got.Ruleset == nil {
		return rulesetDiff{Missing: true, Findings: []string{"宣言の ruleset が実在しません"}}
	}
	rd := rulesetDiff{ID: got.RulesetID}
	keys := make([]string, 0, len(want))
	for k := range want {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		switch k {
		case "rules":
			rd.Findings = append(rd.Findings, diffRules(want[k], got.Ruleset[k])...)
		case "bypass_actors":
			if v, ok := got.Ruleset[k]; ok && !subsetMatch(want[k], v) {
				rd.Findings = append(rd.Findings, "bypass_actors が宣言と異なります")
			}
		default:
			if !subsetMatch(want[k], got.Ruleset[k]) {
				rd.Findings = append(rd.Findings, k+" が宣言と異なります")
			}
		}
	}
	return rd
}

// diffRules はルールを type で照合して比べます。順序は問いません。
func diffRules(want, got any) []string {
	wantRules, _ := rulesByType(want)
	gotRules, dups := rulesByType(got)
	var out []string
	for _, t := range dups {
		out = append(out, "ルール "+t+" が実態に2件以上あります")
	}
	for _, t := range sortedKeys(wantRules) {
		g, ok := gotRules[t]
		if !ok {
			out = append(out, "ルール "+t+" が実在しません")
			continue
		}
		if !subsetMatch(wantRules[t], g) {
			out = append(out, "ルール "+t+" が宣言と異なります"+contextDelta(wantRules[t], g))
		}
	}
	for _, t := range sortedKeys(gotRules) {
		if _, ok := wantRules[t]; !ok {
			out = append(out, "宣言に無いルール "+t+" が実在します")
		}
	}
	return out
}

// contextDelta は、required_status_checks のルールどうしの context の過不足を文にします。
// 過不足が無ければ（他のパラメータだけが違えば）空を返します。
func contextDelta(want, got any) string {
	wantSet, gotSet := statusContexts(want), statusContexts(got)
	var missing, extra []string
	for c := range wantSet {
		if !gotSet[c] {
			missing = append(missing, c)
		}
	}
	for c := range gotSet {
		if !wantSet[c] {
			extra = append(extra, c)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	var parts []string
	if len(missing) > 0 {
		parts = append(parts, "実態に無い context: "+strings.Join(missing, ", "))
	}
	if len(extra) > 0 {
		parts = append(parts, "宣言に無い context: "+strings.Join(extra, ", "))
	}
	if len(parts) == 0 {
		return ""
	}
	return "（" + strings.Join(parts, "。") + "）"
}

func statusContexts(rule any) map[string]bool {
	out := map[string]bool{}
	r, _ := rule.(map[string]any)
	params, _ := r["parameters"].(map[string]any)
	checks, _ := params["required_status_checks"].([]any)
	for _, item := range checks {
		check, _ := item.(map[string]any)
		if c, ok := check["context"].(string); ok {
			out[c] = true
		}
	}
	return out
}

// rulesByType はルールの列を type から引ける形にし、2件以上在った type を返します。
// 宣言の側の重複は checkRuleset が先に拒む。
func rulesByType(v any) (map[string]any, []string) {
	out := map[string]any{}
	var dups []string
	list, _ := v.([]any)
	for _, item := range list {
		rule, _ := item.(map[string]any)
		t, _ := rule["type"].(string)
		if _, ok := out[t]; ok {
			dups = append(dups, t)
		}
		out[t] = rule
	}
	return out, dups
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// subsetMatch は、want が持つものがすべて got に在るかを返します。
// object は want のキーだけを見る。配列は長さが等しく、要素が順序を問わず1対1で当たることを要求する。
func subsetMatch(want, got any) bool {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return false
		}
		for k, wv := range w {
			gv, ok := g[k]
			if !ok || !subsetMatch(wv, gv) {
				return false
			}
		}
		return true
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return false
		}
		return assignAll(w, g, make([]bool, len(g)))
	default:
		return reflect.DeepEqual(want, got)
	}
}

// assignAll は want の要素を got の未使用の要素へ1対1で割り当てられるかを、後戻りしながら探します。
// 先に当たった要素を取るだけでは、部分一致が重なるときに割り当てがあっても見落とす。
func assignAll(want, got []any, used []bool) bool {
	for wi, wv := range want {
		for i, gv := range got {
			if used[i] || !subsetMatch(wv, gv) {
				continue
			}
			used[i] = true
			if assignAll(want[wi+1:], got, used) {
				return true
			}
			used[i] = false
		}
		return false
	}
	return true
}

func diffEnv(want desiredEnv, gotEnvs map[string]liveEnv) envDiff {
	ed := envDiff{Name: want.Name}
	got, ok := gotEnvs[want.Name]
	if !ok {
		ed.Put = true
		ed.AddPatterns = append([]string{}, want.Patterns...)
		ed.Findings = append(ed.Findings, "実在しません")
		return ed
	}
	switch {
	case got.CanAdminsBypass == nil:
		ed.Put = true
		ed.Findings = append(ed.Findings, "can_admins_bypass が応答にありません")
	case *got.CanAdminsBypass:
		ed.Put = true
		ed.Findings = append(ed.Findings, "管理者による保護の迂回が有効です")
	}
	if got.RulesMissing {
		ed.Put = true
		ed.Findings = append(ed.Findings, "protection_rules が応答にありません")
	}
	if got.Reviewers > 0 {
		ed.Put = true
		ed.Findings = append(ed.Findings, "承認者が設定されています")
	}
	if got.WaitTimer > 0 {
		ed.Put = true
		ed.Findings = append(ed.Findings, "待機時間が設定されています")
	}
	if want.Patterns == nil {
		if got.Policy != nil {
			ed.Put = true
			ed.Findings = append(ed.Findings, "宣言に無い deployment branch policy があります")
		}
		return ed
	}
	if got.Policy == nil || *got.Policy != customOnly {
		ed.Put = true
		ed.Findings = append(ed.Findings, "deployment branch policy が宣言のパターンだけを許す形ではありません")
	}
	wantSet := map[string]bool{}
	for _, p := range want.Patterns {
		wantSet[p] = true
		if _, ok := got.Patterns[p]; !ok {
			ed.AddPatterns = append(ed.AddPatterns, p)
			ed.Findings = append(ed.Findings, "パターン "+p+" がありません")
		}
	}
	names := make([]string, 0, len(got.Patterns))
	for name := range got.Patterns {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !wantSet[name] {
			ed.RemovePatterns = append(ed.RemovePatterns, got.Patterns[name])
			ed.Findings = append(ed.Findings, "宣言に無いパターン "+name+" があります")
		}
	}
	return ed
}

type request struct {
	Method string
	Path   string
	Body   any
}

const (
	onlyRuleset      = "ruleset"
	onlyEnvironments = "environments"
)

// planRequests は差分を、それを解消する要求列へ変えます。差分が無ければ空を返します。
func planRequests(d diff, want desired, repo, only string) []request {
	var out []request
	base := "/repos/" + repo
	if only == onlyRuleset {
		switch {
		case d.Ruleset.Missing:
			out = append(out, request{Method: "POST", Path: base + "/rulesets", Body: want.Ruleset})
		case len(d.Ruleset.Findings) > 0:
			out = append(out, request{Method: "PUT", Path: fmt.Sprintf("%s/rulesets/%d", base, d.Ruleset.ID), Body: want.Ruleset})
		}
		return out
	}
	patterns := map[string][]string{}
	for _, e := range want.Envs {
		patterns[e.Name] = e.Patterns
	}
	for _, ed := range d.Envs {
		envPath := base + "/environments/" + url.PathEscape(ed.Name)
		if ed.Put {
			var policy *branchPolicy
			if patterns[ed.Name] != nil {
				policy = &customOnly
			}
			out = append(out, request{Method: "PUT", Path: envPath, Body: map[string]any{
				"wait_timer":               0,
				"prevent_self_review":      false,
				"reviewers":                nil,
				"deployment_branch_policy": policy,
				"can_admins_bypass":        false,
			}})
		}
		for _, id := range ed.RemovePatterns {
			out = append(out, request{Method: "DELETE", Path: fmt.Sprintf("%s/deployment-branch-policies/%d", envPath, id)})
		}
		for _, p := range ed.AddPatterns {
			out = append(out, request{Method: "POST", Path: envPath + "/deployment-branch-policies", Body: map[string]any{
				"name": p,
				"type": "branch",
			}})
		}
	}
	return out
}
