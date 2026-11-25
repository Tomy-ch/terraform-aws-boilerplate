# break-glass の形（ADR-0301 決定19・20、organization-baseline/ADR-0201。conftest/plan/README.md の
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

user_arns := {u.arn | some u in users; user_prefix(u)}

# break-glass の user が自分を指す Resource。group の文は2つの user の両方に掛かるので、2つの ARN が同じ1つの接頭辞を持つときだけ定まる。
own_user_resource := concat("", [prefix, "${aws:username}"]) if {
	declared := {a | some a in object.get(bg, "users", [])}
	prefixes := [user_prefix(after(a)) | some a in declared]
	count(prefixes) == 2
	distinct := {p | some p in prefixes}
	count(distinct) == 1
	some prefix in distinct
}

user_prefix(u) := prefix if {
	is_string(u.name)
	u.name != ""
	lib.fixed_value(u.arn)
	endswith(u.arn, concat("", ["/", u.name]))
	prefix := substring(u.arn, 0, count(u.arn) - count(u.name))
	regex.match(`^arn:[^:]+:iam::[0-9]{12}:user/([^/]+/)*$`, prefix)
}

own_resource(s) if {
	{r | some r in lib.as_array(s.Resource)} == {own_user_resource}
}

role := after(object.get(bg, "role", ""))

role_pattern := sprintf("arn:aws:iam::*:role%s%s", [role.path, role.name])

body(address, attr) := json.unmarshal(after(address)[attr]) if {
	is_string(after(address)[attr])
	json.is_valid(after(address)[attr])
}

group_statements := lib.statements(body(object.get(bg, "group_policy", ""), "policy"))

trust_statements := lib.statements(body(object.get(bg, "role", ""), "assume_role_policy"))

self_management := {"iam:changepassword", "iam:enablemfadevice", "iam:getuser", "iam:listmfadevices", "iam:resyncmfadevice"}

# stack instance は、それを作る権限を先に昇格する間だけ宣言から外せる（docs/setup/organization-baseline.md）。
declared_instances := [bg.stack_instances] if "stack_instances" in object.keys(bg)

else := []

deny contains msg if {
	some a in array.concat(object.get(bg, "users", []), array.concat([object.get(bg, "role", ""), object.get(bg, "group_policy", ""), object.get(bg, "role_attachments", ""), object.get(bg, "role_policy_attachment", ""), object.get(bg, "stack_set", "")], declared_instances))
	not after(a)
	msg := sprintf("[PLAN-BG-00] 宣言した break-glass の %s が入力にありません", [a])
}

# 宣言から外した間に stack instance が plan に現れたら、PLAN-BG-05 が見ないまま通さない。
deny contains msg if {
	count(bg) > 0
	count(declared_instances) == 0
	some c in lib.changes
	c.rc.type == "aws_cloudformation_stack_instances"
	msg := sprintf("[PLAN-BG-00] break-glass の stack instance の宣言が無いのに、%s が入力にあります", [c.rc.address])
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
	some a in object.get(bg, "users", [])
	u := after(a)
	not user_prefix(u)
	msg := sprintf("[PLAN-BG-01] break-glass の user %s が、固定の形の ARN と名前を持ちません", [a])
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
	count(lib.lower_all(s.Action)) > 0
	every a in lib.lower_all(s.Action) {
		a in self_management
	}
	own_resource(s)
}

allowed_group_statement(s) if lib.lower_all(s.Action) == ["iam:getaccountpasswordpolicy"]

allowed_group_statement(s) if {
	lib.lower_all(s.Action) == ["iam:deactivatemfadevice"]
	own_resource(s)
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

long_term_credential_actions := ["iam:createaccesskey", "iam:createservicespecificcredential", "iam:uploadsshpublickey", "iam:uploadsigningcertificate"]

deny contains msg if {
	body(object.get(bg, "group_policy", ""), "policy")
	some a in long_term_credential_actions
	not denies_unconditionally(a)
	msg := sprintf("[PLAN-BG-02] break-glass の group が %s を、条件なしで全 Resource について拒んでいません", [a])
}

denies_unconditionally(action) if {
	some s in group_statements
	s.Effect == "Deny"
	not "Condition" in object.keys(s)
	{r | some r in lib.as_array(object.get(s, "Resource", []))} == {"*"}
	some pattern in lib.lower_all(s.Action)
	lib.iam_match(pattern, action)
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

break_glass_attachment(rc) if {
	rc.address == bg.role_policy_attachment
	rc.change.after.role == role.name
}

deny contains msg if {
	attachment := after(object.get(bg, "role_attachments", ""))
	{a | some a in attachment.policy_arns} != {administrator_access}
	msg := "[PLAN-BG-04] break-glass の role に付ける managed policy が AdministratorAccess だけではありません"
}

deny contains msg if {
	attachment := after(object.get(bg, "role_policy_attachment", ""))
	object.get(attachment, "policy_arn", null) != administrator_access
	msg := "[PLAN-BG-04] break-glass の role の attachment が付ける policy が AdministratorAccess ではありません"
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

# CloudFormation は ASCII の外の文字を「?」に置き換えて保存する。HCL と実物が合わず、plan が毎回 UpdateStackSet を出す。
deny contains msg if {
	stack_set
	description := object.get(stack_set, "description", null)
	description != null
	not printable_ascii(description)
	msg := "[PLAN-BG-05] メンバーの break-glass の StackSet の description が、印字できる ASCII の外の文字を含みます"
}

printable_ascii(s) if {
	is_string(s)
	regex.match(`^[\x20-\x7E]*$`, s)
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
