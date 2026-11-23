# deployment-identity の module が作る customer managed policy の集合が、宣言 deployment_identity.customer_managed_policies と
# 一致すること（deployment-identity/ADR-0201 決定21）。internal/github-oidc の permissions.tftest.hcl は同じ宣言を読み、
# そのすべての policy の長さを入力の境界で測る。terraform test は同じ型の resource を列挙できないので、列挙はここが持つ。
package plan.deployment_identity_policy_inventory

import data.plan.lib

raw := object.get(object.get(data.policy_plan, "deployment_identity", {}), "customer_managed_policies", null)

address_pattern := `^module\.[a-z0-9_]+(\.module\.[a-z0-9_]+)*\.aws_iam_policy\.[a-z0-9_]+$`

well_formed if {
	is_array(raw)
	count(raw) > 0
	every a in raw {
		is_string(a)
		regex.match(address_pattern, a)
	}
}

declared := {a | some a in raw} if well_formed

declared := set() if not well_formed

module_of(address) := regex.replace(address, `\.aws_iam_policy\.[a-z0-9_]+$`, "")

modules := {module_of(a) | some a in declared}

present contains {"path": c.path, "address": lib.strip_index(c.rc.address)} if {
	some c in lib.changes
	c.rc.type == "aws_iam_policy"
	module_of(lib.strip_index(c.rc.address)) in modules
}

present_addresses := {p.address | some p in present}

deny contains msg if {
	not well_formed
	msg := "[PLAN-CMP-00] data.policy_plan.deployment_identity.customer_managed_policies が、index key を除いた aws_iam_policy の address の、空でない配列ではありません"
}

deny contains msg if {
	some p in present
	not p.address in declared
	msg := sprintf("[PLAN-CMP-01] %s: %s が deployment_identity.customer_managed_policies の宣言にありません。宣言に足し、permissions.tftest.hcl の長さの run が測る集合にも足してください", [p.path, p.address])
}

deny contains msg if {
	some a in declared
	not a in present_addresses
	msg := sprintf("[PLAN-CMP-01] deployment_identity.customer_managed_policies の %s が、aws_iam_policy として入力にありません。address が変わったなら宣言を直してください", [a])
}
