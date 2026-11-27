package main

import (
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	oidcPrefix = "module.deployment_identity.module.github_oidc."
	planPath   = "tmp/ci-permissions/plan.json"
	// planPermTotal は、前提の policy がすべて付いた plan の確認の件数。表と独立の定数から数える。
	planPermTotal = permissionsBase + governanceGated + delegatedGated + workloadGatedCount + budgetsGatedCount
)

// testDoc は、name を Sid に持つ文書。role ごとに区別できる。
func testDoc(name string) string {
	return `{"Version":"2012-10-17","Statement":[{"Sid":"` + strings.NewReplacer("-", "", "_", "").Replace(name) + `","Effect":"Allow","Action":"s3:GetObject","Resource":"*"}]}`
}

// planRC は、identity root の plan の resource_changes の1件。after は JSON の値そのもの（null なら削除）。
func planRC(address, typ, actions, after, unknown string) map[string]any {
	var a, u any
	if err := json.Unmarshal([]byte(after), &a); err != nil {
		panic(err)
	}

	if err := json.Unmarshal([]byte(unknown), &u); err != nil {
		panic(err)
	}

	before := a
	if strings.Contains(actions, "create") {
		before = nil
	}

	return map[string]any{"address": oidcPrefix + address, "mode": "managed", "type": typ,
		"change": map[string]any{"actions": json.RawMessage(actions), "before": before, "after": a, "after_unknown": u}}
}

func jsonOf(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}

	return string(b)
}

// identityPlanChanges は、env/admin/identity の plan のうち、CI の role の policy を決める resource の形
// （usecases/deployment-identity/internal/github-oidc の main.tf・policies.tf）。変更の無い plan なので、どれも no-op で
// after を持つ。
func identityPlanChanges() []map[string]any {
	keys := []string{"plan", "apply", "apply_identity", "unlock_identity", "unlock"}
	policy := func(name string) string { return "arn:aws:iam::" + testAccount + ":policy/" + name }
	managed := map[string][]string{
		"apply_identity": {policy("tabp-admin-ci-apply-identity-governance"), policy("tabp-admin-ci-apply-identity-governance-2"),
			policy("tabp-admin-ci-apply-identity-manage"), policy("tabp-admin-ci-apply-identity-members")},
		"apply": {policy("tabp-admin-ci-apply-workload"), policy("tabp-admin-ci-apply-workload-2"), policy("tabp-admin-ci-apply-workload-read")},
		"plan":  {policy("tabp-admin-ci-apply-workload-read")},
	}

	var out []map[string]any

	for _, k := range keys {
		role := "tabp-admin-ci-" + strings.ReplaceAll(k, "_", "-")
		inline := `aws_iam_role_policy.this["` + k + `"]`

		if k == "apply_identity" {
			inline = "aws_iam_role_policy.apply_identity"
		}

		arns := managed[k]
		if arns == nil {
			arns = []string{}
		}

		out = append(out,
			planRC(inline, typeRolePolicy, `["no-op"]`, jsonOf(map[string]any{"name": k, "role": role, "policy": testDoc(k)}), `{}`),
			planRC(`aws_iam_role_policies_exclusive.this["`+k+`"]`, typeRolePolicies, `["no-op"]`, jsonOf(map[string]any{"role_name": role, "policy_names": []string{k}}), `{}`),
			planRC(`aws_iam_role_policy_attachments_exclusive.this["`+k+`"]`, typeRoleAttachments, `["no-op"]`, jsonOf(map[string]any{"role_name": role, "policy_arns": arns}), `{}`),
		)
	}

	for address, name := range map[string]string{
		"aws_iam_policy.manage": "tabp-admin-ci-apply-identity-manage", "aws_iam_policy.governance[0]": "tabp-admin-ci-apply-identity-governance",
		"aws_iam_policy.governance_2[0]": "tabp-admin-ci-apply-identity-governance-2", "aws_iam_policy.members[0]": "tabp-admin-ci-apply-identity-members",
		"aws_iam_policy.workload": "tabp-admin-ci-apply-workload", "aws_iam_policy.workload_2": "tabp-admin-ci-apply-workload-2",
		"aws_iam_policy.workload_read": "tabp-admin-ci-apply-workload-read", `aws_iam_policy.boundary["ops-notification"]`: "tabp-admin-ci-boundary-ops-notification",
	} {
		out = append(out, planRC(address, typeManagedPolicy, `["no-op"]`, jsonOf(map[string]any{"name": name, "path": "/", "arn": policy(name), "policy": testDoc(name)}), `{}`))
	}

	return append(out, map[string]any{"address": "data.aws_region.current", "mode": "data", "type": "aws_region", "change": map[string]any{"actions": []string{"read"}}})
}

// withChange は、address の resource_changes を f で書き換えた写し。f が nil なら取り除く。
func withChange(changes []map[string]any, address string, f func(map[string]any) map[string]any) []map[string]any {
	var out []map[string]any

	for _, c := range changes {
		if c["address"] != oidcPrefix+address {
			out = append(out, c)

			continue
		}

		if f != nil {
			out = append(out, f(c))
		}
	}

	return out
}

// setAfter は、resource_changes の1件の after と after_unknown を差し替える。
func setAfter(after, unknown string) func(map[string]any) map[string]any {
	return func(c map[string]any) map[string]any {
		old := c["change"].(map[string]any)
		n := planRC(strings.TrimPrefix(c["address"].(string), oidcPrefix), c["type"].(string), string(old["actions"].(json.RawMessage)), after, unknown)

		if after == "null" {
			n["change"].(map[string]any)["before"] = old["after"]
		}

		return n
	}
}

func planOf(changes []map[string]any) string {
	return jsonOf(map[string]any{"format_version": "1.2", "resource_changes": changes})
}

func mustChanges(t *testing.T, changes []map[string]any) []planChange {
	t.Helper()

	got, err := readPlanChanges([]byte(planOf(changes)))
	require.NoError(t, err)

	return got
}

func Test_parsePlanPermArgs(t *testing.T) {
	t.Parallel()

	t.Run("plan・project・region を受ける", func(t *testing.T) {
		t.Parallel()

		got, err := parsePlanPermArgs([]string{"-plan", "p.json", "-project", "tabp", "-region", "ap-northeast-1"})

		require.NoError(t, err)
		assert.Equal(t, planPermOptions{plan: "p.json", project: "tabp", region: "ap-northeast-1"}, got)
	})

	for name, args := range map[string][]string{
		"plan が無い":    {"-project", "tabp", "-region", "r"},
		"project が無い": {"-plan", "p", "-region", "r"},
		"region が無い":  {"-plan", "p", "-project", "tabp"},
		"余分な引数":       {"-plan", "p", "-project", "tabp", "-region", "r", "x"},
		"知らないフラグ":     {"-plan", "p", "-project", "tabp", "-region", "r", "-stage", "s"},
	} {
		t.Run(name+"なら errUsage", func(t *testing.T) {
			t.Parallel()

			_, err := parsePlanPermArgs(args)

			require.ErrorIs(t, err, errUsage)
		})
	}
}

func Test_readPlanChanges(t *testing.T) {
	t.Parallel()

	t.Run("resource_changes の各要素の address・type・after・before・after_unknown を読む", func(t *testing.T) {
		t.Parallel()

		got, err := readPlanChanges([]byte(`{"resource_changes":[{"address":"a.b","type":"t","change":{"actions":["update"],"before":{"x":1},"after":{"x":2},"after_unknown":{"y":true}}},` +
			`{"address":"c.d","type":"t","change":{"actions":["delete"],"before":{"x":1},"after":null}}]}`))

		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, planChange{address: "a.b", typ: "t", after: map[string]any{"x": 2.0}, before: map[string]any{"x": 1.0}, unknown: map[string]any{"y": true}}, got[0])
		assert.Nil(t, got[1].after)
		assert.Equal(t, map[string]any{"x": 1.0}, got[1].before)
	})

	for name, tc := range map[string]struct {
		body string
		want error
	}{
		"JSON でない":                {"x", errPlanPermJSON},
		"null":                    {"null", errPlanPermJSON},
		"object でない":              {"[]", errPlanPermJSON},
		"resource_changes が無い":    {`{"format_version":"1.2"}`, errPlanPermEmpty},
		"resource_changes が null": {`{"resource_changes":null}`, errPlanPermEmpty},
		"resource_changes が空":     {`{"resource_changes":[]}`, errPlanPermEmpty},
		"resource_changes が配列でない": {`{"resource_changes":{}}`, errPlanPermJSON},
		"要素が object でない":          {`{"resource_changes":["x"]}`, errPlanPermJSON},
		"要素に address が無い":         {`{"resource_changes":[{"type":"t"}]}`, errPlanPermJSON},
		"要素の address が空":          {`{"resource_changes":[{"address":"","type":"t"}]}`, errPlanPermJSON},
		"要素に type が無い":            {`{"resource_changes":[{"address":"a.b"}]}`, errPlanPermJSON},
		"要素の address が文字列でない":     {`{"resource_changes":[{"address":1,"type":"t"}]}`, errPlanPermJSON},
	} {
		t.Run(name+"なら失敗する", func(t *testing.T) {
			t.Parallel()

			_, err := readPlanChanges([]byte(tc.body))

			require.ErrorIs(t, err, tc.want)
		})
	}
}

func Test_planChange_unknownAt(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		unknown any
		want    bool
	}{
		"after_unknown が true なら、どの key も決まらない": {true, true},
		"key が true":        {map[string]any{"policy": true}, true},
		"一覧の要素のどれかが true":   {map[string]any{"policy": []any{false, true}}, true},
		"key が false":       {map[string]any{"policy": false}, false},
		"別の key だけが true":   {map[string]any{"id": true}, false},
		"after_unknown が無い": {nil, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, planChange{unknown: tc.unknown}.unknownAt("policy"))
		})
	}
}

func Test_anyTrue(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		v    any
		want bool
	}{
		"true":             {true, true},
		"false":            {false, false},
		"配列の要素":            {[]any{false, []any{true}}, true},
		"object の値":        {map[string]any{"a": map[string]any{"b": true}}, true},
		"true を持たない入れ子":    {map[string]any{"a": []any{false}}, false},
		"文字列の true は真としない": {"true", false},
		"nil":              {nil, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, anyTrue(tc.v))
		})
	}
}

func Test_planChange_owner(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		c    planChange
		want string
	}{
		"after の値":               {planChange{after: map[string]any{"role": "r"}}, "r"},
		"削除なら before の値":         {planChange{before: map[string]any{"role": "r"}}, "r"},
		"after で決まらなければ空":        {planChange{after: map[string]any{}, unknown: map[string]any{"role": true}}, ""},
		"文字列でなければ空":              {planChange{after: map[string]any{"role": 1.0}}, ""},
		"after と before のどちらも無い": {planChange{}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, tc.c.owner("role"))
		})
	}
}

func Test_planChange_stringsOf(t *testing.T) {
	t.Parallel()

	t.Run("文字列の配列を返す", func(t *testing.T) {
		t.Parallel()

		got, ok := planChange{after: map[string]any{"a": []any{"x", "y"}}}.stringsOf("a")

		require.True(t, ok)
		assert.Equal(t, []string{"x", "y"}, got)
	})

	for name, c := range map[string]planChange{
		"削除":          {before: map[string]any{"a": []any{"x"}}},
		"plan で決まらない": {after: map[string]any{"a": []any{"x", nil}}, unknown: map[string]any{"a": []any{false, true}}},
		"配列でない":       {after: map[string]any{"a": "x"}},
		"文字列でない要素":    {after: map[string]any{"a": []any{"x", 1.0}}},
		"key が無い":     {after: map[string]any{}},
	} {
		t.Run(name+"なら false", func(t *testing.T) {
			t.Parallel()

			_, ok := c.stringsOf("a")

			assert.False(t, ok)
		})
	}
}

func Test_planDocument(t *testing.T) {
	t.Parallel()

	t.Run("after の policy の文書を返す", func(t *testing.T) {
		t.Parallel()

		doc, reason := planDocument(planChange{address: "a.b", after: map[string]any{"policy": testDoc("x")}})

		assert.Equal(t, testDoc("x"), doc)
		assert.Empty(t, reason)
	})

	for name, tc := range map[string]struct {
		c    planChange
		want string
	}{
		"削除":          {planChange{address: "a.b", before: map[string]any{"policy": testDoc("x")}}, "a.b が削除される"},
		"plan で決まらない": {planChange{address: "a.b", after: map[string]any{}, unknown: map[string]any{"policy": true}}, "a.b の文書が plan で決まらない"},
		"文書が無い":       {planChange{address: "a.b", after: map[string]any{}}, "a.b に文書が無い"},
		"文書が文字列でない":   {planChange{address: "a.b", after: map[string]any{"policy": map[string]any{}}}, "a.b に文書が無い"},
		"JSON でない":    {planChange{address: "a.b", after: map[string]any{"policy": "x"}}, "a.b の文書を JSON の object として読めない"},
		"JSON の配列":    {planChange{address: "a.b", after: map[string]any{"policy": "[]"}}, "a.b の文書を JSON の object として読めない"},
	} {
		t.Run(name+"なら address を名指す理由", func(t *testing.T) {
			t.Parallel()

			doc, reason := planDocument(tc.c)

			assert.Empty(t, doc)
			assert.Equal(t, tc.want, reason)
		})
	}
}

func Test_policyNameOf(t *testing.T) {
	t.Parallel()

	for arn, want := range map[string]string{
		"arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-workload":  "tabp-admin-ci-apply-workload",
		"arn:aws:iam::123456789012:policy/path/to/tabp-admin-ci-apply-x": "tabp-admin-ci-apply-x",
		"arn:aws:iam::123456789012:role/tabp-admin-ci-plan":              "",
		"": "",
	} {
		t.Run(arn, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, want, policyNameOf(arn))
		})
	}
}

func Test_only(t *testing.T) {
	t.Parallel()

	t.Run("型と key の値が合うものだけを、並びを保って返す", func(t *testing.T) {
		t.Parallel()

		changes := []planChange{
			{address: "a", typ: "t", after: map[string]any{"role": "r"}},
			{address: "b", typ: "u", after: map[string]any{"role": "r"}},
			{address: "c", typ: "t", after: map[string]any{"role": "s"}},
			{address: "d", typ: "t", before: map[string]any{"role": "r"}},
		}

		var got []string
		for _, c := range only(changes, "t", "role", "r") {
			got = append(got, c.address)
		}

		assert.Equal(t, []string{"a", "d"}, got)
	})
}

func Test_planDocuments(t *testing.T) {
	t.Parallel()

	n := testNames("")

	t.Run("変更の無い plan の no-op の resource の after から、5つの role の inline と managed の文書を導く", func(t *testing.T) {
		t.Parallel()

		got := planDocuments(mustChanges(t, identityPlanChanges()), n)

		assert.Equal(t, map[string]rolePolicies{
			"plan": {docs: []string{testDoc("plan"), testDoc("tabp-admin-ci-apply-workload-read")}, managed: []string{"tabp-admin-ci-apply-workload-read"}},
			"apply": {docs: []string{testDoc("apply"), testDoc("tabp-admin-ci-apply-workload"), testDoc("tabp-admin-ci-apply-workload-2"), testDoc("tabp-admin-ci-apply-workload-read")},
				managed: []string{"tabp-admin-ci-apply-workload", "tabp-admin-ci-apply-workload-2", "tabp-admin-ci-apply-workload-read"}},
			"apply-identity": {docs: []string{testDoc("apply_identity"), testDoc("tabp-admin-ci-apply-identity-governance"), testDoc("tabp-admin-ci-apply-identity-governance-2"),
				testDoc("tabp-admin-ci-apply-identity-manage"), testDoc("tabp-admin-ci-apply-identity-members")},
				managed: []string{"tabp-admin-ci-apply-identity-governance", "tabp-admin-ci-apply-identity-governance-2", "tabp-admin-ci-apply-identity-manage", "tabp-admin-ci-apply-identity-members"}},
			"unlock-identity": {docs: []string{testDoc("unlock_identity")}},
			"unlock":          {docs: []string{testDoc("unlock")}},
		}, got)
	})

	t.Run("no-op の resource が after を持たなければ、その role は文書を決められない", func(t *testing.T) {
		t.Parallel()

		changes := withChange(identityPlanChanges(), `aws_iam_role_policy.this["plan"]`, func(c map[string]any) map[string]any {
			c["change"].(map[string]any)["after"] = nil

			return c
		})

		got := planDocuments(mustChanges(t, changes), n)

		assert.Equal(t, `module.deployment_identity.module.github_oidc.aws_iam_role_policy.this["plan"] が削除される`, got["plan"].unresolved)
		assert.Empty(t, got["unlock"].unresolved)
	})
}

func Test_roleDocuments(t *testing.T) {
	t.Parallel()

	const role = "tabp-admin-ci-plan"

	for name, tc := range map[string]struct {
		changes []map[string]any
		want    string
	}{
		"role の resource が plan に無い": {withChange(withChange(withChange(identityPlanChanges(),
			`aws_iam_role_policy.this["plan"]`, nil), `aws_iam_role_policies_exclusive.this["plan"]`, nil), `aws_iam_role_policy_attachments_exclusive.this["plan"]`, nil),
			"plan に tabp-admin-ci-plan の aws_iam_role_policies_exclusive が 0 個、aws_iam_role_policy_attachments_exclusive が 0 個（1個ずつを期待）"},
		"aws_iam_role_policies_exclusive が2つ": {append(identityPlanChanges(),
			planRC(`aws_iam_role_policies_exclusive.x`, typeRolePolicies, `["no-op"]`, `{"role_name":"tabp-admin-ci-plan","policy_names":["plan"]}`, `{}`)),
			"plan に tabp-admin-ci-plan の aws_iam_role_policies_exclusive が 2 個、aws_iam_role_policy_attachments_exclusive が 1 個（1個ずつを期待）"},
		"aws_iam_role_policies_exclusive が削除される": {withChange(identityPlanChanges(), `aws_iam_role_policies_exclusive.this["plan"]`, setAfter("null", "null")),
			oidcPrefix + `aws_iam_role_policies_exclusive.this["plan"] が削除される`},
		"policy_names が plan で決まらない": {withChange(identityPlanChanges(), `aws_iam_role_policies_exclusive.this["plan"]`, setAfter(`{"role_name":"tabp-admin-ci-plan"}`, `{"policy_names":true}`)),
			oidcPrefix + `aws_iam_role_policies_exclusive.this["plan"] の policy_names が plan で決まらない`},
		"inline の文書が plan で決まらない（作り直しの plan の形）": {withChange(identityPlanChanges(), `aws_iam_role_policy.this["plan"]`, setAfter(`{"name":"plan","role":"tabp-admin-ci-plan"}`, `{"id":true,"policy":true}`)),
			oidcPrefix + `aws_iam_role_policy.this["plan"] の文書が plan で決まらない`},
		"inline の文書が JSON でない": {withChange(identityPlanChanges(), `aws_iam_role_policy.this["plan"]`, setAfter(`{"name":"plan","role":"tabp-admin-ci-plan","policy":"x"}`, `{}`)),
			oidcPrefix + `aws_iam_role_policy.this["plan"] の文書を JSON の object として読めない`},
		"inline が policy_names より少ない": {withChange(identityPlanChanges(), `aws_iam_role_policy.this["plan"]`, nil),
			`inline policy の文書が 0 個で、` + oidcPrefix + `aws_iam_role_policies_exclusive.this["plan"] の policy_names の 1 個と合わない`},
		"inline が policy_names より多い": {append(identityPlanChanges(),
			planRC(`aws_iam_role_policy.extra`, typeRolePolicy, `["create"]`, jsonOf(map[string]any{"name": "extra", "role": role, "policy": testDoc("extra")}), `{"id":true}`)),
			`inline policy の文書が 2 個で、` + oidcPrefix + `aws_iam_role_policies_exclusive.this["plan"] の policy_names の 1 個と合わない`},
		"inline の role が plan で決まらなければ、数の突き合わせで落ちる": {withChange(identityPlanChanges(), `aws_iam_role_policy.this["plan"]`, setAfter(`{"name":"plan","policy":"{}"}`, `{"role":true}`)),
			`inline policy の文書が 0 個で、` + oidcPrefix + `aws_iam_role_policies_exclusive.this["plan"] の policy_names の 1 個と合わない`},
		"policy_arns が plan で決まらない": {withChange(identityPlanChanges(), `aws_iam_role_policy_attachments_exclusive.this["plan"]`, setAfter(`{"role_name":"tabp-admin-ci-plan","policy_arns":[null]}`, `{"policy_arns":[true]}`)),
			oidcPrefix + `aws_iam_role_policy_attachments_exclusive.this["plan"] の policy_arns が plan で決まらない`},
		"attachment が削除される": {withChange(identityPlanChanges(), `aws_iam_role_policy_attachments_exclusive.this["plan"]`, setAfter("null", "null")),
			oidcPrefix + `aws_iam_role_policy_attachments_exclusive.this["plan"] が削除される`},
		"policy_arns の policy が plan に無い": {withChange(identityPlanChanges(), `aws_iam_policy.workload_read`, nil),
			`managed policy の文書が 0 個で、` + oidcPrefix + `aws_iam_role_policy_attachments_exclusive.this["plan"] の policy_arns の 1 個と合わない`},
		"policy_arns が policy の ARN でない": {withChange(identityPlanChanges(), `aws_iam_role_policy_attachments_exclusive.this["plan"]`, setAfter(`{"role_name":"tabp-admin-ci-plan","policy_arns":["arn:aws:iam::123456789012:role/x"]}`, `{}`)),
			`managed policy の文書が 0 個で、` + oidcPrefix + `aws_iam_role_policy_attachments_exclusive.this["plan"] の policy_arns の 1 個と合わない`},
		"同じ ARN の aws_iam_policy が2つ（ARN の無い方は name と path から組む）": {append(identityPlanChanges(),
			planRC(`aws_iam_policy.dup`, typeManagedPolicy, `["no-op"]`, jsonOf(map[string]any{"name": "tabp-admin-ci-apply-workload-read", "policy": testDoc("dup")}), `{}`)),
			`managed policy の文書が 0 個で、` + oidcPrefix + `aws_iam_role_policy_attachments_exclusive.this["plan"] の policy_arns の 1 個と合わない`},
		"managed の文書が plan で決まらない": {withChange(identityPlanChanges(), `aws_iam_policy.workload_read`, setAfter(`{"name":"tabp-admin-ci-apply-workload-read"}`, `{"policy":true}`)),
			oidcPrefix + `aws_iam_policy.workload_read の文書が plan で決まらない`},
		"managed の policy が削除される": {withChange(identityPlanChanges(), `aws_iam_policy.workload_read`, setAfter("null", "null")),
			oidcPrefix + `aws_iam_policy.workload_read が削除される`},
		"aws_iam_role_policy_attachments_exclusive だけが0個": {withChange(identityPlanChanges(), `aws_iam_role_policy_attachments_exclusive.this["plan"]`, nil),
			"plan に tabp-admin-ci-plan の aws_iam_role_policies_exclusive が 1 個、aws_iam_role_policy_attachments_exclusive が 0 個（1個ずつを期待）"},
		"aws_iam_role_policy_attachments_exclusive だけが2つ": {append(identityPlanChanges(),
			planRC(`aws_iam_role_policy_attachments_exclusive.x`, typeRoleAttachments, `["no-op"]`, `{"role_name":"tabp-admin-ci-plan","policy_arns":[]}`, `{}`)),
			"plan に tabp-admin-ci-plan の aws_iam_role_policies_exclusive が 1 個、aws_iam_role_policy_attachments_exclusive が 2 個（1個ずつを期待）"},
		"policy_arns に同じ ARN が2つ": {withChange(identityPlanChanges(), `aws_iam_role_policy_attachments_exclusive.this["plan"]`,
			setAfter(`{"role_name":"tabp-admin-ci-plan","policy_arns":["arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-workload-read","arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-workload-read"]}`, `{}`)),
			oidcPrefix + `aws_iam_role_policy_attachments_exclusive.this["plan"] の policy_arns に同じ policy が2つある`},
		"aws_iam_policy の name が plan で決まらず ARN も無い": {withChange(identityPlanChanges(), `aws_iam_policy.workload_read`, setAfter(`{"policy":"{}"}`, `{"name":true,"arn":true}`)),
			`managed policy の文書が 0 個で、` + oidcPrefix + `aws_iam_role_policy_attachments_exclusive.this["plan"] の policy_arns の 1 個と合わない`},
		"名前は同じでも ARN が違う（path が違う）": {withChange(identityPlanChanges(), `aws_iam_policy.workload_read`,
			setAfter(`{"name":"tabp-admin-ci-apply-workload-read","path":"/x/","arn":"arn:aws:iam::123456789012:policy/x/tabp-admin-ci-apply-workload-read","policy":"{}"}`, `{}`)),
			`managed policy の文書が 0 個で、` + oidcPrefix + `aws_iam_role_policy_attachments_exclusive.this["plan"] の policy_arns の 1 個と合わない`},
		"名前は同じでもアカウントが違う": {withChange(identityPlanChanges(), `aws_iam_policy.workload_read`,
			setAfter(`{"name":"tabp-admin-ci-apply-workload-read","arn":"arn:aws:iam::210987654321:policy/tabp-admin-ci-apply-workload-read","policy":"{}"}`, `{}`)),
			`managed policy の文書が 0 個で、` + oidcPrefix + `aws_iam_role_policy_attachments_exclusive.this["plan"] の policy_arns の 1 個と合わない`},
		"inline も managed も0個": {withChange(withChange(withChange(identityPlanChanges(),
			`aws_iam_role_policy.this["plan"]`, nil),
			`aws_iam_role_policies_exclusive.this["plan"]`, setAfter(`{"role_name":"tabp-admin-ci-plan","policy_names":[]}`, `{}`)),
			`aws_iam_role_policy_attachments_exclusive.this["plan"]`, setAfter(`{"role_name":"tabp-admin-ci-plan","policy_arns":[]}`, `{}`)),
			"plan に tabp-admin-ci-plan の文書が1つも無い"},
	} {
		t.Run(name+"なら、文書を決められない", func(t *testing.T) {
			t.Parallel()

			got := roleDocuments(mustChanges(t, tc.changes), testNames(""), role)

			assert.Equal(t, rolePolicies{unresolved: tc.want}, got)
			assert.NotContains(t, got.unresolved, "arn:", "理由は address を名指し、ARN を出さない")
		})
	}
}

func Test_managedPolicyARN(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		c    planChange
		want string
	}{
		"plan で決まった arn": {planChange{after: map[string]any{"arn": "arn:aws:iam::1:policy/p/x", "name": "x"}}, "arn:aws:iam::1:policy/p/x"},
		"arn が決まらなければ、account・path・name から組む": {planChange{after: map[string]any{"name": "x", "path": "/p/"}, unknown: map[string]any{"arn": true}},
			"arn:aws:iam::" + testAccount + ":policy/p/x"},
		"path が無ければ /":         {planChange{after: map[string]any{"name": "x"}}, "arn:aws:iam::" + testAccount + ":policy/x"},
		"削除なら before の値から組む":   {planChange{before: map[string]any{"name": "x"}}, "arn:aws:iam::" + testAccount + ":policy/x"},
		"name も arn も決まらなければ空": {planChange{after: map[string]any{}, unknown: map[string]any{"name": true, "arn": true}}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, managedPolicyARN(tc.c, testAccount))
		})
	}
}

func Test_planGates(t *testing.T) {
	t.Parallel()

	n := testNames("")
	resolved := planDocuments(mustChanges(t, identityPlanChanges()), n)

	gateNames := map[gate]string{gateNone: "条件の無い区分", gateGovernance: "統制", gateDelegatedAdmin: "委任管理者", gateWorkload: "workload"}

	for g, name := range gateNames {
		t.Run("plan の managed policy の名前で、"+name+"の区分の付き方を attached と決める", func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, gateAttached, planGates(n, resolved)[g].state)
		})
	}

	absent := map[string]rolePolicies{n.applyIdentity: {docs: []string{"{}"}, managed: []string{"tabp-admin-ci-apply-identity-manage"}}, n.apply: {docs: []string{"{}"}}}

	for _, g := range []gate{gateGovernance, gateDelegatedAdmin, gateWorkload} {
		t.Run(gateNames[g]+"の policy が付いていなければ absent", func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, gateAbsent, planGates(n, absent)[g].state)
		})
	}

	t.Run("role の文書を決められなければ unknown で、NG は統制と workload の側が持つ", func(t *testing.T) {
		t.Parallel()

		p := map[string]rolePolicies{n.applyIdentity: {unresolved: "a"}, n.apply: {unresolved: "b"}}
		got := planGates(n, p)

		assert.Equal(t, gateUnknown, got[gateGovernance].state)
		assert.Equal(t, gateUnknown, got[gateDelegatedAdmin].state)
		assert.Nil(t, got[gateDelegatedAdmin].ng.eval)
		assert.Equal(t, gateUnknown, got[gateWorkload].state)

		v, _ := got[gateGovernance].ng.eval(nil)
		assert.Equal(t, verdict{reason: "a"}, v)

		v, _ = got[gateWorkload].ng.eval(nil)
		assert.Equal(t, verdict{reason: "b"}, v)
	})
}

func Test_planPermissionGroups(t *testing.T) {
	t.Parallel()

	n := testNames("")
	resolved := planDocuments(mustChanges(t, identityPlanChanges()), n)

	t.Run("前提の policy がすべて付けば、permissions の表のすべての行を role の文書で評価する", func(t *testing.T) {
		t.Parallel()

		groups := planPermissionGroups(n, resolved, testKeyARN, "")

		var headings []string
		for _, s := range permissionSections(n, testKeyARN, "") {
			headings = append(headings, s.heading)
		}

		total := 0

		for i, g := range groups {
			assert.Equal(t, headings[i], g.heading)

			for _, it := range g.items {
				require.NotNil(t, it.row, it.check.desc)
				assert.Equal(t, resolved[it.row.role].docs, it.row.docs, it.check.desc)
				assert.Empty(t, it.row.unresolved, it.check.desc)

				total++
			}
		}

		assert.Len(t, groups, len(headings))
		assert.Equal(t, planPermTotal, total)
	})

	t.Run("前提の policy が plan で付いていなければ、注記で通さずに区分ごとに NG を1件置く", func(t *testing.T) {
		t.Parallel()

		p := map[string]rolePolicies{}
		for k, v := range resolved {
			p[k] = v
		}

		p[n.applyIdentity] = rolePolicies{docs: []string{"{}"}, managed: []string{"tabp-admin-ci-apply-identity-manage"}}

		groups := planPermissionGroups(n, p, testKeyARN, "")

		var fixed []string

		for _, g := range groups {
			for _, it := range g.items {
				if it.row == nil {
					v, _ := it.check.eval(nil)
					assert.False(t, v.ok)

					fixed = append(fixed, g.heading+" | "+it.check.desc)
				}
			}
		}

		assert.Equal(t, []string{
			"# 統制の構成物と apply-identity の policy（deployment-identity/ADR-0201 決定3・5・7・22） | この区分の前提の managed policy が plan で付いていない（条件つきの " + strconv.Itoa(governanceGated) + " 行を確かめない）",
			"# 委任管理者の指定（deployment-identity/ADR-0201 決定7・21、repository-wide/ADR-0209 決定4） | この区分の前提の managed policy が plan で付いていない（条件つきの " + strconv.Itoa(delegatedGated) + " 行を確かめない）",
		}, fixed)
	})

	t.Run("role の文書を決められなければ、その role の行は理由を持ち、統制の NG と委任管理者の NG を1件ずつ置く", func(t *testing.T) {
		t.Parallel()

		p := map[string]rolePolicies{}
		for k, v := range resolved {
			p[k] = v
		}

		p[n.applyIdentity] = rolePolicies{unresolved: "x の文書が plan で決まらない"}

		groups := planPermissionGroups(n, p, testKeyARN, "")

		var fixed []string

		for _, g := range groups {
			for _, it := range g.items {
				switch {
				case it.row == nil:
					fixed = append(fixed, it.check.desc)
				case it.row.role == n.applyIdentity:
					assert.Equal(t, "x の文書が plan で決まらない", it.row.unresolved)
					assert.Empty(t, it.row.docs)
				default:
					assert.Empty(t, it.row.unresolved)
				}
			}
		}

		assert.Equal(t, []string{
			"apply-identity: plan で managed policy を決められないので、統制の policy とメンバーの role の policy が付いているかを決められない",
			"この区分の前提の managed policy の付き方を決められない（上の区分の NG。条件つきの " + strconv.Itoa(delegatedGated) + " 行を確かめない）",
		}, fixed)
	})

	workloadHeadings := []string{"# workload の権限の policy と permissions boundary（deployment-identity/ADR-0201 決定5・13-17）", "# budgets と Cost Anomaly Detection（deployment-identity/ADR-0201 決定13）"}

	for name, apply := range map[string]rolePolicies{
		"absent":         {docs: []string{"{}"}},
		"unknown（名前が違う）": {docs: []string{"{}"}, managed: []string{"x"}},
		"unknown（role の文書を決められない）": {unresolved: "y"},
	} {
		t.Run("workload の gate が "+name+" なら、workload と budgets の区分は表の行と NG 1件だけを持つ", func(t *testing.T) {
			t.Parallel()

			p := map[string]rolePolicies{}
			for k, v := range resolved {
				p[k] = v
			}

			p[n.apply] = apply

			var got []int

			for _, g := range planPermissionGroups(n, p, testKeyARN, "") {
				if !slices.Contains(workloadHeadings, g.heading) {
					continue
				}

				fixed := 0

				for _, it := range g.items {
					if it.row == nil {
						v, _ := it.check.eval(nil)
						assert.False(t, v.ok, g.heading)

						fixed++
					}
				}

				got = append(got, fixed, len(g.items))
			}

			assert.Equal(t, []int{1, len(permWorkloadDescs) + 1, 1, len(permBudgetsDescs) + 1}, got)
		})
	}

	t.Run("state の鍵を読めなければ、鍵の区分の先頭にその NG を置き、鍵の行は理由を持つ", func(t *testing.T) {
		t.Parallel()

		groups := planPermissionGroups(n, resolved, "arn:unknown", "呼び出しが失敗: x")

		require.Nil(t, groups[1].items[0].row)
		assert.Equal(t, "state の鍵の ARN（alias/tabp-admin-tfstate）を読めない", groups[1].items[0].check.desc)

		keyRows := 0

		for _, g := range groups {
			for _, it := range g.items {
				if it.row != nil && it.row.resource == "arn:unknown" {
					assert.Equal(t, "state の鍵の ARN を読めない", it.row.unresolved)

					keyRows++
				}
			}
		}

		// 鍵の区分 6、unlock-identity と unlock の各 1。
		assert.Equal(t, 8, keyRows)
	})
}

func Test_planNGLine(t *testing.T) {
	t.Parallel()

	r := simCtx(allowed, "plan", "arn:aws:s3:::secret-bucket", []string{ctxEntry("s3:prefix", "secret-prefix", "string")}, "s3:A", "s3:B")

	t.Run("表の行は role・action の一覧・期待・理由だけを出し、resource と context を出さない", func(t *testing.T) {
		t.Parallel()

		got := planNGLine(planItem{check: simCheck(r, testNames("")), row: &r}, verdict{reason: "違う判定: s3:A=implicitDeny"})

		assert.Equal(t, "NG  plan: s3:A s3:B が allowed（違う判定: s3:A=implicitDeny）", got)
	})

	t.Run("理由が無ければ括弧を付けない", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, "NG  plan: s3:A s3:B が allowed", planNGLine(planItem{row: &r}, verdict{}))
	})

	t.Run("表の行でない確認は desc を出す", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, "NG  前提（r）", planNGLine(planItem{check: fixedCheck("前提", verdict{})}, verdict{reason: "r"}))
	})
}

func Test_runPlanChecks(t *testing.T) {
	t.Parallel()

	ok, ng := sim(allowed, "plan", "arn:x", "a"), sim(allowed, "apply", "arn:y", "b", "c")
	item := func(r simRow, v verdict) planItem {
		return planItem{check: fixedCheck(r.desc(), v), row: &r}
	}

	t.Run("区分ごとに ok と NG の件数を出し、NG の行を続け、最後に結果の行を出す", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer

		err := runPlanChecks(nil, &out, []planGroup{
			{heading: "# a", items: []planItem{item(ok, verdict{ok: true}), item(ng, verdict{reason: "r"})}},
			{heading: "# b", items: []planItem{item(ok, verdict{ok: true})}},
			{heading: "# c", items: []planItem{{check: fixedCheck("前提", verdict{})}}},
		})

		require.ErrorIs(t, err, errMismatch)
		assert.Equal(t, "# a: ok 1 件、NG 1 件\nNG  apply: b c が allowed（r）\n# b: ok 1 件、NG 0 件\n# c: ok 0 件、NG 1 件\nNG  前提\n# 結果: 4 件のうち NG 2 件\n", out.String())
	})

	t.Run("すべて ok ならエラーを返さない", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer

		require.NoError(t, runPlanChecks(nil, &out, []planGroup{{heading: "# a", items: []planItem{item(ok, verdict{ok: true})}}}))
		assert.Equal(t, "# a: ok 1 件、NG 0 件\n# 結果: 1 件のうち NG 0 件\n", out.String())
	})

	for name, groups := range map[string][]planGroup{
		"区分が無い":       nil,
		"区分に確認が1件も無い": {{heading: "# a"}},
	} {
		t.Run(name+"なら errNoChecks で、結果の行を出さない", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			require.ErrorIs(t, runPlanChecks(nil, &out, groups), errNoChecks)
			assert.NotContains(t, out.String(), "# 結果")
		})
	}

	t.Run("aws を起動できなくなれば、結果の行を出さずにそのエラーを返す", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer

		q := newQuery(func(...string) awsResult { return awsResult{err: os.ErrNotExist} }, testRegion)

		err := runPlanChecks(q, &out, []planGroup{{heading: "# a", items: []planItem{{check: simCheck(ok, testNames("")), row: &ok}}}})

		require.ErrorIs(t, err, errAWSStart)
		assert.Empty(t, out.String())
	})
}

func Test_canaryRow(t *testing.T) {
	t.Parallel()

	for region, other := range map[string]string{"ap-northeast-1": "us-west-2", "us-west-2": "eu-west-1"} {
		t.Run(region+" なら、type の TargetRegion を "+region+" だけの一覧で与えて allowed を期待する行", func(t *testing.T) {
			t.Parallel()

			n := newNames(verifyOptions{project: testProject, region: region}, testAccount)

			got, ok := canaryRow(n, stackInstanceRows(n, otherRegion(n)))

			require.True(t, ok)
			assert.Equal(t, simCtx(allowed, "apply-identity", "arn:aws:cloudformation:*::type/resource/AWS-IAM-Role",
				[]string{"ContextKeyName=cloudformation:TargetRegion,ContextKeyValues=" + region + ",ContextKeyType=stringList"},
				"cloudformation:CreateStackInstances", "cloudformation:UpdateStackInstances"), got)
			assert.NotContains(t, got.context[0], other)
		})
	}
}

func Test_canaryDocument(t *testing.T) {
	t.Parallel()

	t.Run("行の action を行の resource に、TargetRegion を StringEquals で region に限って許す", func(t *testing.T) {
		t.Parallel()

		r := sim(allowed, "apply-identity", "arn:aws:cloudformation:*::type/resource/AWS-IAM-Role", "cloudformation:CreateStackInstances", "cloudformation:UpdateStackInstances")

		assert.JSONEq(t, `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["cloudformation:CreateStackInstances","cloudformation:UpdateStackInstances"],`+
			`"Resource":"arn:aws:cloudformation:*::type/resource/AWS-IAM-Role","Condition":{"StringEquals":{"cloudformation:TargetRegion":"ap-northeast-1"}}}]}`,
			canaryDocument(r, "ap-northeast-1"))
	})
}

func Test_runCanary(t *testing.T) {
	t.Parallel()

	n := testNames("")
	rows := stackInstanceRows(n, otherRegion(n))
	row, ok := canaryRow(n, rows)
	require.True(t, ok)

	row.docs = []string{canaryDocument(row, testRegion)}
	key := jsonKey(row.args(n)...)

	missing := `{"EvaluationResults":[{"EvalActionName":"cloudformation:CreateStackInstances","EvalDecision":"implicitDeny","MissingContextValues":["cloudformation:TargetRegion"]},` +
		`{"EvalActionName":"cloudformation:UpdateStackInstances","EvalDecision":"implicitDeny"}]}`

	for name, tc := range map[string]struct {
		r    awsResult
		want error
	}{
		"落ちるべき文書の action がすべて implicitDeny なら本走査へ進む":   {simResult(row.actions, implicitDeny), nil},
		"落ちるべき文書が allowed なら errCanary":                {simResult(row.actions, allowed), errCanary},
		"explicitDeny でも implicitDeny でなければ errCanary": {simResult(row.actions, explicitDeny), errCanary},
		"AccessDenied で評価が返らなければ errCanary":            {failed("An error occurred (AccessDenied) when calling the SimulateCustomPolicy operation: User: arn:aws:sts::" + testAccount + ":assumed-role/r/s"), errCanary},
		"応答を読めなければ errCanary":                          {okResult("x"), errCanary},
		"結果の配列が無ければ errCanary":                         {okResult(`{}`), errCanary},
		"足りない context があれば errCanary":                  {okResult(missing), errCanary},
		"返った action の数が要求と違えば errCanary":               {simResult(row.actions[:1], implicitDeny), errCanary},
		"返った action が要求より多ければ errCanary":               {simResult(append(slices.Clone(row.actions), "cloudformation:DeleteStackInstances"), implicitDeny), errCanary},
		"同じ数で名前が違えば errCanary":                         {simResult([]string{"cloudformation:CreateStackInstances", "cloudformation:DeleteStackInstances"}, implicitDeny), errCanary},
		"返った action が0件なら errCanary":                   {okResult(`{"EvaluationResults":[]}`), errCanary},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := verifyFake(t, map[string]awsResult{key: tc.r}, nil)

			q := newQuery(f.fn, testRegion)
			q.terse = true

			err := runCanary(q, n, rows)

			if tc.want == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.want)
				assert.Equal(t, 2, exitCode(err))
				assert.NotContains(t, err.Error(), "arn:")
			}

			assert.Equal(t, []string{key}, f.calls, "simulate-custom-policy を1回だけ、落ちるべき文書で呼ぶ")
			assert.Contains(t, key, "iam simulate-custom-policy --policy-input-list ")
		})
	}

	t.Run("aws を起動できなければ errAWSStart", func(t *testing.T) {
		t.Parallel()

		err := runCanary(newQuery(func(...string) awsResult { return awsResult{err: os.ErrNotExist} }, testRegion), n, rows)

		require.ErrorIs(t, err, errAWSStart)
	})

	t.Run("表に落ちるべき文書の行が無ければ、aws を呼ばずに errCanary", func(t *testing.T) {
		t.Parallel()

		without := slices.DeleteFunc(slices.Clone(rows), func(r simRow) bool {
			c, ok := canaryRow(n, []simRow{r})

			return ok && slices.Equal(c.context, r.context)
		})
		require.Len(t, without, len(rows)-1)

		f := verifyFake(t, nil, nil)

		err := runCanary(newQuery(f.fn, testRegion), n, without)

		require.ErrorIs(t, err, errCanary)
		assert.Equal(t, 2, exitCode(err))
		assert.Empty(t, f.calls)
	})
}

// planSimHandler は、simulate-custom-policy に、落ちるべき文書なら implicitDeny を、changes の plan の表の行なら want
// （override が空でない判定を返せばその判定）を返す。同じ引数の行が2つあれば落とす。
func planSimHandler(t *testing.T, n names, changes []map[string]any, override func(simRow) string) func(args []string) (awsResult, bool) {
	t.Helper()

	canary, ok := canaryRow(n, stackInstanceRows(n, otherRegion(n)))
	require.True(t, ok)

	canary.docs = []string{canaryDocument(canary, n.region)}
	wants := map[string]string{strings.Join(canary.args(n), " "): implicitDeny}

	for _, g := range planPermissionGroups(n, planDocuments(mustChanges(t, changes), n), testKeyARN, "") {
		for _, it := range g.items {
			if it.row == nil {
				continue
			}

			want := it.row.want
			if override != nil {
				if d := override(*it.row); d != "" {
					want = d
				}
			}

			key := strings.Join(it.row.args(n), " ")
			_, dup := wants[key]
			require.False(t, dup, "同じ引数の行が2つある: %s", it.row.desc())

			wants[key] = want
		}
	}

	return func(args []string) (awsResult, bool) {
		want, ok := wants[strings.Join(args[:len(args)-4], " ")]
		if !ok || args[1] != "simulate-custom-policy" {
			return awsResult{}, false
		}

		return simResult(simActions(args), want), true
	}
}

func Test_checkPlanPermissions(t *testing.T) {
	t.Parallel()

	n := testNames("")
	opts := planPermOptions{plan: planPath, project: testProject, region: testRegion}
	keyKey := jsonKey("kms", "describe-key", "--key-id", testAlias)
	baseResponses := func() map[string]awsResult {
		return map[string]awsResult{stsAccountKey: okResult(testAccount), keyKey: okResult(describeKeyBody)}
	}
	setup := func(t *testing.T, changes []map[string]any, override func(simRow) string) (*fakeAWS, deps) {
		t.Helper()

		f := &fakeAWS{t: t, responses: baseResponses(), handler: planSimHandler(t, n, changes, override)}

		return f, verifyDeps(f, map[string]string{planPath: planOf(changes)})
	}
	lines := func(out bytes.Buffer) []string {
		return strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	}

	t.Run("すべて期待どおりなら、区分ごとの件数と結果の行だけを出し、resource の ARN と context の値を出さない", func(t *testing.T) {
		t.Parallel()

		f, d := setup(t, identityPlanChanges(), nil)

		var out bytes.Buffer

		require.NoError(t, checkPlanPermissions(opts, &out, d))

		got := lines(out)
		assert.Equal(t, "# plan-permissions（profile: tabp-admin、region: "+testRegion+"）", got[0])
		assert.Equal(t, "# 評価器の確認: 落ちるべき文書が NG になった", got[1])
		assert.Equal(t, "# 結果: "+strconv.Itoa(planPermTotal)+" 件のうち NG 0 件", got[len(got)-1])
		assert.Len(t, got, 3+len(permissionSections(n, testKeyARN, "")))

		for _, l := range got[2 : len(got)-1] {
			assert.Regexp(t, `^# .+: ok [0-9]+ 件、NG 0 件$`, l)
		}

		assert.NotContains(t, out.String(), "arn:")
		assert.NotContains(t, out.String(), "ContextKey")
		assert.NotContains(t, out.String(), testAccount)

		sims := 0

		for _, c := range f.calls {
			if strings.HasPrefix(c, "iam simulate-custom-policy ") {
				sims++
			}

			assert.NotContains(t, c, "simulate-principal-policy")
		}

		assert.Equal(t, planPermTotal+1, sims, "落ちるべき文書の1回と、表の行の数")
	})

	t.Run("表の行の判定が違えば、role・action・期待・理由だけの NG の行を出して errMismatch", func(t *testing.T) {
		t.Parallel()

		_, d := setup(t, identityPlanChanges(), func(r simRow) string {
			if slices.Equal(r.actions, []string{"iam:SimulateCustomPolicy"}) {
				return implicitDeny
			}

			return ""
		})

		var out bytes.Buffer

		require.ErrorIs(t, checkPlanPermissions(opts, &out, d), errMismatch)
		assert.Contains(t, lines(out), "# S3 と IAM: ok "+strconv.Itoa(len(permS3Descs)-1)+" 件、NG 1 件")
		assert.Contains(t, lines(out), "NG  plan: iam:SimulateCustomPolicy が allowed（違う判定: iam:SimulateCustomPolicy=implicitDeny）")
		assert.Equal(t, "# 結果: "+strconv.Itoa(planPermTotal)+" 件のうち NG 1 件", lines(out)[len(lines(out))-1])
	})

	t.Run("simulate の失敗の理由はエラーコードと ARN を伏せた本文で、stderr の ARN を出さない", func(t *testing.T) {
		t.Parallel()

		f, d := setup(t, identityPlanChanges(), nil)
		inner := f.handler
		f.handler = func(args []string) (awsResult, bool) {
			if slices.Contains(args, "--action-names") && slices.Contains(args, "iam:SimulateCustomPolicy") {
				return failed("\nAn error occurred (AccessDenied) when calling the SimulateCustomPolicy operation: User: arn:aws:sts::" + testAccount +
					":assumed-role/tabp-admin-ci-plan/s is not authorized to perform: iam:SimulateCustomPolicy\n"), true
			}

			return inner(args)
		}

		var out bytes.Buffer

		require.ErrorIs(t, checkPlanPermissions(opts, &out, d), errMismatch)
		assert.Contains(t, lines(out), "NG  plan: iam:SimulateCustomPolicy が allowed（呼び出しが失敗: AccessDenied: User: <arn> is not authorized to perform: iam:SimulateCustomPolicy）")
		assert.NotContains(t, out.String(), "arn:")
		assert.NotContains(t, out.String(), "assumed-role")
	})

	t.Run("state の鍵を読めない理由も、エラーコードと ARN を伏せた本文だけを出す", func(t *testing.T) {
		t.Parallel()

		f, d := setup(t, identityPlanChanges(), nil)
		f.responses[keyKey] = failed("An error occurred (NotFoundException) when calling the DescribeKey operation: Alias arn:aws:kms:" + testRegion + ":" + testAccount + ":alias/x is not found.")

		var out bytes.Buffer

		require.ErrorIs(t, checkPlanPermissions(opts, &out, d), errMismatch)
		assert.Contains(t, lines(out), "NG  state の鍵の ARN（alias/tabp-admin-tfstate）を読めない（呼び出しが失敗: NotFoundException: Alias <arn> is not found.）")
		assert.Contains(t, lines(out), "NG  apply-identity: kms:DescribeKey kms:GetKeyPolicy kms:GetKeyRotationStatus kms:ListResourceTags kms:EnableKeyRotation kms:PutKeyPolicy が allowed（state の鍵の ARN を読めない）")
		assert.Equal(t, "# 結果: "+strconv.Itoa(planPermTotal+1)+" 件のうち NG 9 件", lines(out)[len(lines(out))-1])
		assert.NotContains(t, out.String(), "arn:")
	})

	t.Run("統制の policy が plan で付いていなければ、注記で通さずに NG（apply の後に確かめる host の verify と違う）", func(t *testing.T) {
		t.Parallel()

		changes := withChange(identityPlanChanges(), `aws_iam_role_policy_attachments_exclusive.this["apply_identity"]`,
			setAfter(`{"role_name":"tabp-admin-ci-apply-identity","policy_arns":["arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-identity-manage"]}`, `{}`))
		_, d := setup(t, changes, nil)

		var out bytes.Buffer

		err := checkPlanPermissions(opts, &out, d)

		require.ErrorIs(t, err, errMismatch)
		assert.Contains(t, lines(out), "NG  この区分の前提の managed policy が plan で付いていない（条件つきの "+strconv.Itoa(governanceGated)+" 行を確かめない）")
		assert.Contains(t, lines(out), "NG  この区分の前提の managed policy が plan で付いていない（条件つきの "+strconv.Itoa(delegatedGated)+" 行を確かめない）")
		assert.NotContains(t, out.String(), "確かめていない区分")
		assert.Equal(t, "# 結果: "+strconv.Itoa(planPermTotal-governanceGated-delegatedGated+2)+" 件のうち NG 2 件", lines(out)[len(lines(out))-1])
	})

	t.Run("落ちるべき文書が NG にならなければ、本走査をせずに errCanary", func(t *testing.T) {
		t.Parallel()

		f := &fakeAWS{t: t, responses: baseResponses(), handler: func(args []string) (awsResult, bool) {
			return simResult(simActions(args), allowed), args[1] == "simulate-custom-policy"
		}}

		var out bytes.Buffer

		err := checkPlanPermissions(opts, &out, verifyDeps(f, map[string]string{planPath: planOf(identityPlanChanges())}))

		require.ErrorIs(t, err, errCanary)
		assert.Equal(t, 2, exitCode(err))
		assert.NotContains(t, out.String(), "# 結果")

		sims := 0

		for _, c := range f.calls {
			if strings.HasPrefix(c, "iam simulate-custom-policy ") {
				sims++
			}
		}

		assert.Equal(t, 1, sims)
	})

	for name, canary := range map[string]awsResult{
		"AccessDenied":   failed("An error occurred (AccessDenied) when calling the SimulateCustomPolicy operation: User: arn:aws:sts::" + testAccount + ":assumed-role/r/s"),
		"読めない応答":         okResult("x"),
		"action の数の食い違い": simResult([]string{"cloudformation:CreateStackInstances"}, implicitDeny),
	} {
		t.Run("落ちるべき文書の評価が "+name+" なら、本走査をせずに errCanary で、ARN を出さない", func(t *testing.T) {
			t.Parallel()

			f := &fakeAWS{t: t, responses: baseResponses(), handler: func(args []string) (awsResult, bool) {
				return canary, args[1] == "simulate-custom-policy"
			}}

			var out bytes.Buffer

			err := checkPlanPermissions(opts, &out, verifyDeps(f, map[string]string{planPath: planOf(identityPlanChanges())}))

			require.ErrorIs(t, err, errCanary)
			assert.Equal(t, 2, exitCode(err))
			assert.NotContains(t, err.Error(), "arn:")
			assert.NotContains(t, out.String(), "# 結果")

			sims := 0

			for _, c := range f.calls {
				if strings.HasPrefix(c, "iam simulate-custom-policy ") {
					sims++
				}
			}

			assert.Equal(t, 1, sims, "落ちるべき文書の1回だけ")
		})
	}

	for name, tc := range map[string]struct {
		files map[string]string
		want  error
	}{
		"plan のファイルを読めない":     {nil, errPlanPermInput},
		"plan が JSON でない":     {map[string]string{planPath: "x"}, errPlanPermJSON},
		"resource_changes が空": {map[string]string{planPath: `{"resource_changes":[]}`}, errPlanPermEmpty},
	} {
		t.Run(name+"なら、aws を呼ばずに評価できない", func(t *testing.T) {
			t.Parallel()

			f := verifyFake(t, nil, nil)

			err := checkPlanPermissions(opts, &bytes.Buffer{}, verifyDeps(f, tc.files))

			require.ErrorIs(t, err, tc.want)
			assert.Equal(t, 2, exitCode(err))
			assert.Empty(t, f.calls)
		})
	}

	t.Run("CI の role の文書が plan に1つも無ければ、simulate を呼ばずに評価できない", func(t *testing.T) {
		t.Parallel()

		f := verifyFake(t, map[string]awsResult{stsAccountKey: okResult(testAccount)}, nil)
		plan := planOf([]map[string]any{planRC("aws_s3_bucket.this", "aws_s3_bucket", `["no-op"]`, `{"bucket":"b"}`, `{}`)})

		var out bytes.Buffer

		err := checkPlanPermissions(opts, &out, verifyDeps(f, map[string]string{planPath: plan}))

		require.ErrorIs(t, err, errPlanPermNoDocs)
		assert.Equal(t, 2, exitCode(err))
		assert.Equal(t, []string{stsAccountKey}, f.calls)
		assert.Contains(t, err.Error(), "plan: plan に tabp-admin-ci-plan の aws_iam_role_policies_exclusive が 0 個")
	})

	t.Run("AWS_PROFILE が無ければ、plan も aws も読まずに終える", func(t *testing.T) {
		t.Parallel()

		f := verifyFake(t, nil, nil)
		d := verifyDeps(f, nil)
		d.getenv = func(string) string { return "" }

		require.ErrorIs(t, checkPlanPermissions(opts, &bytes.Buffer{}, d), errNoProfile)
		assert.Empty(t, f.calls)
	})

	t.Run("アカウント ID を取れなければ errNoAccount で、理由に stderr の ARN を出さない", func(t *testing.T) {
		t.Parallel()

		f := verifyFake(t, map[string]awsResult{stsAccountKey: failed("An error occurred (ExpiredToken) when calling the GetCallerIdentity operation: arn:aws:sts::" + testAccount + ":x")}, nil)

		err := checkPlanPermissions(opts, &bytes.Buffer{}, verifyDeps(f, map[string]string{planPath: planOf(identityPlanChanges())}))

		require.ErrorIs(t, err, errNoAccount)
		assert.Contains(t, err.Error(), "ExpiredToken")
		assert.NotContains(t, err.Error(), "arn:")
	})

	t.Run("アカウント ID が12桁でなければ errNoAccount", func(t *testing.T) {
		t.Parallel()

		f := verifyFake(t, map[string]awsResult{stsAccountKey: okResult("None")}, nil)

		err := checkPlanPermissions(opts, &bytes.Buffer{}, verifyDeps(f, map[string]string{planPath: planOf(identityPlanChanges())}))

		require.ErrorIs(t, err, errNoAccount)
	})

	for _, failing := range []string{"get-caller-identity", "describe-key", "simulate-custom-policy"} {
		t.Run(failing+" で aws を起動できなければ errAWSStart", func(t *testing.T) {
			t.Parallel()

			f, d := setup(t, identityPlanChanges(), nil)
			d.aws = func(args ...string) awsResult {
				if strings.Contains(strings.Join(args, " "), failing) {
					return awsResult{err: os.ErrNotExist}
				}

				return f.fn(args...)
			}

			require.ErrorIs(t, checkPlanPermissions(opts, &bytes.Buffer{}, d), errAWSStart)
		})
	}

	t.Run("読むだけの操作（get-・list-・describe-・simulate-）しか呼ばない", func(t *testing.T) {
		t.Parallel()

		f, d := setup(t, identityPlanChanges(), nil)

		require.NoError(t, checkPlanPermissions(opts, &bytes.Buffer{}, d))
		assert.Empty(t, writeOps(f.calls))
		require.GreaterOrEqual(t, len(f.calls), 2)
		assert.Equal(t, []string{stsAccountKey, keyKey}, f.calls[:2])
	})

	t.Run("落ちるべき文書と、表の代表の行（context あり・なし）を、組み立てた引数のとおりに呼ぶ", func(t *testing.T) {
		t.Parallel()

		f, d := setup(t, identityPlanChanges(), nil)

		require.NoError(t, checkPlanPermissions(opts, &bytes.Buffer{}, d))

		canary := `iam simulate-custom-policy --policy-input-list ["{\"Statement\":[{\"Action\":[\"cloudformation:CreateStackInstances\",\"cloudformation:UpdateStackInstances\"],` +
			`\"Condition\":{\"StringEquals\":{\"cloudformation:TargetRegion\":\"ap-northeast-1\"}},\"Effect\":\"Allow\",\"Resource\":\"arn:aws:cloudformation:*::type/resource/AWS-IAM-Role\"}],` +
			`\"Version\":\"2012-10-17\"}"] --resource-arns arn:aws:cloudformation:*::type/resource/AWS-IAM-Role ` +
			`--context-entries ContextKeyName=cloudformation:TargetRegion,ContextKeyValues=ap-northeast-1,ContextKeyType=stringList ` +
			`--action-names cloudformation:CreateStackInstances cloudformation:UpdateStackInstances --output json --region ap-northeast-1`
		withContext := `iam simulate-custom-policy --policy-input-list ["{\"Version\":\"2012-10-17\",\"Statement\":[{\"Sid\":\"unlockidentity\",\"Effect\":\"Allow\",\"Action\":\"s3:GetObject\",\"Resource\":\"*\"}]}"] ` +
			`--resource-arns arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an ` +
			`--context-entries ContextKeyName=s3:prefix,ContextKeyValues=identity/terraform.tfstate.tflock,ContextKeyType=string ` +
			`--action-names s3:ListBucket --output json --region ap-northeast-1`
		withoutContext := `iam simulate-custom-policy --policy-input-list ["{\"Version\":\"2012-10-17\",\"Statement\":[{\"Sid\":\"unlock\",\"Effect\":\"Allow\",\"Action\":\"s3:GetObject\",\"Resource\":\"*\"}]}"] ` +
			`--resource-arns arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/workload/terraform.tfstate.tflock ` +
			`--action-names s3:GetObject s3:DeleteObject --output json --region ap-northeast-1`

		require.GreaterOrEqual(t, len(f.calls), 3)
		assert.Equal(t, canary, f.calls[2], "落ちるべき文書は本走査より先")
		assert.Contains(t, f.calls, withContext)
		assert.Contains(t, f.calls, withoutContext)
	})

	t.Run("context を持つ行と resource が ARN の行が NG でも、出力に ARN・context・アカウント ID を出さない", func(t *testing.T) {
		t.Parallel()

		identityKey := "arn:aws:s3:::" + testBucket + "/identity/terraform.tfstate"
		_, d := setup(t, identityPlanChanges(), func(r simRow) string {
			switch {
			case r.role == n.unlockIdentity && r.want == allowed && len(r.context) > 0:
				return implicitDeny
			case r.role == n.applyIdentity && r.resource == identityKey && slices.Equal(r.actions, []string{"s3:PutObject"}):
				return implicitDeny
			}

			return ""
		})

		var out bytes.Buffer

		require.ErrorIs(t, checkPlanPermissions(opts, &out, d), errMismatch)
		assert.Contains(t, lines(out), "NG  unlock-identity: s3:ListBucket が allowed（違う判定: s3:ListBucket=implicitDeny）")
		assert.Contains(t, lines(out), "NG  apply-identity: s3:PutObject が allowed（違う判定: s3:PutObject=implicitDeny）")
		assert.Equal(t, "# 結果: "+strconv.Itoa(planPermTotal)+" 件のうち NG 2 件", lines(out)[len(lines(out))-1])

		for _, v := range []string{"arn:", "ContextKey", "s3:prefix", "terraform.tfstate", testAccount, testBucket} {
			assert.NotContains(t, out.String(), v)
		}
	})

	// withMissing は、simulate-custom-policy の各結果の MissingContextValues に keys を足す handler。
	withMissing := func(inner func([]string) (awsResult, bool), keys ...string) func([]string) (awsResult, bool) {
		return func(args []string) (awsResult, bool) {
			r, ok := inner(args)
			if !ok || r.failed() {
				return r, ok
			}

			var v map[string][]map[string]any
			require.NoError(t, json.Unmarshal(r.stdout, &v))

			for _, e := range v["EvaluationResults"] {
				e["MissingContextValues"] = keys
			}

			b, err := json.Marshal(v)
			require.NoError(t, err)

			return okResult(string(b)), true
		}
	}

	t.Run("simulate-custom-policy が文書のすべての条件キーを足りないと返しても、行に関わらないキーは数えない", func(t *testing.T) {
		t.Parallel()

		f, d := setup(t, identityPlanChanges(), nil)
		f.handler = withMissing(f.handler, "kms:BypassPolicyLockoutSafetyCheck", "iam:PolicyARN", "aws:TagKeys")

		var out bytes.Buffer

		require.NoError(t, checkPlanPermissions(opts, &out, d))
		assert.NotContains(t, out.String(), "足りない context")
	})

	t.Run("行に関わる Deny の文の条件キーが足りなければ、context を与える行は NG", func(t *testing.T) {
		t.Parallel()

		doc := `{"Version":"2012-10-17","Statement":[{"Sid":"unlockidentity","Effect":"Allow","Action":"s3:GetObject","Resource":"*"},` +
			`{"Sid":"DenyOutside","Effect":"Deny","Action":"s3:ListBucket","Resource":"*","Condition":{"StringNotEquals":{"aws:RequestedRegion":["ap-northeast-1"]}}}]}`
		changes := withChange(identityPlanChanges(), `aws_iam_role_policy.this["unlock_identity"]`,
			setAfter(jsonOf(map[string]any{"name": "unlock_identity", "role": "tabp-admin-ci-unlock-identity", "policy": doc}), `{}`))
		f, d := setup(t, changes, nil)
		f.handler = withMissing(f.handler, "aws:RequestedRegion", "kms:KeySpec")

		var out bytes.Buffer

		require.ErrorIs(t, checkPlanPermissions(opts, &out, d), errMismatch)
		// context を与える unlock-identity の ListBucket の2行（prefix が lock と state）。
		assert.Contains(t, lines(out), "NG  unlock-identity: s3:ListBucket が allowed（評価に足りない context: aws:RequestedRegion）")
		assert.Contains(t, lines(out), "NG  unlock-identity: s3:ListBucket が implicitDeny（評価に足りない context: aws:RequestedRegion）")
		assert.Equal(t, "# 結果: "+strconv.Itoa(planPermTotal)+" 件のうち NG 2 件", lines(out)[len(lines(out))-1])
	})

	t.Run("context の行の ValidationError は、本文を出しても ARN・ID・context の値を出さない", func(t *testing.T) {
		t.Parallel()

		f, d := setup(t, identityPlanChanges(), nil)
		inner := f.handler
		f.handler = func(args []string) (awsResult, bool) {
			if slices.ContainsFunc(args, func(a string) bool { return strings.HasPrefix(a, "ContextKeyName=iam:PolicyARN,") }) {
				entry := args[slices.Index(args, "--context-entries")+1]

				return failed("\nAn error occurred (ValidationError) when calling the SimulateCustomPolicy operation: Invalid context entry " + entry +
					" for resource arn:aws:iam::" + testAccount + ":role/tabp-admin-ci-apply in account " + testAccount + "\n"), true
			}

			return inner(args)
		}

		var out bytes.Buffer

		require.ErrorIs(t, checkPlanPermissions(opts, &out, d), errMismatch)
		assert.Contains(t, lines(out), "NG  apply-identity: iam:AttachRolePolicy が allowed（呼び出しが失敗: ValidationError: Invalid context entry <context> for resource <arn> in account <account>）")

		for _, v := range []string{"arn:", "ContextKey", testAccount, "tabp-admin-ci-apply-identity-governance", "AdministratorAccess"} {
			assert.NotContains(t, out.String(), v)
		}
	})

	t.Run("CI の role の1つだけ文書が決まらなければ、その role の行を NG にし、ほかの行は評価して errMismatch", func(t *testing.T) {
		t.Parallel()

		changes := withChange(identityPlanChanges(), `aws_iam_role_policy.this["unlock"]`, nil)
		f, d := setup(t, changes, nil)

		var out bytes.Buffer

		err := checkPlanPermissions(opts, &out, d)

		require.ErrorIs(t, err, errMismatch)
		assert.NotErrorIs(t, err, errPlanPermNoDocs)

		reason := "inline policy の文書が 0 個で、" + oidcPrefix + `aws_iam_role_policies_exclusive.this["unlock"] の policy_names の 1 個と合わない`
		assert.Contains(t, lines(out), "NG  unlock: s3:GetObject s3:DeleteObject が allowed（"+reason+"）")
		assert.Contains(t, lines(out), "# unlock の role: ok 0 件、NG "+strconv.Itoa(len(permUnlockDescs))+" 件")
		assert.Equal(t, "# 結果: "+strconv.Itoa(planPermTotal)+" 件のうち NG "+strconv.Itoa(len(permUnlockDescs))+" 件", lines(out)[len(lines(out))-1])

		sims := 0

		for _, c := range f.calls {
			if strings.HasPrefix(c, "iam simulate-custom-policy ") {
				sims++
			}
		}

		assert.Equal(t, planPermTotal-len(permUnlockDescs)+1, sims, "決まらない role の行は aws を呼ばない")
	})
}

func Test_relevantMissing(t *testing.T) {
	t.Parallel()

	deny := `{"Statement":[{"Effect":"Allow","Action":"s3:ListBucket","Resource":"arn:aws:s3:::b","Condition":{"StringLike":{"s3:prefix":"x/*"}}},` +
		`{"Effect":"Deny","Action":["s3:List*"],"Resource":"*","Condition":{"StringNotEquals":{"aws:RequestedRegion":["r"]}}},` +
		`{"Effect":"Allow","Action":"kms:Decrypt","Resource":"*","Condition":{"StringEquals":{"kms:KeySpec":"x"}}}]}`

	for name, tc := range map[string]struct {
		docs     []string
		action   string
		resource string
		keys     []string
		want     []string
	}{
		"関わる Deny の文のキーは残す":      {[]string{deny}, "s3:ListBucket", "arn:aws:s3:::b", []string{"aws:RequestedRegion", "kms:KeySpec"}, []string{"aws:RequestedRegion"}},
		"関わらない文のキーだけなら空":         {[]string{deny}, "s3:ListBucket", "arn:aws:s3:::b", []string{"kms:KeySpec", "ec2:Region"}, []string{}},
		"Resource が当たらない文のキーは除く": {[]string{deny}, "s3:ListBucket", "arn:aws:s3:::other", []string{"s3:prefix", "aws:RequestedRegion"}, []string{"aws:RequestedRegion"}},
		"キーは大文字小文字を区別しない":        {[]string{deny}, "S3:listbucket", "arn:aws:s3:::b", []string{"AWS:RequestedRegion"}, []string{"AWS:RequestedRegion"}},
		"複数の文書の文を合わせる":           {[]string{`{"Statement":[]}`, deny}, "kms:Decrypt", "arn:k", []string{"kms:KeySpec", "s3:prefix"}, []string{"kms:KeySpec"}},
		"Statement が object 1つで Action と Resource が文字列でも読む": {[]string{`{"Statement":{"Effect":"Deny","Action":"iam:PassRole","Resource":"arn:r","Condition":{"StringNotEquals":{"iam:PassedToService":"x"}}}}`},
			"iam:PassRole", "arn:r", []string{"iam:PassedToService", "aws:TagKeys"}, []string{"iam:PassedToService"}},
		"NotAction の文は当たるとみなす": {[]string{`{"Statement":[{"Effect":"Deny","NotAction":"s3:*","Resource":"*","Condition":{"Bool":{"aws:SecureTransport":"false"}}}]}`},
			"s3:GetObject", "arn:x", []string{"aws:SecureTransport"}, []string{"aws:SecureTransport"}},
		"文書を読めなければ keys をそのまま返す": {[]string{"x"}, "s3:A", "arn:x", []string{"a", "b"}, []string{"a", "b"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, relevantMissing(tc.docs, tc.action, tc.resource, tc.keys))
		})
	}
}

func Test_statementApplies(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		st       string
		action   string
		resource string
		want     bool
	}{
		"Action と Resource が当たる":              {`{"Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*"}`, "s3:GetObject", "arn:aws:s3:::b/k", true},
		"Action が当たらない":                       {`{"Action":["s3:PutObject"],"Resource":"*"}`, "s3:GetObject", "arn:x", false},
		"Action の wildcard":                   {`{"Action":"s3:Get*","Resource":"*"}`, "s3:GetObject", "arn:x", true},
		"Action は大文字小文字を区別しない":                {`{"Action":"S3:GETOBJECT","Resource":"*"}`, "s3:GetObject", "arn:x", true},
		"Resource が当たらない":                     {`{"Action":"*","Resource":"arn:aws:s3:::b"}`, "s3:GetObject", "arn:aws:s3:::c", false},
		"NotAction を持てば当たる":                   {`{"NotAction":"s3:GetObject","Resource":"*"}`, "s3:GetObject", "arn:x", true},
		"NotResource を持てば当たる":                 {`{"Action":"s3:GetObject","NotResource":"arn:x"}`, "s3:GetObject", "arn:x", true},
		"行の resource が * ならどの Resource にも当たる": {`{"Action":"kms:ListAliases","Resource":"arn:aws:kms:r:1:key/k"}`, "kms:ListAliases", "*", true},
		"Action を読めなければ当たる":                   {`{"Resource":"*"}`, "s3:GetObject", "arn:x", true},
		"object でなければ当たる":                     {`"x"`, "s3:GetObject", "arn:x", true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var st any
			require.NoError(t, json.Unmarshal([]byte(tc.st), &st))

			assert.Equal(t, tc.want, statementApplies(st, tc.action, tc.resource))
		})
	}
}

func Test_wildcardMatch(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		pattern, s string
		want       bool
	}{
		{"s3:GetObject", "s3:GetObject", true},
		{"s3:GetObject", "s3:GetObjectAcl", false},
		{"s3:Get*", "s3:Get", true},
		{"s3:Get*", "s3:Ge", false},
		{"s3:*Object", "s3:Object", true},
		{"s3:*Object", "s3:GetObjects", false},
		{"s3:Get?bject", "s3:GetObject", true},
		{"s3:Get?bject", "s3:Getbject", false},
		{"s3:Get??bject", "s3:GetObject", false},
		{"*", "", true},
		{"?", "", false},
		{"", "", true},
		{"a*b*c", "aXbYbZc", true},
		{"a*b*c", "aXbYbZ", false},
		{"S3:GETOBJECT", "s3:getobject", true},
	} {
		t.Run(tc.pattern+" と "+tc.s, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, wildcardMatch(tc.pattern, tc.s))
		})
	}
}
