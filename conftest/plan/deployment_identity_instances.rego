# deployment-identity の規則を、宣言した instance（lib/instances.rego。reference のメンバーの形の instance）ごとに評価し直す
# （conftest/plan/README.md の plan.deployment_identity_instances）。入力は lib.member_view で instance を正規の接頭辞へ付け替え、
# 宣言は lib.member_decl で管理アカウントにだけ在る policy を除き、lib.member_form を真にする。9つの package の規則は正規の
# 形のまま、メンバーの instance にも効く。
package plan.deployment_identity_instances

import data.plan.lib

di := object.get(data.policy_plan, "deployment_identity", {})

members contains i if {
	some i in lib.instances
	i.root != null
}

declared := object.get(di, "management_only_policies", null)

well_formed_declaration if {
	is_array(declared)
	count(declared) > 0
	every a in declared {
		is_string(a)
		startswith(a, lib.di_module)
	}
	count({a | some a in declared}) == count(declared)
}

deny contains msg if {
	not well_formed_declaration
	msg := "[PLAN-DIM-00] 宣言 deployment_identity.management_only_policies が、deployment_identity.module で始まる、重ならない address の空でない配列ではありません"
}

present(address) if {
	some f in input
	some rc in f.contents.resource_changes
	rc.address == address
}

deny contains msg if {
	well_formed_declaration
	some a in declared
	not present(a)
	msg := sprintf("[PLAN-DIM-00] 宣言 deployment_identity.management_only_policies の %s が、正規の instance（管理アカウントの形）の入力にありません", [a])
}

# lib.member_form が外す存在の要求は、この宣言が除く policy の存在の要求である。
deny contains msg if {
	well_formed_declaration
	some k in [["apply_role", "governance", "management_policy"], ["bootstrap_assume", "apply_identity_policy"]]
	a := object.get(di, k, null)
	not a in declared
	msg := sprintf("[PLAN-DIM-00] 宣言 deployment_identity.%s（%v）が management_only_policies にありません。メンバーの形の評価が外す要求の根拠になりません", [concat(".", k), a])
}

deny contains msg if {
	well_formed_declaration
	some i in members
	some a in declared
	present(lib.rebase(a, i))
	msg := sprintf("[PLAN-DIM-00] instance %s が管理アカウントの形です（%s が入力に在ります）。メンバーの形の instance を宣言してください", [i.name, lib.rebase(a, i)])
}

# メンバーの形の instance が無ければ、下の規則はどれも何も見ていない。この package は reference の plan にだけ掛かる。
deny contains msg if {
	count(members) == 0
	msg := "[PLAN-DIM-00] 入力に、宣言したメンバーの形の instance（state_backend.instances）がありません。reference に置いてください"
}

deny contains msg if {
	some i in members
	some m in data.plan.deployment_identity_trust.deny with input as lib.member_view(i) with data.policy_plan as lib.member_decl with data.plan.lib.member_form as true
	msg := sprintf("[PLAN-DIM-01] instance %s: %s", [i.name, m])
}

deny contains msg if {
	some i in members
	some m in data.plan.deployment_identity_plan_role.deny with input as lib.member_view(i) with data.policy_plan as lib.member_decl with data.plan.lib.member_form as true
	msg := sprintf("[PLAN-DIM-01] instance %s: %s", [i.name, m])
}

deny contains msg if {
	some i in members
	some m in data.plan.deployment_identity_apply_role.deny with input as lib.member_view(i) with data.policy_plan as lib.member_decl with data.plan.lib.member_form as true
	msg := sprintf("[PLAN-DIM-01] instance %s: %s", [i.name, m])
}

deny contains msg if {
	some i in members
	some m in data.plan.deployment_identity_apply_identity_output.deny with input as lib.member_view(i) with data.policy_plan as lib.member_decl with data.plan.lib.member_form as true
	msg := sprintf("[PLAN-DIM-01] instance %s: %s", [i.name, m])
}

deny contains msg if {
	some i in members
	some m in data.plan.deployment_identity_unlock_role.deny with input as lib.member_view(i) with data.policy_plan as lib.member_decl with data.plan.lib.member_form as true
	msg := sprintf("[PLAN-DIM-01] instance %s: %s", [i.name, m])
}

deny contains msg if {
	some i in members
	some m in data.plan.deployment_identity_bootstrap_assume.deny with input as lib.member_view(i) with data.policy_plan as lib.member_decl with data.plan.lib.member_form as true
	msg := sprintf("[PLAN-DIM-01] instance %s: %s", [i.name, m])
}

deny contains msg if {
	some i in members
	some m in data.plan.deployment_identity_workload_policy.deny with input as lib.member_view(i) with data.policy_plan as lib.member_decl with data.plan.lib.member_form as true
	msg := sprintf("[PLAN-DIM-01] instance %s: %s", [i.name, m])
}

deny contains msg if {
	some i in members
	some m in data.plan.deployment_identity_region_deny.deny with input as lib.member_view(i) with data.policy_plan as lib.member_decl with data.plan.lib.member_form as true
	msg := sprintf("[PLAN-DIM-01] instance %s: %s", [i.name, m])
}

deny contains msg if {
	some i in members
	some m in data.plan.deployment_identity_policy_inventory.deny with input as lib.member_view(i) with data.policy_plan as lib.member_decl with data.plan.lib.member_form as true
	msg := sprintf("[PLAN-DIM-01] instance %s: %s", [i.name, m])
}
