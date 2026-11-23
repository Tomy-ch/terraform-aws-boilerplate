# organization-baseline の SCP・RCP の形と付け先（organization-baseline/ADR-0101、ADR-0301 決定19、ADR-0602 決定11。
# conftest/plan/README.md の plan.organization_baseline_scp）。付け先は OU と Root の address と plan の値で判定する（ADR-0402 決定13）。
package plan.organization_baseline_scp

import data.plan.lib

ob := object.get(data.policy_plan, "organization_baseline", {})

policy_addresses := object.get(ob, "policies", {})

declared_targets := object.get(ob, "attachments", {})

ou_addresses := object.get(ob, "organizational_units", {})

task_actions := {lower(a) | some a in object.get(ob, "assume_root_task_actions", [])}

break_glass := object.get(ob, "break_glass", {})

# 規則が名指しで読む policy と OU。宣言から消えると、その規則が黙る。
required_policies := {
	"deny-leave-and-close", "deny-root-user", "deny-state-write", "protect-governance-roles", "deny-guardduty-stop",
	"data-perimeter-prod-resources", "data-perimeter-org-principals", "data-perimeter-prod-principals",
}

required_ous := {"Workloads", "Prod", "PolicyStaging"}

after(address) := c.rc.change.after if {
	some c in lib.changes
	c.rc.address == address
}

organization := after(object.get(ob, "organization", ""))

root_id := organization.roots[0].id

target_names[root_id] := "Root"

root_id_known if is_string(root_id)

attachment_declared(key) if is_array(declared_targets[key])

target_names[id] := name if {
	some name, address in ou_addresses
	id := after(address).id
	is_string(id)
}

policy_ids[id] := key if {
	some key, address in policy_addresses
	id := after(address).id
	is_string(id)
}

content(key) := json.unmarshal(after(policy_addresses[key]).content) if {
	is_string(after(policy_addresses[key]).content)
	json.is_valid(after(policy_addresses[key]).content)
}

statements(key) := lib.statements(content(key))

# Statement の無い文書でも件数を数えられる形。statements(key) は Statement が無いと未定義になり、規則ごと黙る。
statement_count(key) := count(lib.as_array(object.get(content(key), "Statement", [])))

policy_types := {"SERVICE_CONTROL_POLICY", "RESOURCE_CONTROL_POLICY", "DECLARATIVE_POLICY_EC2"}

attachment_changes contains c if {
	some c in lib.changes
	c.rc.type == "aws_organizations_policy_attachment"
}

attachments contains {"address": c.rc.address, "policy": policy_ids[pid], "target": object.get(target_names, tid, tid)} if {
	some c in attachment_changes
	pid := c.rc.change.after.policy_id
	tid := c.rc.change.after.target_id
}

attached_targets(key) := {a.target | some a in attachments; a.policy == key}

break_glass_pattern := sprintf("arn:aws:iam::*:role%s%s", [role.path, role.name]) if {
	role := after(object.get(break_glass, "role", ""))
}

prod_path := sprintf("%s/%s/%s/%s/", [organization.id, root_id, after(ou_addresses.Workloads).id, after(ou_addresses.Prod).id])

deny contains msg if {
	not is_object(object.get(data.policy_plan, "organization_baseline", null))
	msg := "[PLAN-ORG-00] 宣言 data.policy_plan.organization_baseline がありません"
}

deny contains msg if {
	is_object(object.get(data.policy_plan, "organization_baseline", null))
	some key in required_policies
	not policy_addresses[key]
	msg := sprintf("[PLAN-ORG-00] 宣言の policies に %s がありません", [key])
}

deny contains msg if {
	is_object(object.get(data.policy_plan, "organization_baseline", null))
	some name in required_ous
	not ou_addresses[name]
	msg := sprintf("[PLAN-ORG-00] 宣言の organizational_units に %s がありません", [name])
}

deny contains msg if {
	is_object(object.get(data.policy_plan, "organization_baseline", null))
	some key in required_policies
	not attachment_declared(key)
	msg := sprintf("[PLAN-ORG-00] 宣言の attachments に %s の付け先の並びがありません", [key])
}

deny contains msg if {
	some key, address in policy_addresses
	after(address)
	not object.get(after(address), "type", null) in policy_types
	msg := sprintf("[PLAN-ORG-00] policy %s の種類が SCP・RCP・EC2 の declarative policy のどれでもないか、plan で決まりません", [key])
}

deny contains msg if {
	is_object(object.get(data.policy_plan, "organization_baseline", null))
	some address in array.concat(
		[object.get(ob, "organization", ""), object.get(break_glass, "role", "")],
		array.concat([a | some _, a in ou_addresses], [a | some _, a in policy_addresses]),
	)
	not after(address)
	msg := sprintf("[PLAN-ORG-00] 宣言した %s が入力にありません", [address])
}

deny contains msg if {
	organization
	not root_id_known
	msg := "[PLAN-ORG-00] 組織の Root の ID が plan で決まりません"
}

deny contains msg if {
	some key, _ in policy_addresses
	after(policy_addresses[key])
	not content(key)
	msg := sprintf("[PLAN-ORG-00] policy %s の文書が plan で決まらないか、JSON として読めません", [key])
}

deny contains msg if {
	some key in required_policies
	content(key)
	statement_count(key) == 0
	msg := sprintf("[PLAN-ORG-00] policy %s が文を持ちません", [key])
}

deny contains msg if {
	some key, targets in declared_targets
	got := attached_targets(key)
	got != {t | some t in targets}
	msg := sprintf("[PLAN-ORG-01] policy %s の付け先 %v が宣言 %v と違います", [key, got, targets])
}

deny contains msg if {
	some key, _ in declared_targets
	count([a | some a in attachments; a.policy == key]) != count(attached_targets(key))
	msg := sprintf("[PLAN-ORG-01] policy %s が同じ付け先に2回付いています", [key])
}

deny contains msg if {
	some a in attachments
	not a.policy in object.keys(declared_targets)
	msg := sprintf("[PLAN-ORG-01] %s の付け先が宣言にありません（policy %s）", [a.address, a.policy])
}

# 決まらない値は after に現れないので、文字列でないことで拾う（欠けた値も同じに扱う）。
deny contains msg if {
	some c in attachment_changes
	some attr in ["policy_id", "target_id"]
	not is_string(object.get(c.rc.change.after, attr, null))
	msg := sprintf("[PLAN-ORG-01] %s の %s が plan で決まらないか、ありません", [c.rc.address, attr])
}

deny contains msg if {
	some c in attachment_changes
	is_string(c.rc.change.after.policy_id)
	not policy_ids[c.rc.change.after.policy_id]
	msg := sprintf("[PLAN-ORG-01] %s が宣言の外の policy を付けます", [c.rc.address])
}

deny contains msg if {
	some a in attachments
	a.target == "Root"
	a.policy != "deny-leave-and-close"
	msg := sprintf("[PLAN-ORG-02] Root に %s が付いています。Root に付けてよいのは離脱と閉鎖の拒否だけです", [a.policy])
}

deny contains msg if {
	content("deny-leave-and-close")
	not leave_and_close_shape
	msg := "[PLAN-ORG-02] deny-leave-and-close が organizations:LeaveOrganization と account:CloseAccount を、Resource \"*\"・条件なしで拒んでいません"
}

leave_and_close_shape if {
	some s in statements("deny-leave-and-close")
	s.Effect == "Deny"
	{a | some a in lib.lower_all(s.Action)} == {"organizations:leaveorganization", "account:closeaccount"}
	s.Resource == "*"
	not s.Condition
}

deny contains msg if {
	content("deny-root-user")
	not root_user_shape
	msg := "[PLAN-ORG-03] deny-root-user が、aws:AssumedRoot の無い root の要求だけを拒む形（ArnLike aws:PrincipalArn = arn:aws:iam::*:root、Null aws:AssumedRoot = true）ではありません"
}

root_user_shape if {
	some s in statements("deny-root-user")
	s.Effect == "Deny"
	s.Condition == {"ArnLike": {"aws:PrincipalArn": "arn:aws:iam::*:root"}, "Null": {"aws:AssumedRoot": "true"}}
}

scp_keys := {key | some key, _ in policy_addresses; after(policy_addresses[key]).type == "SERVICE_CONTROL_POLICY"}

rcp_keys := {key | some key, _ in policy_addresses; after(policy_addresses[key]).type == "RESOURCE_CONTROL_POLICY"}

covers(patterns, action) if {
	some p in lib.lower_all(patterns)
	glob.match(p, [], action)
}

# NotAction の Deny は、拒む Action を列挙できず、どの規則の covers も読めない。
deny contains msg if {
	some key in (scp_keys | rcp_keys)
	some s in statements(key)
	s.Effect == "Deny"
	object.get(s, "NotAction", null) != null
	msg := sprintf("[PLAN-ORG-04] policy %s の Deny が NotAction を使っています。拒む Action を列挙してください", [key])
}

spares_root_session(s) if s.Condition.Null["aws:AssumedRoot"] == "true"

spares_root_session(s) if {
	values := lib.as_array(s.Condition.ArnLike["aws:PrincipalArn"])
	count(values) > 0
	every v in values {
		contains(v, ":role/")
	}
}

spares_root_session(s) if s.Condition["ForAnyValue:StringLike"]["aws:ResourceOrgPaths"] == sprintf("%s*", [prod_path])

deny contains msg if {
	some key in scp_keys
	some s in statements(key)
	s.Effect == "Deny"
	some action in task_actions
	covers(object.get(s, "Action", []), action)
	not spares_root_session(s)
	msg := sprintf("[PLAN-ORG-04] SCP %s が、sts:AssumeRoot の回復の Action %s を root のセッションに拒みます", [key, action])
}

deny contains msg if {
	count(task_actions) == 0
	msg := "[PLAN-ORG-04] 宣言 assume_root_task_actions が空です"
}

state_writer_pattern := `^arn:aws:iam::\*:role/[a-z0-9]+-(security|log-archive|sandbox|dev|stg|prd)-ci-(apply|apply-identity|unlock|unlock-identity)$`

state_write_exceptions := {v | some s in statements("deny-state-write"); some v in lib.as_array(s.Condition.ArnNotLike["aws:PrincipalArn"])}

deny contains msg if {
	content("deny-state-write")
	some v in state_write_exceptions
	v != break_glass_pattern
	not regex.match(state_writer_pattern, v)
	msg := sprintf("[PLAN-ORG-05] deny-state-write の例外 %s が、メンバーの env の CI の apply 系と lock の解除の role でも break-glass の role でもありません", [v])
}

deny contains msg if {
	content("deny-state-write")
	not break_glass_pattern in state_write_exceptions
	msg := "[PLAN-ORG-05] deny-state-write が break-glass の role を例外にしていません（回復を妨げる）"
}

deny contains msg if {
	some key in scp_keys
	key != "deny-state-write"
	some s in statements(key)
	some op, keys in object.get(s, "Condition", {})
	contains(lower(op), "not")
	some v in lib.as_array(object.get(keys, "aws:PrincipalArn", []))
	v == break_glass_pattern
	msg := sprintf("[PLAN-ORG-05] SCP %s が break-glass の role を例外にしています。例外は state の書き込みの拒否に限ります", [key])
}

deny contains msg if {
	content("protect-governance-roles")
	not governance_roles_shape
	msg := "[PLAN-ORG-06] protect-governance-roles が、StackSet の実行の role のほかを外さずに break-glass の role を守る形ではありません"
}

stacksets_exec_pattern := "arn:aws:iam::*:role/stacksets-exec-*"

governance_roles_shape if {
	some s in statements("protect-governance-roles")
	s.Effect == "Deny"
	s.Resource == break_glass_pattern
	not s.Condition
}

# 外せるのは StackSet の実行の role だけで、その名前の role はメンバーの主体に作らせない（organization-baseline/ADR-0202）。
governance_roles_shape if {
	some s in statements("protect-governance-roles")
	s.Effect == "Deny"
	s.Resource == break_glass_pattern
	s.Condition == {"ArnNotLike": {"aws:PrincipalArn": stacksets_exec_pattern}}
	stacksets_exec_protected
}

stacksets_exec_protected if {
	some s in statements("protect-governance-roles")
	s.Effect == "Deny"
	s.Resource == stacksets_exec_pattern
	not s.Condition
	every a in {"iam:create*", "iam:update*", "iam:put*", "iam:attach*", "iam:passrole"} {
		a in lib.lower_all(s.Action)
	}
}

deny contains msg if {
	content("data-perimeter-prod-resources")
	not prod_resources_shape
	msg := sprintf("[PLAN-ORG-07] data-perimeter-prod-resources が Prod の OU の path %s* の resource を拒んでいません", [prod_path])
}

prod_resources_shape if {
	some s in statements("data-perimeter-prod-resources")
	s.Effect == "Deny"
	s.Action == "*"
	s.Condition["ForAnyValue:StringLike"]["aws:ResourceOrgPaths"] == sprintf("%s*", [prod_path])
}

deny contains msg if {
	some s in statements("data-perimeter-prod-principals")
	not s.Condition["ForAllValues:StringNotLike"]["aws:PrincipalOrgPaths"] == sprintf("%s*", [prod_path])
	msg := sprintf("[PLAN-ORG-07] data-perimeter-prod-principals の文 %v が Prod の OU の path で限っていません", [object.get(s, "Sid", "")])
}

deny contains msg if {
	some s in statements("data-perimeter-org-principals")
	not s.Condition.StringNotEqualsIfExists["aws:PrincipalOrgID"] == organization.id
	msg := sprintf("[PLAN-ORG-07] data-perimeter-org-principals の文 %v が組織の ID で限っていません", [object.get(s, "Sid", "")])
}

deny contains msg if {
	some key in rcp_keys
	some s in statements(key)
	not s.Condition.BoolIfExists["aws:PrincipalIsAWSService"] == "false"
	msg := sprintf("[PLAN-ORG-07] RCP %s の文 %v が AWS のサービスの主体を外していません", [key, object.get(s, "Sid", "")])
}

# GitHub の OIDC と Identity Center の到達がこの2つの引き受けを使う。
deny contains msg if {
	some key in rcp_keys
	some s in statements(key)
	some action in {"sts:assumerolewithwebidentity", "sts:assumerolewithsaml"}
	covers(object.get(s, "Action", []), action)
	msg := sprintf("[PLAN-ORG-07] RCP %s が %s を拒みます", [key, action])
}

# 他の Action まで拒むと、sandbox の掃除を妨げる。
deny contains msg if {
	some s in statements("deny-guardduty-stop")
	some a in lib.lower_all(object.get(s, "Action", []))
	not startswith(a, "guardduty:")
	msg := sprintf("[PLAN-ORG-08] deny-guardduty-stop が GuardDuty の外の Action %s を拒みます", [a])
}

deny contains msg if {
	content("deny-guardduty-stop")
	not guardduty_stop_shape
	msg := "[PLAN-ORG-08] deny-guardduty-stop が guardduty:DeleteDetector を拒んでいません"
}

guardduty_stop_shape if {
	some s in statements("deny-guardduty-stop")
	s.Effect == "Deny"
	"guardduty:deletedetector" in lib.lower_all(object.get(s, "Action", []))
}
