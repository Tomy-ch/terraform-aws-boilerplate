# break-glass の形（ADR-0301 決定19 の検証方法、organization-baseline/ADR-0201。conftest/plan/README.md の
# plan.organization_baseline_break_glass）。
package plan.organization_baseline_break_glass

import data.plan.lib

bg := object.get(object.get(data.policy_plan, "organization_baseline", {}), "break_glass", {})

administrator_access := "arn:aws:iam::aws:policy/AdministratorAccess"

after(address) := c.rc.change.after if {
	some c in lib.changes
	c.rc.address == address
}

users := {after(a) | some a in object.get(bg, "users", [])}

user_arns := {u.arn | some u in users}

role := after(object.get(bg, "role", ""))

role_pattern := sprintf("arn:aws:iam::*:role%s%s", [role.path, role.name])

body(address, attr) := json.unmarshal(after(address)[attr]) if {
	is_string(after(address)[attr])
	json.is_valid(after(address)[attr])
}

group_statements := lib.statements(body(object.get(bg, "group_policy", ""), "policy"))

trust_statements := lib.statements(body(object.get(bg, "role", ""), "assume_role_policy"))

self_management := {"iam:changepassword", "iam:enablemfadevice", "iam:getuser", "iam:listmfadevices", "iam:resyncmfadevice"}

deny contains msg if {
	some a in array.concat(object.get(bg, "users", []), [object.get(bg, "role", ""), object.get(bg, "group_policy", ""), object.get(bg, "role_attachments", ""), object.get(bg, "stack_set", ""), object.get(bg, "stack_instances", "")])
	not after(a)
	msg := sprintf("[PLAN-BG-00] 宣言した break-glass の %s が入力にありません", [a])
}

deny contains msg if {
	count(object.get(bg, "users", [])) != 2
	msg := "[PLAN-BG-01] break-glass の IAM user の宣言がちょうど2つではありません"
}

deny contains msg if {
	some c in lib.changes
	c.rc.type in {"aws_iam_access_key", "aws_iam_user_login_profile", "aws_iam_virtual_mfa_device"}
	msg := sprintf("[PLAN-BG-01] %s が長期の資格情報を作ります", [c.rc.address])
}

deny contains msg if {
	some s in group_statements
	s.Effect == "Allow"
	not allowed_group_statement(s)
	msg := sprintf("[PLAN-BG-02] break-glass の group の Allow %v が、role の引き受けと自分の資格情報の管理の外です", [object.get(s, "Sid", "")])
}

allowed_group_statement(s) if {
	lib.lower_all(s.Action) == ["sts:assumerole"]
	s.Resource == role_pattern
	s.Condition == {"Bool": {"aws:MultiFactorAuthPresent": "true"}}
}

allowed_group_statement(s) if {
	every a in lib.lower_all(s.Action) {
		a in self_management
	}
	every r in lib.as_array(s.Resource) {
		endswith(r, "${aws:username}")
	}
}

allowed_group_statement(s) if lib.lower_all(s.Action) == ["iam:getaccountpasswordpolicy"]

# 自分の MFA を外すのは、MFA で認証したセッションに限る。
allowed_group_statement(s) if {
	lib.lower_all(s.Action) == ["iam:deactivatemfadevice"]
	endswith(s.Resource, "${aws:username}")
	s.Condition == {"Bool": {"aws:MultiFactorAuthPresent": "true"}}
}

deny contains msg if {
	body(object.get(bg, "group_policy", ""), "policy")
	count(lib.as_array(object.get(body(object.get(bg, "group_policy", ""), "policy"), "Statement", []))) == 0
	msg := "[PLAN-BG-02] break-glass の group の policy が文を持ちません"
}

deny contains msg if {
	some s in group_statements
	s.Effect == "Allow"
	object.get(s, "NotAction", null) != null
	msg := "[PLAN-BG-02] break-glass の group の Allow が NotAction を使っています"
}

deny contains msg if {
	body(object.get(bg, "group_policy", ""), "policy")
	not denies_access_keys
	msg := "[PLAN-BG-02] break-glass の group が iam:CreateAccessKey を拒んでいません"
}

denies_access_keys if {
	some s in group_statements
	s.Effect == "Deny"
	"iam:createaccesskey" in lib.lower_all(s.Action)
	s.Resource == "*"
}

deny contains msg if {
	some s in trust_statements
	s.Effect == "Allow"
	not trust_shape(s)
	msg := "[PLAN-BG-03] break-glass の role の信頼条件が、break-glass の user と aws:MultiFactorAuthPresent に限っていません"
}

deny contains msg if {
	body(object.get(bg, "role", ""), "assume_role_policy")
	count([s | some s in trust_statements; s.Effect == "Allow"]) == 0
	msg := "[PLAN-BG-03] break-glass の role の信頼条件に Allow の文がありません"
}

trust_shape(s) if {
	{p | some p in lib.as_array(s.Principal.AWS)} == user_arns
	count(user_arns) == 2
	lib.lower_all(s.Action) == ["sts:assumerole"]
	s.Condition == {"Bool": {"aws:MultiFactorAuthPresent": "true"}}
}

deny contains msg if {
	some c in lib.changes
	c.rc.type in {"aws_iam_role_policy_attachments_exclusive", "aws_iam_role_policy_attachment", "aws_iam_policy_attachment", "aws_iam_group_policy_attachment", "aws_iam_user_policy_attachment"}
	administrator_access in admin_arns(c.rc)
	not break_glass_attachment(c.rc)
	msg := sprintf("[PLAN-BG-04] %s が AdministratorAccess を break-glass の role の外へ付けます", [c.rc.address])
}

admin_arns(rc) := {a | some a in lib.as_array(object.get(rc.change.after, "policy_arns", []))} | {object.get(rc.change.after, "policy_arn", "")}

break_glass_attachment(rc) if {
	rc.address == bg.role_attachments
	rc.change.after.role_name == role.name
}

deny contains msg if {
	attachment := after(object.get(bg, "role_attachments", ""))
	{a | some a in attachment.policy_arns} != {administrator_access}
	msg := "[PLAN-BG-04] break-glass の role に付ける managed policy が AdministratorAccess だけではありません"
}

# メンバーの break-glass の role（organization-baseline/ADR-0202）。
stack_set := after(object.get(bg, "stack_set", ""))

template := body(object.get(bg, "stack_set", ""), "template_body")

member_role_keys := {"RoleName", "Path", "MaxSessionDuration", "ManagedPolicyArns", "AssumeRolePolicyDocument"}

deny contains msg if {
	stack_set
	not service_managed_auto(stack_set)
	msg := "[PLAN-BG-05] メンバーの break-glass の StackSet が、service-managed で新しいアカウントへ自動で置く形ではありません"
}

service_managed_auto(s) if {
	s.permission_model == "SERVICE_MANAGED"
	s.auto_deployment[0].enabled == true
}

deny contains msg if {
	stack_set
	not member_role_shape
	msg := "[PLAN-BG-05] メンバーの break-glass の StackSet のテンプレートが、管理アカウントの break-glass の role と同じ1つの role ではありません"
}

member_role_shape if {
	object.keys(template) == {"AWSTemplateFormatVersion", "Resources"}
	count(template.Resources) == 1
	some r in template.Resources
	object.keys(r) == {"Type", "Properties"}
	r.Type == "AWS::IAM::Role"
	object.keys(r.Properties) == member_role_keys
	r.Properties.RoleName == role.name
	r.Properties.Path == role.path
	r.Properties.MaxSessionDuration == role.max_session_duration
	r.Properties.ManagedPolicyArns == [administrator_access]
	statements := lib.statements(r.Properties.AssumeRolePolicyDocument)
	count([s | some s in statements; s.Effect == "Allow"]) > 0
	every s in statements {
		s.Effect == "Allow"
		trust_shape(s)
	}
}

deny contains msg if {
	stack_set
	instances := after(object.get(bg, "stack_instances", ""))
	not instances.stack_set_name == stack_set.name
	msg := "[PLAN-BG-05] メンバーの break-glass の stack instance が、break-glass の StackSet を名指していません"
}

# 展開先は組織の Root だけ。メンバーのすべて（これから作るものを含む）に置く。
organization_root_id := after(object.get(object.get(data.policy_plan, "organization_baseline", {}), "organization", "")).roots[0].id

deny contains msg if {
	stack_set
	instances := after(object.get(bg, "stack_instances", ""))
	not targets_root_only(instances)
	msg := "[PLAN-BG-05] メンバーの break-glass の stack instance の展開先が、組織の Root だけではありません"
}

targets_root_only(instances) if {
	count(instances.deployment_targets) == 1
	target := instances.deployment_targets[0]
	target.organizational_unit_ids == [organization_root_id]
	no_accounts(target)
}

no_accounts(target) if object.get(target, "accounts", null) == null

no_accounts(target) if count(target.accounts) == 0
