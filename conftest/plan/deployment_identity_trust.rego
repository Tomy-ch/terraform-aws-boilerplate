# deployment-identity の CI role の信頼条件の形（ADR-0602 決定14・15、deployment-identity/ADR-0101 決定1-2）。
# 値は入力ごとに変わるので持たない。どの root のどの入力でも成り立つべき形だけを見る。
package plan.deployment_identity_trust

import data.plan.lib

cfg := data.policy_plan.deployment_identity

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
	not lib.github_allow_web_identity(s)
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
	object.keys(lib.github_string_equals(s)) != lib.github_required_keys
	msg := sprintf("[PLAN-TRUST-05] %s: %s の StringEquals のキーが %v と一致しません", [r.document.path, r.document.address, lib.github_required_keys])
}

deny contains msg if {
	some r in trusts
	some s in lib.statements(r.body)
	some k, v in lib.github_string_equals(s)
	not lib.github_single_literal(v)
	msg := sprintf("[PLAN-TRUST-06] %s: %s の %s が1つの値ではないか、ワイルドカード（* ?）を含みます", [r.document.path, r.document.address, k])
}

deny contains msg if {
	some r in trusts
	some s in lib.statements(r.body)
	not lib.github_aud_is_sts(lib.github_string_equals(s))
	msg := sprintf("[PLAN-TRUST-07] %s: %s の aud が sts.amazonaws.com ではありません", [r.document.path, r.document.address])
}

deny contains msg if {
	some r in trusts
	some s in lib.statements(r.body)
	not lib.github_sub_consistent(lib.github_string_equals(s))
	msg := sprintf("[PLAN-TRUST-08] %s: %s の sub が environment の不変形式でないか、repository_id / repository_owner_id と一致しません", [r.document.path, r.document.address])
}

federated_by_provider(path, s) if {
	some c in lib.changes
	c.path == path
	lib.strip_index(c.rc.address) == cfg.oidc_provider
	is_string(c.rc.change.after.arn)
	s.Principal == {"Federated": c.rc.change.after.arn}
}
