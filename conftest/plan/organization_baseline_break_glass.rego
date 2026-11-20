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

# break-glass の user が自分を指す Resource。group の文は2つの user の両方に掛かるので、2つの ARN が同じ1つの接頭辞を持つときだけ定まる。
own_user_resource := concat("", [prefix, "${aws:username}"]) if {
	prefixes := [user_prefix(after(a)) | some a in object.get(bg, "users", [])]
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

deny contains msg if {
	some a in array.concat(object.get(bg, "users", []), [object.get(bg, "role", ""), object.get(bg, "group_policy", ""), object.get(bg, "role_attachments", "")])
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
	own_resource(s)
}

allowed_group_statement(s) if lib.lower_all(s.Action) == ["iam:getaccountpasswordpolicy"]

# 自分の MFA を外すのは、MFA で認証したセッションに限る。
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
