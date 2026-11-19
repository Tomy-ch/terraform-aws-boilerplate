# 発火側（本リポジトリの外のリポジトリの CI）を信頼する role の信頼条件の形（ADR-0602 決定14 の第1項・決定15・決定19）。
# Principal の OIDC provider は入力で受けるので、同じ plan の resource とではなく ARN の形と突き合わせる。
package plan.publisher_trust

import data.plan.lib

default roles := []

roles := data.policy_plan.publisher_trust.roles if well_formed_roles(data.policy_plan.publisher_trust.roles)

well_formed_roles(rs) if {
	is_array(rs)
	count(rs) > 0
	every r in rs {
		is_string(r)
	}
}

provider_pattern := `^arn:aws[a-z-]*:iam::[0-9]{12}:oidc-provider/token\.actions\.githubusercontent\.com$`

trusts contains r if {
	some r in lib.readable
	r.document.type == "aws_iam_role"
	r.document.attr == "assume_role_policy"
	lib.strip_index(r.document.address) in roles
}

# 宣言が欠けると、どの role も見ずに黙る。
deny contains msg if {
	not well_formed_roles(object.get(object.get(data.policy_plan, "publisher_trust", {}), "roles", null))
	msg := "[PLAN-PUBTRUST-00] 宣言 data.policy_plan.publisher_trust.roles が無いか、1件以上の文字列の配列ではありません"
}

deny contains msg if {
	some role in roles
	count({d | some d in lib.documents; d.type == "aws_iam_role"; lib.strip_index(d.address) == role}) == 0
	msg := sprintf("[PLAN-PUBTRUST-00] 信頼条件の対象 %s が入力にありません。address が変わったなら data.policy_plan.publisher_trust を直してください", [role])
}

deny contains msg if {
	some r in trusts
	count(lib.statements(r.body)) != 1
	msg := sprintf("[PLAN-PUBTRUST-01] %s: %s の信頼条件は statement を1つだけ持ちます", [r.document.path, r.document.address])
}

deny contains msg if {
	some r in trusts
	some s in lib.statements(r.body)
	not lib.github_allow_web_identity(s)
	msg := sprintf("[PLAN-PUBTRUST-02] %s: %s の信頼条件が Allow の sts:AssumeRoleWithWebIdentity だけではありません", [r.document.path, r.document.address])
}

deny contains msg if {
	some r in trusts
	some s in lib.statements(r.body)
	not federated_by_github(s)
	msg := sprintf("[PLAN-PUBTRUST-03] %s: %s の Principal が GitHub Actions の OIDC provider の Federated ではありません", [r.document.path, r.document.address])
}

deny contains msg if {
	some r in trusts
	some s in lib.statements(r.body)
	object.keys(object.get(s, "Condition", {})) != {"StringEquals"}
	msg := sprintf("[PLAN-PUBTRUST-04] %s: %s の Condition の演算子が StringEquals だけではありません", [r.document.path, r.document.address])
}

deny contains msg if {
	some r in trusts
	some s in lib.statements(r.body)
	object.keys(lib.github_string_equals(s)) != lib.github_required_keys
	msg := sprintf("[PLAN-PUBTRUST-05] %s: %s の StringEquals のキーが %v と一致しません", [r.document.path, r.document.address, lib.github_required_keys])
}

deny contains msg if {
	some r in trusts
	some s in lib.statements(r.body)
	some k, v in lib.github_string_equals(s)
	not lib.github_single_literal(v)
	msg := sprintf("[PLAN-PUBTRUST-06] %s: %s の %s が1つの値ではないか、ワイルドカード（* ?）を含みます", [r.document.path, r.document.address, k])
}

deny contains msg if {
	some r in trusts
	some s in lib.statements(r.body)
	not lib.github_aud_is_sts(lib.github_string_equals(s))
	msg := sprintf("[PLAN-PUBTRUST-07] %s: %s の aud が sts.amazonaws.com ではありません", [r.document.path, r.document.address])
}

deny contains msg if {
	some r in trusts
	some s in lib.statements(r.body)
	not lib.github_sub_consistent(lib.github_string_equals(s))
	msg := sprintf("[PLAN-PUBTRUST-08] %s: %s の sub が environment の不変形式でないか、repository_id / repository_owner_id と一致しません", [r.document.path, r.document.address])
}

federated_by_github(s) if {
	is_string(s.Principal.Federated)
	regex.match(provider_pattern, s.Principal.Federated)
	object.keys(s.Principal) == {"Federated"}
}
