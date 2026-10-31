package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func decodeAny(t *testing.T, s string) map[string]any {
	t.Helper()
	var v map[string]any
	require.NoError(t, json.Unmarshal([]byte(s), &v))
	return v
}

// editorconfig-checker-disable -- raw string の fixture は埋め込んだ JSON の字下げを持つ。撤回: fixture を外部ファイルへ移したとき

const wantRulesetJSON = `{
  "name": "branch-protection",
  "enforcement": "active",
  "rules": [
    {"type": "deletion"},
    {"type": "required_status_checks", "parameters": {"required_status_checks": [{"context": "a"}, {"context": "b"}]}}
  ],
  "bypass_actors": []
}`

// liveRulesetJSON は wantRulesetJSON と一致する実態。応答にだけ在るキーと、順序の違いを含む。
const liveRulesetJSON = `{
  "id": 7,
  "name": "branch-protection",
  "enforcement": "active",
  "source": "o/r",
  "rules": [
    {"type": "required_status_checks", "parameters": {"required_status_checks": [{"context": "b", "integration_id": 1}, {"context": "a"}], "extra": true}},
    {"type": "deletion"}
  ],
  "bypass_actors": []
}`

// editorconfig-checker-enable

func Test_diff_findings(t *testing.T) {
	t.Parallel()

	d := diff{
		Ruleset: rulesetDiff{Findings: []string{"r1"}},
		Envs:    []envDiff{{Name: "e", Findings: []string{"f1", "f2"}}},
		Extra:   []string{"x"},
	}
	t.Run("ruleset・environment・宣言に無い environment の順に1行ずつ並べる", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{
			"ruleset: r1",
			"environment e: f1",
			"environment e: f2",
			"environment x: 宣言に無い environment が実在します",
		}, d.findings())
	})

	t.Run("差分が無ければ空", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, diff{}.findings())
	})
}

func Test_computeDiff(t *testing.T) {
	t.Parallel()

	want := desired{
		Ruleset: decodeAny(t, wantRulesetJSON),
		Envs: []desiredEnv{
			{Name: "a-plan", Patterns: []string{"main", pullRequestRef}},
			{Name: "copilot"},
		},
	}
	matching := live{
		Ruleset:   decodeAny(t, liveRulesetJSON),
		RulesetID: 7,
		Envs: map[string]liveEnv{
			"a-plan":  {CanAdminsBypass: boolPtr(false), Policy: &customOnly, Patterns: map[string]int64{"main": 1, pullRequestRef: 2}},
			"copilot": {CanAdminsBypass: boolPtr(false)},
		},
	}

	t.Run("宣言どおりの実態には差分が無い", func(t *testing.T) {
		t.Parallel()
		d := computeDiff(want, matching)
		assert.Empty(t, d.findings())
		assert.Empty(t, d.Unobserved)
	})

	t.Run("宣言に無い environment は Extra に入る", func(t *testing.T) {
		t.Parallel()
		got := matching
		got.Envs = map[string]liveEnv{"zz": {}, "a-plan": matching.Envs["a-plan"], "copilot": matching.Envs["copilot"], "aa": {}}
		d := computeDiff(want, got)
		assert.Equal(t, []string{"aa", "zz"}, d.Extra)
	})

	t.Run("bypass_actors が応答に無ければ観測できなかったと記録し、ずれとはしない", func(t *testing.T) {
		t.Parallel()
		got := matching
		got.Ruleset = decodeAny(t, liveRulesetJSON)
		delete(got.Ruleset, "bypass_actors")
		d := computeDiff(want, got)
		assert.Empty(t, d.findings())
		require.Len(t, d.Unobserved, 1)
		assert.Contains(t, d.Unobserved[0], "bypass_actors")
	})

	t.Run("environment の差分を集める", func(t *testing.T) {
		t.Parallel()
		got := matching
		got.Envs = map[string]liveEnv{"copilot": {CanAdminsBypass: boolPtr(true)}}
		d := computeDiff(want, got)
		require.Len(t, d.Envs, 2)
		assert.Equal(t, "a-plan", d.Envs[0].Name)
		assert.Equal(t, "copilot", d.Envs[1].Name)
	})
}

func Test_diffRuleset(t *testing.T) {
	t.Parallel()

	want := decodeAny(t, wantRulesetJSON)

	t.Run("ruleset が無ければ Missing", func(t *testing.T) {
		t.Parallel()
		rd := diffRuleset(want, live{})
		assert.True(t, rd.Missing)
		assert.Equal(t, []string{"宣言の ruleset が実在しません"}, rd.Findings)
	})

	t.Run("宣言が持つキーだけを比べる", func(t *testing.T) {
		t.Parallel()
		rd := diffRuleset(want, live{Ruleset: decodeAny(t, liveRulesetJSON), RulesetID: 7})
		assert.False(t, rd.Missing)
		assert.Equal(t, int64(7), rd.ID)
		assert.Empty(t, rd.Findings)
	})

	t.Run("値の違うキーを報せる", func(t *testing.T) {
		t.Parallel()
		got := decodeAny(t, liveRulesetJSON)
		got["enforcement"] = "disabled"
		rd := diffRuleset(want, live{Ruleset: got})
		assert.Equal(t, []string{"enforcement が宣言と異なります"}, rd.Findings)
	})

	t.Run("入れ子の object の違いと、実態に無いキーを報せる", func(t *testing.T) {
		t.Parallel()
		w := decodeAny(t, wantRulesetJSON)
		w["conditions"] = map[string]any{"ref_name": map[string]any{"include": []any{"refs/heads/main"}}}
		w["target"] = "branch"
		got := decodeAny(t, liveRulesetJSON)
		got["conditions"] = map[string]any{"ref_name": map[string]any{"include": []any{"refs/heads/other"}}}
		rd := diffRuleset(w, live{Ruleset: got})
		assert.Equal(t, []string{"conditions が宣言と異なります", "target が宣言と異なります"}, rd.Findings)
	})

	t.Run("bypass の列挙を報せる", func(t *testing.T) {
		t.Parallel()
		got := decodeAny(t, liveRulesetJSON)
		got["bypass_actors"] = []any{map[string]any{"actor_id": 1.0}}
		rd := diffRuleset(want, live{Ruleset: got})
		assert.Equal(t, []string{"bypass_actors が宣言と異なります"}, rd.Findings)
	})

	t.Run("bypass_actors が応答に無ければ比べない", func(t *testing.T) {
		t.Parallel()
		got := decodeAny(t, liveRulesetJSON)
		delete(got, "bypass_actors")
		rd := diffRuleset(want, live{Ruleset: got})
		assert.Empty(t, rd.Findings)
	})
}

func Test_diffRules(t *testing.T) {
	t.Parallel()

	want := []any{
		map[string]any{"type": "deletion"},
		map[string]any{"type": "pull_request", "parameters": map[string]any{"x": true}},
	}

	t.Run("順序を問わず type で照合する", func(t *testing.T) {
		t.Parallel()
		got := []any{want[1], want[0]}
		assert.Empty(t, diffRules(want, got))
	})

	t.Run("欠落・相違・余分を報せる", func(t *testing.T) {
		t.Parallel()
		got := []any{
			map[string]any{"type": "pull_request", "parameters": map[string]any{"x": false}},
			map[string]any{"type": "copilot_code_review"},
		}
		assert.Equal(t, []string{
			"ルール deletion が実在しません",
			"ルール pull_request が宣言と異なります",
			"宣言に無いルール copilot_code_review が実在します",
		}, diffRules(want, got))
	})

	t.Run("実態の同じ type のルールの重複を報せる", func(t *testing.T) {
		t.Parallel()
		got := []any{want[0], want[1], map[string]any{"type": "deletion"}}
		assert.Equal(t, []string{"ルール deletion が実態に2件以上あります"}, diffRules(want, got))
	})

	t.Run("required status check の過不足を文に添える", func(t *testing.T) {
		t.Parallel()
		check := func(contexts ...string) any {
			list := make([]any, 0, len(contexts))
			for _, c := range contexts {
				list = append(list, map[string]any{"context": c})
			}
			return map[string]any{"type": "required_status_checks", "parameters": map[string]any{"required_status_checks": list}}
		}
		assert.Equal(t, []string{"ルール required_status_checks が宣言と異なります（実態に無い context: b）"},
			diffRules([]any{check("a", "b")}, []any{check("a")}))
	})
}

func Test_contextDelta(t *testing.T) {
	t.Parallel()

	rule := func(contexts ...string) any {
		checks := make([]any, 0, len(contexts))
		for _, c := range contexts {
			checks = append(checks, map[string]any{"context": c})
		}
		return map[string]any{"parameters": map[string]any{"required_status_checks": checks}}
	}

	cases := []struct {
		name      string
		want, got any
		expected  string
	}{
		{"不足と余分を並べて報せる", rule("c", "a", "b"), rule("b", "z"), "（実態に無い context: a, c。宣言に無い context: z）"},
		{"不足だけなら不足だけを報せる", rule("a", "b"), rule("b"), "（実態に無い context: a）"},
		{"余分だけなら余分だけを報せる", rule("a"), rule("a", "z"), "（宣言に無い context: z）"},
		{"過不足が無ければ空", rule("a"), rule("a"), ""},
		{"required_status_checks でないルールは空", map[string]any{"type": "deletion"}, map[string]any{"type": "deletion"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, contextDelta(tc.want, tc.got))
		})
	}
}

func Test_statusContexts(t *testing.T) {
	t.Parallel()

	t.Run("context を持つ要素だけを集める", func(t *testing.T) {
		t.Parallel()
		rule := decodeAny(t, `{"parameters": {"required_status_checks": [{"context": "a"}, {"integration_id": 1}, {"context": "b"}]}}`)
		assert.Equal(t, map[string]bool{"a": true, "b": true}, statusContexts(rule))
	})

	t.Run("ルールの形でなければ空", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, statusContexts("x"))
	})
}

func Test_rulesByType(t *testing.T) {
	t.Parallel()

	t.Run("type から引ける形にする", func(t *testing.T) {
		t.Parallel()
		got, dups := rulesByType([]any{map[string]any{"type": "a", "v": 1.0}, map[string]any{"type": "b"}})
		assert.Equal(t, map[string]any{
			"a": map[string]any{"type": "a", "v": 1.0},
			"b": map[string]any{"type": "b"},
		}, got)
		assert.Empty(t, dups)
	})

	t.Run("2件以上在った type を返す", func(t *testing.T) {
		t.Parallel()
		_, dups := rulesByType([]any{map[string]any{"type": "a"}, map[string]any{"type": "a"}, map[string]any{"type": "b"}})
		assert.Equal(t, []string{"a"}, dups)
	})

	t.Run("配列でなければ空を返す", func(t *testing.T) {
		t.Parallel()
		got, dups := rulesByType(nil)
		assert.Empty(t, got)
		assert.Empty(t, dups)
	})
}

func Test_sortedKeys(t *testing.T) {
	t.Parallel()

	t.Run("キーを昇順で返す", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"a", "b", "c"}, sortedKeys(map[string]any{"c": 1, "a": 2, "b": 3}))
	})

	t.Run("nil なら空", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, sortedKeys(nil))
	})
}

func Test_subsetMatch(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		want string
		got  string
		ok   bool
	}{
		{"object は want のキーだけを見る", `{"a": 1}`, `{"a": 1, "b": 2}`, true},
		{"want のキーが無ければ不一致", `{"a": 1}`, `{"b": 1}`, false},
		{"object と他の型は不一致", `{"a": 1}`, `[1]`, false},
		{"配列は順序を問わない", `[1, 2]`, `[2, 1]`, true},
		{"配列の長さが違えば不一致", `[1]`, `[1, 1]`, false},
		{"配列の要素は1対1で当てる", `[1, 1]`, `[1, 2]`, false},
		{"配列と他の型は不一致", `[1]`, `{"a": 1}`, false},
		{"配列の中の object も want のキーだけを見る", `[{"c": "x"}]`, `[{"c": "x", "id": 3}]`, true},
		{"部分一致が重なっても割り当てがあれば一致", `[{"x": 1}, {"x": 1, "y": 2}]`, `[{"x": 1, "y": 2}, {"x": 1}]`, true},
		{"スカラーは等しさで比べる", `"x"`, `"x"`, true},
		{"スカラーの違いは不一致", `true`, `false`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var want, got any
			require.NoError(t, json.Unmarshal([]byte(tc.want), &want))
			require.NoError(t, json.Unmarshal([]byte(tc.got), &got))
			assert.Equal(t, tc.ok, subsetMatch(want, got))
		})
	}
}

func Test_assignAll(t *testing.T) {
	t.Parallel()

	one, two := map[string]any{"x": 1.0}, map[string]any{"x": 1.0, "y": 2.0}

	t.Run("先に当たった要素を取り直して割り当てを見つける", func(t *testing.T) {
		t.Parallel()
		assert.True(t, assignAll([]any{one, two}, []any{two, one}, make([]bool, 2)))
	})

	t.Run("割り当てが無ければ偽", func(t *testing.T) {
		t.Parallel()
		assert.False(t, assignAll([]any{two, two}, []any{two, one}, make([]bool, 2)))
	})

	t.Run("want が空なら真", func(t *testing.T) {
		t.Parallel()
		assert.True(t, assignAll(nil, []any{one}, make([]bool, 1)))
	})
}

func Test_diffEnv(t *testing.T) {
	t.Parallel()

	custom := desiredEnv{Name: "e", Patterns: []string{"main", "release/*"}}
	anyEnv := desiredEnv{Name: "e"}
	okEnv := liveEnv{CanAdminsBypass: boolPtr(false), Policy: &customOnly, Patterns: map[string]int64{"main": 1, "release/*": 2}}
	protected := branchPolicy{ProtectedBranches: true}

	t.Run("一致すれば差分が無い", func(t *testing.T) {
		t.Parallel()
		ed := diffEnv(custom, map[string]liveEnv{"e": okEnv})
		assert.Empty(t, ed.Findings)
		assert.False(t, ed.Put)
	})

	t.Run("実在しなければ作り、パターンを足す", func(t *testing.T) {
		t.Parallel()
		ed := diffEnv(custom, map[string]liveEnv{})
		assert.True(t, ed.Put)
		assert.Equal(t, []string{"main", "release/*"}, ed.AddPatterns)
		assert.Equal(t, []string{"実在しません"}, ed.Findings)
	})

	t.Run("管理者の迂回が有効なら直す", func(t *testing.T) {
		t.Parallel()
		got := okEnv
		got.CanAdminsBypass = boolPtr(true)
		ed := diffEnv(custom, map[string]liveEnv{"e": got})
		assert.True(t, ed.Put)
		assert.Equal(t, []string{"管理者による保護の迂回が有効です"}, ed.Findings)
	})

	t.Run("can_admins_bypass が応答に無ければずれとする", func(t *testing.T) {
		t.Parallel()
		got := okEnv
		got.CanAdminsBypass = nil
		ed := diffEnv(custom, map[string]liveEnv{"e": got})
		assert.True(t, ed.Put)
		assert.Equal(t, []string{"can_admins_bypass が応答にありません"}, ed.Findings)
	})

	t.Run("承認者だけでもずれとする", func(t *testing.T) {
		t.Parallel()
		got := okEnv
		got.Reviewers = 1
		ed := diffEnv(custom, map[string]liveEnv{"e": got})
		assert.True(t, ed.Put)
		assert.Equal(t, []string{"承認者が設定されています"}, ed.Findings)
	})

	t.Run("待機時間だけでもずれとする", func(t *testing.T) {
		t.Parallel()
		got := okEnv
		got.WaitTimer = 5
		ed := diffEnv(custom, map[string]liveEnv{"e": got})
		assert.True(t, ed.Put)
		assert.Equal(t, []string{"待機時間が設定されています"}, ed.Findings)
	})

	t.Run("protection_rules が応答に無ければずれとする", func(t *testing.T) {
		t.Parallel()
		got := okEnv
		got.RulesMissing = true
		ed := diffEnv(custom, map[string]liveEnv{"e": got})
		assert.True(t, ed.Put)
		assert.Equal(t, []string{"protection_rules が応答にありません"}, ed.Findings)
	})

	t.Run("custom の宣言に policy の無い実態はずれとする", func(t *testing.T) {
		t.Parallel()
		ed := diffEnv(custom, map[string]liveEnv{"e": {CanAdminsBypass: boolPtr(false)}})
		assert.True(t, ed.Put)
		assert.Equal(t, []string{"main", "release/*"}, ed.AddPatterns)
		assert.Equal(t, []string{
			"deployment branch policy が宣言のパターンだけを許す形ではありません",
			"パターン main がありません",
			"パターン release/* がありません",
		}, ed.Findings)
	})

	t.Run("any に policy があればずれとする", func(t *testing.T) {
		t.Parallel()
		ed := diffEnv(anyEnv, map[string]liveEnv{"e": {CanAdminsBypass: boolPtr(false), Policy: &protected}})
		assert.True(t, ed.Put)
		assert.Equal(t, []string{"宣言に無い deployment branch policy があります"}, ed.Findings)
	})

	t.Run("any に policy が無ければ一致", func(t *testing.T) {
		t.Parallel()
		ed := diffEnv(anyEnv, map[string]liveEnv{"e": {CanAdminsBypass: boolPtr(false)}})
		assert.Empty(t, ed.Findings)
	})

	t.Run("保護ブランチの policy は custom に直す", func(t *testing.T) {
		t.Parallel()
		got := okEnv
		got.Policy = &protected
		ed := diffEnv(custom, map[string]liveEnv{"e": got})
		assert.True(t, ed.Put)
		assert.Equal(t, []string{"deployment branch policy が宣言のパターンだけを許す形ではありません"}, ed.Findings)
	})

	t.Run("パターンの過不足を足し引きする", func(t *testing.T) {
		t.Parallel()
		got := okEnv
		got.Patterns = map[string]int64{"main": 1, "feature/*": 9, "tag:v*": 10}
		ed := diffEnv(custom, map[string]liveEnv{"e": got})
		assert.False(t, ed.Put)
		assert.Equal(t, []string{"release/*"}, ed.AddPatterns)
		assert.Equal(t, []int64{9, 10}, ed.RemovePatterns)
		assert.Equal(t, []string{
			"パターン release/* がありません",
			"宣言に無いパターン feature/* があります",
			"宣言に無いパターン tag:v* があります",
		}, ed.Findings)
	})
}

func Test_planRequests(t *testing.T) {
	t.Parallel()

	want := desired{
		Ruleset: map[string]any{"name": "branch-protection"},
		Envs:    []desiredEnv{{Name: "a plan", Patterns: []string{"main"}}, {Name: "copilot"}},
	}

	t.Run("差分が無ければ要求を出さない", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, planRequests(diff{}, want, "o/r", onlyRuleset))
		assert.Empty(t, planRequests(diff{}, want, "o/r", onlyEnvironments))
	})

	t.Run("ruleset が無ければ POST で作る", func(t *testing.T) {
		t.Parallel()
		reqs := planRequests(diff{Ruleset: rulesetDiff{Missing: true, Findings: []string{"x"}}}, want, "o/r", onlyRuleset)
		assert.Equal(t, []request{{Method: "POST", Path: "/repos/o/r/rulesets", Body: want.Ruleset}}, reqs)
	})

	t.Run("ruleset がずれていれば PUT で置き換える", func(t *testing.T) {
		t.Parallel()
		reqs := planRequests(diff{Ruleset: rulesetDiff{ID: 3, Findings: []string{"x"}}}, want, "o/r", onlyRuleset)
		assert.Equal(t, []request{{Method: "PUT", Path: "/repos/o/r/rulesets/3", Body: want.Ruleset}}, reqs)
	})

	t.Run("ruleset だけを対象にすると environment の差分を送らない", func(t *testing.T) {
		t.Parallel()
		d := diff{Envs: []envDiff{{Name: "copilot", Put: true, Findings: []string{"x"}}}}
		assert.Empty(t, planRequests(d, want, "o/r", onlyRuleset))
	})

	t.Run("environment を PUT し、パターンを消してから足す", func(t *testing.T) {
		t.Parallel()
		d := diff{
			Ruleset: rulesetDiff{Findings: []string{"ruleset は対象外"}},
			Envs: []envDiff{
				{Name: "a plan", Put: true, AddPatterns: []string{"main"}, RemovePatterns: []int64{4}, Findings: []string{"x"}},
				{Name: "copilot", Put: true, Findings: []string{"x"}},
			},
		}
		reqs := planRequests(d, want, "o/r", onlyEnvironments)
		require.Len(t, reqs, 4)
		assert.Equal(t, request{Method: "PUT", Path: "/repos/o/r/environments/a%20plan", Body: map[string]any{
			"wait_timer":               0,
			"prevent_self_review":      false,
			"reviewers":                nil,
			"deployment_branch_policy": &customOnly,
			"can_admins_bypass":        false,
		}}, reqs[0])
		assert.Equal(t, request{Method: "DELETE", Path: "/repos/o/r/environments/a%20plan/deployment-branch-policies/4"}, reqs[1])
		assert.Equal(t, request{Method: "POST", Path: "/repos/o/r/environments/a%20plan/deployment-branch-policies",
			Body: map[string]any{"name": "main", "type": "branch"}}, reqs[2])
		assert.Equal(t, "PUT", reqs[3].Method)
		assert.Equal(t, "/repos/o/r/environments/copilot", reqs[3].Path)
		assert.Nil(t, reqs[3].Body.(map[string]any)["deployment_branch_policy"])
	})

	t.Run("PUT が要らなければパターンだけを直す", func(t *testing.T) {
		t.Parallel()
		d := diff{Envs: []envDiff{{Name: "a plan", AddPatterns: []string{"main"}, Findings: []string{"x"}}}}
		reqs := planRequests(d, want, "o/r", onlyEnvironments)
		require.Len(t, reqs, 1)
		assert.Equal(t, "POST", reqs[0].Method)
	})
}
