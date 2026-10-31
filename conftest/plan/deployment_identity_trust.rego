# deployment-identity の CI role の信頼条件の形（ADR-0602 決定14・15、deployment-identity/ADR-0101 決定1-2）。
# 値は入力ごとに変わるので持たない。どの root のどの入力でも成り立つべき形だけを見る。
package plan.deployment_identity_trust

import data.plan.lib

cfg := data.policy_plan.deployment_identity

issuer := "token.actions.githubusercontent.com"

required_keys := {sprintf("%s:%s", [issuer, k]) | some k in ["aud", "sub", "repository_id", "repository_owner_id"]}

# 不変形式の sub: repo:<owner>@<owner_id>/<repo>@<repo_id>:environment:<name>
sub_pattern := `^repo:[^/@:*?]+@([0-9]+)/[^/@:*?]+@([0-9]+):environment:[^:*?]+$`

trusts contains r if {
	some r in lib.readable
	r.document.type == "aws_iam_role"
	r.document.attr == "assume_role_policy"
	lib.strip_index(r.document.address) in cfg.roles
}

deny contains msg if {
	some role in cfg.roles
	count({d | some d in lib.documents; d.type == "aws_iam_role"; lib.strip_index(d.address) == role}) == 0
	msg := sprintf("[PLAN-TRUST-00] 信頼条件の対象 %s が入力にありません。address が変わったなら data.policy_plan.deployment_identity を直してください", [role])
}

deny contains msg if {
	some r in trusts
	count(lib.statements(r.body)) != 1
	msg := sprintf("[PLAN-TRUST-01] %s: %s の信頼条件は statement を1つだけ持ちます", [r.document.path, r.document.address])
}

deny contains msg if {
	some r in trusts
	some s in lib.statements(r.body)
	not allow_web_identity(s)
	msg := sprintf("[PLAN-TRUST-02] %s: %s の信頼条件が Allow の sts:AssumeRoleWithWebIdentity だけではありません", [r.document.path, r.document.address])
}

deny contains msg if {
	some r in trusts
	some s in lib.statements(r.body)
	not federated_by_provider(r.document.path, s)
	msg := sprintf("[PLAN-TRUST-03] %s: %s の Principal が同じ module の OIDC provider の Federated ではありません", [r.document.path, r.document.address])
}

deny contains msg if {
	some r in trusts
	some s in lib.statements(r.body)
	object.keys(object.get(s, "Condition", {})) != {"StringEquals"}
	msg := sprintf("[PLAN-TRUST-04] %s: %s の Condition の演算子が StringEquals だけではありません", [r.document.path, r.document.address])
}

deny contains msg if {
	some r in trusts
	some s in lib.statements(r.body)
	object.keys(string_equals(s)) != required_keys
	msg := sprintf("[PLAN-TRUST-05] %s: %s の StringEquals のキーが %v と一致しません", [r.document.path, r.document.address, required_keys])
}

deny contains msg if {
	some r in trusts
	some s in lib.statements(r.body)
	some k, v in string_equals(s)
	not single_literal(v)
	msg := sprintf("[PLAN-TRUST-06] %s: %s の %s が1つの値ではないか、ワイルドカード（* ?）を含みます", [r.document.path, r.document.address, k])
}

deny contains msg if {
	some r in trusts
	some s in lib.statements(r.body)
	not aud_is_sts(string_equals(s))
	msg := sprintf("[PLAN-TRUST-07] %s: %s の aud が sts.amazonaws.com ではありません", [r.document.path, r.document.address])
}

deny contains msg if {
	some r in trusts
	some s in lib.statements(r.body)
	not sub_consistent(string_equals(s))
	msg := sprintf("[PLAN-TRUST-08] %s: %s の sub が environment の不変形式でないか、repository_id / repository_owner_id と一致しません", [r.document.path, r.document.address])
}

# Condition か StringEquals が無い文書でも、これを使う規則が未定義で黙らないよう空の object を返す。
string_equals(s) := object.get(object.get(s, "Condition", {}), "StringEquals", {})

aud_is_sts(eq) if eq[sprintf("%s:aud", [issuer])] == "sts.amazonaws.com"

allow_web_identity(s) if {
	s.Effect == "Allow"
	lib.lower_all(s.Action) == ["sts:assumerolewithwebidentity"]
	not s.NotAction
	not s.NotPrincipal
}

federated_by_provider(path, s) if {
	some c in lib.changes
	c.path == path
	c.rc.address == cfg.oidc_provider
	is_string(c.rc.change.after.arn)
	s.Principal == {"Federated": c.rc.change.after.arn}
}

single_literal(v) if {
	is_string(v)
	not contains(v, "*")
	not contains(v, "?")
}

sub_consistent(eq) if {
	m := regex.find_all_string_submatch_n(sub_pattern, eq[sprintf("%s:sub", [issuer])], 1)
	m[0][1] == eq[sprintf("%s:repository_owner_id", [issuer])]
	m[0][2] == eq[sprintf("%s:repository_id", [issuer])]
}
