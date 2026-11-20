package plan.organization_baseline_break_glass_test

import data.plan.fixture
import data.plan.organization_baseline_break_glass as p

decl := {"break_glass": {
	"users": ["module.b.aws_iam_user.this[\"break-glass-1\"]", "module.b.aws_iam_user.this[\"break-glass-2\"]"],
	"group_policy": "module.b.aws_iam_group_policy.this",
	"role": "module.b.aws_iam_role.this",
	"role_attachments": "module.b.aws_iam_role_policy_attachments_exclusive.this",
}}

user_arns := ["arn:aws:iam::111111111111:user/break-glass/break-glass-1", "arn:aws:iam::111111111111:user/break-glass/break-glass-2"]

mfa := {"Bool": {"aws:MultiFactorAuthPresent": "true"}}

change(address, type, after) := {"address": address, "type": type, "change": {"actions": ["create"], "after": after, "after_unknown": {}}}

user(n, arn) := change(sprintf("module.b.aws_iam_user.this[\"break-glass-%d\"]", [n]), "aws_iam_user", {"name": sprintf("break-glass-%d", [n]), "arn": arn})

assume := {"Sid": "AssumeBreakGlassRole", "Effect": "Allow", "Action": "sts:AssumeRole", "Resource": "arn:aws:iam::*:role/break-glass/tabp-break-glass", "Condition": mfa}

self_manage := {"Effect": "Allow", "Action": ["iam:ChangePassword", "iam:EnableMFADevice"], "Resource": "arn:aws:iam::111111111111:user/break-glass/${aws:username}"}

password_policy := {"Effect": "Allow", "Action": "iam:GetAccountPasswordPolicy", "Resource": "*"}

long_term_credentials_deny := {"Effect": "Deny", "Action": ["iam:CreateAccessKey"], "Resource": "*"}

deactivate := {"Effect": "Allow", "Action": "iam:DeactivateMFADevice", "Resource": "arn:aws:iam::111111111111:user/break-glass/${aws:username}", "Condition": mfa}

valid_group := [assume, self_manage, deactivate, password_policy, long_term_credentials_deny]

trust(principals) := [{"Effect": "Allow", "Principal": {"AWS": principals}, "Action": "sts:AssumeRole", "Condition": mfa}]

role(statements) := change("module.b.aws_iam_role.this", "aws_iam_role", {"name": "tabp-break-glass", "path": "/break-glass/", "assume_role_policy": json.marshal({"Statement": statements})})

group_policy(statements) := change("module.b.aws_iam_group_policy.this", "aws_iam_group_policy", {"policy": json.marshal({"Statement": statements})})

attachments(arns) := change("module.b.aws_iam_role_policy_attachments_exclusive.this", "aws_iam_role_policy_attachments_exclusive", {"role_name": "tabp-break-glass", "policy_arns": arns})

admin := "arn:aws:iam::aws:policy/AdministratorAccess"

rcs(group, trusts, arns) := [user(1, user_arns[0]), user(2, user_arns[1]), group_policy(group), role(trusts), attachments(arns)]

valid := fixture.plan(rcs(valid_group, trust(user_arns), [admin]))

test_pass_reference_shape if {
	count(p.deny) == 0 with input as valid with data.policy_plan as {"organization_baseline": decl}
}

test_violation_declared_resource_missing if {
	inp := fixture.plan([user(1, user_arns[0]), user(2, user_arns[1]), role(trust(user_arns)), attachments([admin])])
	fixture.has("[PLAN-BG-00]", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_three_users if {
	d := {"break_glass": object.union(decl.break_glass, {"users": array.concat(decl.break_glass.users, ["module.b.aws_iam_user.this[\"break-glass-3\"]"])})}
	inp := fixture.plan(array.concat(rcs(valid_group, trust(user_arns), [admin]), [user(3, "arn:aws:iam::111111111111:user/break-glass/break-glass-3")]))
	fixture.has("[PLAN-BG-01]", p.deny) with input as inp with data.policy_plan as {"organization_baseline": d}
}

test_violation_access_key if {
	key := change("module.b.aws_iam_access_key.this", "aws_iam_access_key", {"user": "break-glass-1"})
	inp := fixture.plan(array.concat(rcs(valid_group, trust(user_arns), [admin]), [key]))
	fixture.has("[PLAN-BG-01]", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_group_allows_more if {
	extra := {"Effect": "Allow", "Action": "iam:CreateRole", "Resource": "*"}
	inp := fixture.plan(rcs(array.concat(valid_group, [extra]), trust(user_arns), [admin]))
	fixture.has("[PLAN-BG-02]", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_assume_without_mfa if {
	no_mfa := object.remove(assume, ["Condition"])
	inp := fixture.plan(rcs([no_mfa, self_manage, password_policy, long_term_credentials_deny], trust(user_arns), [admin]))
	fixture.has("[PLAN-BG-02]", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_deactivate_mfa_without_mfa if {
	no_mfa := object.remove(deactivate, ["Condition"])
	inp := fixture.plan(rcs([assume, self_manage, no_mfa, password_policy, long_term_credentials_deny], trust(user_arns), [admin]))
	fixture.has("[PLAN-BG-02]", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_access_key_not_denied if {
	inp := fixture.plan(rcs([assume, self_manage, password_policy], trust(user_arns), [admin]))
	fixture.has("[PLAN-BG-02] break-glass の group が iam:CreateAccessKey を拒んでいません", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_trust_other_principal if {
	inp := fixture.plan(rcs(valid_group, trust(["arn:aws:iam::111111111111:root"]), [admin]))
	fixture.has("[PLAN-BG-03]", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_trust_without_mfa if {
	trusts := [{"Effect": "Allow", "Principal": {"AWS": user_arns}, "Action": "sts:AssumeRole"}]
	inp := fixture.plan(rcs(valid_group, trusts, [admin]))
	fixture.has("[PLAN-BG-03]", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_admin_elsewhere if {
	other := change("module.x.aws_iam_role_policy_attachments_exclusive.other", "aws_iam_role_policy_attachments_exclusive", {"role_name": "other", "policy_arns": [admin]})
	inp := fixture.plan(array.concat(rcs(valid_group, trust(user_arns), [admin]), [other]))
	fixture.has("[PLAN-BG-04]", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_break_glass_extra_managed_policy if {
	inp := fixture.plan(rcs(valid_group, trust(user_arns), [admin, "arn:aws:iam::aws:policy/ReadOnlyAccess"]))
	fixture.has("[PLAN-BG-04]", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_group_policy_without_statement if {
	inp := fixture.plan([user(1, user_arns[0]), user(2, user_arns[1]), change("module.b.aws_iam_group_policy.this", "aws_iam_group_policy", {"policy": "{}"}), role(trust(user_arns)), attachments([admin])])
	fixture.has("[PLAN-BG-02] break-glass の group の policy が文を持ちません", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_group_allow_not_action if {
	wide := {"Effect": "Allow", "NotAction": "iam:CreateUser", "Resource": "*"}
	inp := fixture.plan(rcs(array.concat(valid_group, [wide]), trust(user_arns), [admin]))
	fixture.has("[PLAN-BG-02] break-glass の group の Allow が NotAction", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_self_manage_other_users if {
	other := {"Effect": "Allow", "Action": ["iam:ChangePassword"], "Resource": "*"}
	inp := fixture.plan(rcs([assume, other, deactivate, password_policy, long_term_credentials_deny], trust(user_arns), [admin]))
	fixture.has("[PLAN-BG-02] break-glass の group の Allow ", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_deactivate_other_users_mfa if {
	other := object.union(deactivate, {"Resource": "*"})
	inp := fixture.plan(rcs([assume, self_manage, other, password_policy, long_term_credentials_deny], trust(user_arns), [admin]))
	fixture.has("[PLAN-BG-02] break-glass の group の Allow ", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_assume_any_role if {
	any_role := object.union(assume, {"Resource": "*"})
	inp := fixture.plan(rcs([any_role, self_manage, deactivate, password_policy, long_term_credentials_deny], trust(user_arns), [admin]))
	fixture.has("[PLAN-BG-02] break-glass の group の Allow AssumeBreakGlassRole", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_trust_without_allow if {
	inp := fixture.plan(rcs(valid_group, [], [admin]))
	fixture.has("[PLAN-BG-03] break-glass の role の信頼条件に Allow の文がありません", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_login_profile if {
	profile := change("module.b.aws_iam_user_login_profile.this", "aws_iam_user_login_profile", {"user": "break-glass-1"})
	inp := fixture.plan(array.concat(rcs(valid_group, trust(user_arns), [admin]), [profile]))
	fixture.has("[PLAN-BG-01] module.b.aws_iam_user_login_profile.this が長期の資格情報を作ります", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_virtual_mfa if {
	mfa_device := change("module.b.aws_iam_virtual_mfa_device.this", "aws_iam_virtual_mfa_device", {"virtual_mfa_device_name": "x"})
	inp := fixture.plan(array.concat(rcs(valid_group, trust(user_arns), [admin]), [mfa_device]))
	fixture.has("[PLAN-BG-01] module.b.aws_iam_virtual_mfa_device.this が長期の資格情報を作ります", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_admin_to_user if {
	direct := change("module.x.aws_iam_user_policy_attachment.admin", "aws_iam_user_policy_attachment", {"user": "someone", "policy_arn": admin})
	inp := fixture.plan(array.concat(rcs(valid_group, trust(user_arns), [admin]), [direct]))
	fixture.has("[PLAN-BG-04] module.x.aws_iam_user_policy_attachment.admin が AdministratorAccess", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_declared_attachment_for_other_role if {
	other := change("module.b.aws_iam_role_policy_attachments_exclusive.this", "aws_iam_role_policy_attachments_exclusive", {"role_name": "other", "policy_arns": [admin]})
	inp := fixture.plan([user(1, user_arns[0]), user(2, user_arns[1]), group_policy(valid_group), role(trust(user_arns)), other])
	fixture.has("[PLAN-BG-04] module.b.aws_iam_role_policy_attachments_exclusive.this が AdministratorAccess", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

own := "arn:aws:iam::111111111111:user/break-glass/${aws:username}"

manage_own(resource) := object.union(self_manage, {"Sid": "ManageOwn", "Resource": resource})

deactivate_own(resource) := object.union(deactivate, {"Sid": "DeactivateOwnMfa", "Resource": resource})

manage_own_denied := "[PLAN-BG-02] break-glass の group の Allow ManageOwn が、role の引き受けと自分の資格情報の管理の外です"

with_changes(group, replacements) := [object.get(replacements, r.address, r) | some r in rcs(group, trust(user_arns), [admin])]

users_as(after1, after2) := {
	"module.b.aws_iam_user.this[\"break-glass-1\"]": change("module.b.aws_iam_user.this[\"break-glass-1\"]", "aws_iam_user", after1),
	"module.b.aws_iam_user.this[\"break-glass-2\"]": change("module.b.aws_iam_user.this[\"break-glass-2\"]", "aws_iam_user", after2),
}

manage_own_group(resource) := [assume, manage_own(resource), deactivate, password_policy, long_term_credentials_deny]

test_violation_self_manage_resource_not_own if {
	every resource in [
		"arn:aws:iam::111111111111:user/*${aws:username}",
		"arn:aws:iam::111111111111:user/admins/${aws:username}",
		"arn:aws:iam::*:user/break-glass/${aws:username}",
		[own, "arn:aws:iam::111111111111:user/*${aws:username}"],
		[],
	] {
		fixture.has(manage_own_denied, p.deny) with input as fixture.plan(with_changes(manage_own_group(resource), {})) with data.policy_plan as {"organization_baseline": decl}
	}
}

test_violation_deactivate_wildcard_before_username if {
	group := [assume, self_manage, deactivate_own("arn:aws:iam::111111111111:user/*${aws:username}"), password_policy, long_term_credentials_deny]
	fixture.has("[PLAN-BG-02] break-glass の group の Allow DeactivateOwnMfa が、", p.deny) with input as fixture.plan(with_changes(group, {})) with data.policy_plan as {"organization_baseline": decl}
}

test_violation_own_resource_not_derivable if {
	every users in [
		# ARN が無い。
		users_as({"name": "break-glass-1"}, {"name": "break-glass-2", "arn": user_arns[1]}),
		# 2人の path が違う。
		users_as({"name": "break-glass-1", "arn": user_arns[0]}, {"name": "break-glass-2", "arn": "arn:aws:iam::111111111111:user/other/break-glass-2"}),
		# ARN が自分の名前で終わらない。
		users_as({"name": "break-glass-1", "arn": user_arns[1]}, {"name": "break-glass-2", "arn": user_arns[0]}),
		# 名前が空。
		users_as({"name": "", "arn": "arn:aws:iam::111111111111:user/break-glass/"}, {"name": "", "arn": "arn:aws:iam::111111111111:user/break-glass/"}),
	] {
		fixture.has(manage_own_denied, p.deny) with input as fixture.plan(with_changes(manage_own_group(own), users)) with data.policy_plan as {"organization_baseline": decl}
	}
}

test_violation_own_resource_from_unfixed_arn if {
	every path in ["/break*/", "/${aws:PrincipalTag/p}/"] {
		users := users_as(
			{"name": "break-glass-1", "arn": sprintf("arn:aws:iam::111111111111:user%sbreak-glass-1", [path])},
			{"name": "break-glass-2", "arn": sprintf("arn:aws:iam::111111111111:user%sbreak-glass-2", [path])},
		)
		resource := sprintf("arn:aws:iam::111111111111:user%s${aws:username}", [path])
		fixture.has(manage_own_denied, p.deny) with input as fixture.plan(with_changes(manage_own_group(resource), users)) with data.policy_plan as {"organization_baseline": decl}
	}
}

test_violation_own_resource_from_malformed_arn if {
	users := users_as({"name": "break-glass-1", "arn": "x/break-glass-1"}, {"name": "break-glass-2", "arn": "x/break-glass-2"})
	fixture.has(manage_own_denied, p.deny) with input as fixture.plan(with_changes(manage_own_group("x/${aws:username}"), users)) with data.policy_plan as {"organization_baseline": decl}
}

test_pass_own_resource_as_array if {
	group := [assume, manage_own([own]), deactivate_own([own]), password_policy, long_term_credentials_deny]
	count(p.deny) == 0 with input as fixture.plan(with_changes(group, {})) with data.policy_plan as {"organization_baseline": decl}
}
