package plan.organization_baseline_break_glass_test

import data.plan.fixture
import data.plan.organization_baseline_break_glass as p

decl := {"organization": "module.o.aws_organizations_organization.this", "break_glass": {
	"users": ["module.b.aws_iam_user.this[\"break-glass-1\"]", "module.b.aws_iam_user.this[\"break-glass-2\"]"],
	"group_policy": "module.b.aws_iam_group_policy.this",
	"role": "module.b.aws_iam_role.this",
	"role_attachments": "module.b.aws_iam_role_policy_attachments_exclusive.this",
	"role_policy_attachment": "module.b.aws_iam_role_policy_attachment.administrator_access",
	"stack_set": "module.b.aws_cloudformation_stack_set.this",
	"stack_instances": "module.b.aws_cloudformation_stack_instances.this",
}}

user_arns := ["arn:aws:iam::111111111111:user/break-glass/break-glass-1", "arn:aws:iam::111111111111:user/break-glass/break-glass-2"]

mfa := {"Bool": {"aws:MultiFactorAuthPresent": "true"}}

change(address, type, after) := {"address": address, "type": type, "change": {"actions": ["create"], "after": after, "after_unknown": {}}}

user(n, arn) := change(sprintf("module.b.aws_iam_user.this[\"break-glass-%d\"]", [n]), "aws_iam_user", {"name": sprintf("break-glass-%d", [n]), "arn": arn})

assume := {"Sid": "AssumeBreakGlassRole", "Effect": "Allow", "Action": "sts:AssumeRole", "Resource": "arn:aws:iam::*:role/break-glass/tabp-break-glass", "Condition": mfa}

self_manage := {"Effect": "Allow", "Action": ["iam:ChangePassword", "iam:EnableMFADevice"], "Resource": "arn:aws:iam::111111111111:user/break-glass/${aws:username}"}

password_policy := {"Effect": "Allow", "Action": "iam:GetAccountPasswordPolicy", "Resource": "*"}

long_term_credential_actions := ["iam:CreateAccessKey", "iam:CreateServiceSpecificCredential", "iam:UploadSSHPublicKey", "iam:UploadSigningCertificate"]

long_term_credentials_deny := {"Effect": "Deny", "Action": long_term_credential_actions, "Resource": "*"}

deactivate := {"Effect": "Allow", "Action": "iam:DeactivateMFADevice", "Resource": "arn:aws:iam::111111111111:user/break-glass/${aws:username}", "Condition": mfa}

login_client := "arn:aws:signin:ap-northeast-1:111111111111:oauth2/public-client/localhost"

login_authorize := {"Sid": "AwsLoginAuthorize", "Effect": "Allow", "Action": "signin:AuthorizeOAuth2Access", "Resource": login_client}

login_token := {"Sid": "AwsLoginToken", "Effect": "Allow", "Action": "signin:CreateOAuth2Token", "Resource": login_client, "Condition": {"StringEquals": {"signin:OAuthGrantType": ["authorization_code", "refresh_token"]}}}

valid_group := [assume, self_manage, deactivate, password_policy, login_authorize, login_token, long_term_credentials_deny]

trust(principals) := [{"Effect": "Allow", "Principal": {"AWS": principals}, "Action": "sts:AssumeRole", "Condition": mfa}]

role(statements) := change("module.b.aws_iam_role.this", "aws_iam_role", {"name": "tabp-break-glass", "path": "/break-glass/", "max_session_duration": 3600, "assume_role_policy": json.marshal({"Statement": statements})})

organization := change("module.o.aws_organizations_organization.this", "aws_organizations_organization", {"roots": [{"id": "r-exam"}]})

group_policy(statements) := change("module.b.aws_iam_group_policy.this", "aws_iam_group_policy", {"policy": json.marshal({"Statement": statements})})

attachments(arns) := change("module.b.aws_iam_role_policy_attachments_exclusive.this", "aws_iam_role_policy_attachments_exclusive", {"role_name": "tabp-break-glass", "policy_arns": arns})

admin := "arn:aws:iam::aws:policy/AdministratorAccess"

attachment(arn) := change("module.b.aws_iam_role_policy_attachment.administrator_access", "aws_iam_role_policy_attachment", {"role": "tabp-break-glass", "policy_arn": arn})

member_properties := {
	"RoleName": "tabp-break-glass",
	"Path": "/break-glass/",
	"MaxSessionDuration": 3600,
	"ManagedPolicyArns": [admin],
	"AssumeRolePolicyDocument": {"Version": "2012-10-17", "Statement": trust(user_arns)},
}

member_template(resources) := json.marshal({"AWSTemplateFormatVersion": "2010-09-09", "Resources": resources})

stack_set(model, auto, resources) := change("module.b.aws_cloudformation_stack_set.this", "aws_cloudformation_stack_set", {
	"name": "tabp-break-glass",
	"description": "Break-glass role for member accounts",
	"permission_model": model,
	"auto_deployment": [{"enabled": auto, "retain_stacks_on_account_removal": false}],
	"template_body": member_template(resources),
})

instances_with(after) := change("module.b.aws_cloudformation_stack_instances.this", "aws_cloudformation_stack_instances", after)

instances(name) := instances_with({"stack_set_name": name, "deployment_targets": [{"organizational_unit_ids": ["r-exam"], "accounts": null}]})

valid_member := {"BreakGlassRole": {"Type": "AWS::IAM::Role", "Properties": member_properties}}

member_rcs_with(set, inst) := [organization, user(1, user_arns[0]), user(2, user_arns[1]), group_policy(valid_group), role(trust(user_arns)), attachments([admin]), attachment(admin), set, inst]

member_rcs(set) := member_rcs_with(set, instances("tabp-break-glass"))

valid_stack_set := stack_set("SERVICE_MANAGED", true, valid_member)

template_violation := "[PLAN-BG-05] メンバーの break-glass の StackSet のテンプレート"

member_messages(rcs) := msgs if {
	msgs := p.deny with input as fixture.plan(rcs) with data.policy_plan as {"organization_baseline": decl}
}

with_properties(patch) := {"BreakGlassRole": {"Type": "AWS::IAM::Role", "Properties": object.union(member_properties, patch)}}

rcs(group, trusts, arns) := [organization, user(1, user_arns[0]), user(2, user_arns[1]), group_policy(group), role(trusts), attachments(arns), attachment(admin), stack_set("SERVICE_MANAGED", true, valid_member), instances("tabp-break-glass")]

valid := fixture.plan(rcs(valid_group, trust(user_arns), [admin]))

test_pass_reference_shape if {
	count(p.deny) == 0 with input as valid with data.policy_plan as {"organization_baseline": decl}
}

test_violation_declared_resource_missing if {
	inp := fixture.plan([user(1, user_arns[0]), user(2, user_arns[1]), role(trust(user_arns)), attachments([admin]), attachment(admin)])
	fixture.has("[PLAN-BG-00]", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_declared_attachment_missing if {
	inp := fixture.plan([user(1, user_arns[0]), user(2, user_arns[1]), group_policy(valid_group), role(trust(user_arns)), attachments([admin])])
	fixture.has("[PLAN-BG-00] 宣言した break-glass の module.b.aws_iam_role_policy_attachment.administrator_access", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_attachment_other_policy if {
	inp := fixture.plan([user(1, user_arns[0]), user(2, user_arns[1]), group_policy(valid_group), role(trust(user_arns)), attachments([admin]), attachment("arn:aws:iam::aws:policy/ReadOnlyAccess")])
	fixture.has("[PLAN-BG-04] break-glass の role の attachment が付ける policy", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_attachment_on_other_role if {
	other := change("module.b.aws_iam_role_policy_attachment.administrator_access", "aws_iam_role_policy_attachment", {"role": "other", "policy_arn": admin})
	inp := fixture.plan([user(1, user_arns[0]), user(2, user_arns[1]), group_policy(valid_group), role(trust(user_arns)), attachments([admin]), other])
	fixture.has("[PLAN-BG-04] module.b.aws_iam_role_policy_attachment.administrator_access が AdministratorAccess を break-glass の role の外へ", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_three_users if {
	d := {"break_glass": object.union(decl.break_glass, {"users": array.concat(decl.break_glass.users, ["module.b.aws_iam_user.this[\"break-glass-3\"]"])})}
	inp := fixture.plan(array.concat(rcs(valid_group, trust(user_arns), [admin]), [user(3, "arn:aws:iam::111111111111:user/break-glass/break-glass-3")]))
	fixture.has("[PLAN-BG-01]", p.deny) with input as inp with data.policy_plan as {"organization_baseline": d}
}

users_not_two := "[PLAN-BG-01] break-glass の IAM user の宣言がちょうど2つではありません"

test_violation_one_user if {
	d := {"break_glass": object.union(decl.break_glass, {"users": [decl.break_glass.users[0]]})}
	users_not_two in p.deny with input as valid with data.policy_plan as {"organization_baseline": d}
}

test_violation_no_users if {
	d := {"break_glass": object.union(decl.break_glass, {"users": []})}
	users_not_two in p.deny with input as valid with data.policy_plan as {"organization_baseline": d}
}

test_violation_break_glass_declaration_absent if {
	msgs := p.deny with input as valid with data.policy_plan as {"organization_baseline": {"organization": decl.organization}}
	users_not_two in msgs
	count(msgs) > 0
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

not_denied(action) := sprintf("[PLAN-BG-02] break-glass の group が %s を、条件なしで全 Resource について拒んでいません", [lower(action)])

group_messages(group) := msgs if {
	msgs := p.deny with input as fixture.plan(rcs(group, trust(user_arns), [admin])) with data.policy_plan as {"organization_baseline": decl}
}

with_deny(statements) := array.concat([assume, self_manage, deactivate, password_policy], statements)

test_violation_access_key_not_denied if {
	msgs := group_messages([assume, self_manage, password_policy])
	every a in long_term_credential_actions {
		not_denied(a) in msgs
	}
}

test_violation_long_term_credential_deny_with_condition if {
	conditioned := object.union(long_term_credentials_deny, {"Condition": {"StringEquals": {"aws:username": "nobody"}}})
	msgs := group_messages(with_deny([conditioned]))
	every a in long_term_credential_actions {
		not_denied(a) in msgs
	}
}

test_violation_long_term_credential_deny_with_empty_condition if {
	msgs := group_messages(with_deny([object.union(long_term_credentials_deny, {"Condition": {}})]))
	every a in long_term_credential_actions {
		not_denied(a) in msgs
	}
}

bg02(msgs) := {m | some m in msgs; startswith(m, "[PLAN-BG-02]")}

without_action(i) := bg02(group_messages(with_deny([object.union(long_term_credentials_deny, {"Action": array.concat(array.slice(long_term_credential_actions, 0, i), array.slice(long_term_credential_actions, i + 1, count(long_term_credential_actions)))})])))

test_violation_create_access_key_missing if {
	without_action(0) == {not_denied("iam:CreateAccessKey")}
}

test_violation_create_service_specific_credential_missing if {
	without_action(1) == {not_denied("iam:CreateServiceSpecificCredential")}
}

test_violation_upload_ssh_public_key_missing if {
	without_action(2) == {not_denied("iam:UploadSSHPublicKey")}
}

test_violation_upload_signing_certificate_missing if {
	without_action(3) == {not_denied("iam:UploadSigningCertificate")}
}

test_violation_long_term_credential_deny_partial_wildcard if {
	msgs := group_messages(with_deny([{"Effect": "Deny", "Action": "iam:Create*", "Resource": "*"}]))
	bg02(msgs) == {not_denied("iam:UploadSSHPublicKey"), not_denied("iam:UploadSigningCertificate")}
}

test_pass_long_term_credential_deny_service_wildcard if {
	not fixture.has("[PLAN-BG-02]", group_messages(with_deny([{"Effect": "Deny", "Action": "iam:*", "Resource": "*"}])))
}

test_pass_long_term_credential_deny_all_actions if {
	not fixture.has("[PLAN-BG-02]", group_messages(with_deny([{"Effect": "Deny", "Action": "*", "Resource": "*"}])))
}

test_pass_long_term_credential_deny_single_character_wildcard if {
	actions := ["iam:CreateAccessKe?", "iam:CreateServiceSpecificCredentia?", "iam:UploadSSHPublicKe?", "iam:UploadSigning?ertificate"]
	not fixture.has("[PLAN-BG-02]", group_messages(with_deny([{"Effect": "Deny", "Action": actions, "Resource": "*"}])))
}

test_violation_long_term_credential_deny_without_resource if {
	msgs := group_messages(with_deny([object.remove(long_term_credentials_deny, ["Resource"])]))
	every a in long_term_credential_actions {
		not_denied(a) in msgs
	}
}

test_violation_long_term_credential_deny_not_resource if {
	statement := object.union(object.remove(long_term_credentials_deny, ["Resource"]), {"NotResource": "arn:aws:iam::111111111111:user/other"})
	msgs := group_messages(with_deny([statement]))
	every a in long_term_credential_actions {
		not_denied(a) in msgs
	}
}

test_violation_long_term_credential_deny_not_action if {
	statement := {"Effect": "Deny", "NotAction": "s3:*", "Resource": "*"}
	msgs := group_messages(with_deny([statement]))
	every a in long_term_credential_actions {
		not_denied(a) in msgs
	}
}

test_violation_long_term_credential_deny_narrowed_resource if {
	narrowed := object.union(long_term_credentials_deny, {"Resource": "arn:aws:iam::111111111111:user/break-glass/break-glass-1"})
	msgs := group_messages(with_deny([narrowed]))
	every a in long_term_credential_actions {
		not_denied(a) in msgs
	}
}

test_violation_long_term_credential_deny_partial_resource if {
	partial := object.union(long_term_credentials_deny, {"Resource": ["*", "arn:aws:iam::111111111111:user/break-glass/break-glass-1"]})
	msgs := group_messages(with_deny([partial]))
	every a in long_term_credential_actions {
		not_denied(a) in msgs
	}
}

test_violation_long_term_credential_allow_does_not_count if {
	allow := object.union(long_term_credentials_deny, {"Effect": "Allow"})
	msgs := group_messages(with_deny([allow]))
	every a in long_term_credential_actions {
		not_denied(a) in msgs
	}
}

test_pass_long_term_credential_deny_split_and_cased if {
	first := {"Effect": "Deny", "Action": "IAM:CREATEACCESSKEY", "Resource": "*"}
	second := {"Effect": "Deny", "Action": ["iam:createservicespecificcredential", "iam:UploadSshPublicKey", "iam:UploadSigningCertificate"], "Resource": ["*"]}
	msgs := group_messages(with_deny([first, second]))
	not fixture.has("[PLAN-BG-02]", msgs)
}

test_pass_long_term_credential_deny_wildcard if {
	msgs := group_messages(with_deny([{"Effect": "Deny", "Action": ["iam:Create*", "iam:Upload*"], "Resource": "*"}]))
	not fixture.has("[PLAN-BG-02]", msgs)
}

test_pass_long_term_credential_conditioned_deny_beside_unconditioned if {
	conditioned := object.union(long_term_credentials_deny, {"Condition": {"Bool": {"aws:MultiFactorAuthPresent": "false"}}})
	msgs := group_messages(with_deny([conditioned, long_term_credentials_deny]))
	not fixture.has("[PLAN-BG-02]", msgs)
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

test_violation_group_policy_without_statement_denies_nothing if {
	msgs := group_messages([])
	every a in long_term_credential_actions {
		not_denied(a) in msgs
	}
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

test_violation_member_stack_set_self_managed if {
	inp := fixture.plan(member_rcs(stack_set("SELF_MANAGED", true, valid_member)))
	fixture.has("[PLAN-BG-05] メンバーの break-glass の StackSet が、service-managed", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_member_stack_set_without_auto_deployment if {
	inp := fixture.plan(member_rcs(stack_set("SERVICE_MANAGED", false, valid_member)))
	fixture.has("[PLAN-BG-05] メンバーの break-glass の StackSet が、service-managed", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_member_template_extra_resource if {
	extra := object.union(valid_member, {"Fn": {"Type": "AWS::Lambda::Function", "Properties": {"Code": {"ZipFile": "x"}}}})
	inp := fixture.plan(member_rcs(stack_set("SERVICE_MANAGED", true, extra)))
	fixture.has("[PLAN-BG-05] メンバーの break-glass の StackSet のテンプレート", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_member_role_inline_policy if {
	inp := fixture.plan(member_rcs(stack_set("SERVICE_MANAGED", true, with_properties({"Policies": [{"PolicyName": "x", "PolicyDocument": {}}]}))))
	fixture.has("[PLAN-BG-05] メンバーの break-glass の StackSet のテンプレート", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_member_role_other_managed_policy if {
	inp := fixture.plan(member_rcs(stack_set("SERVICE_MANAGED", true, with_properties({"ManagedPolicyArns": [admin, "arn:aws:iam::aws:policy/ReadOnlyAccess"]}))))
	fixture.has("[PLAN-BG-05] メンバーの break-glass の StackSet のテンプレート", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_member_role_other_name if {
	inp := fixture.plan(member_rcs(stack_set("SERVICE_MANAGED", true, with_properties({"RoleName": "other"}))))
	fixture.has("[PLAN-BG-05] メンバーの break-glass の StackSet のテンプレート", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_member_role_trusts_other_principal if {
	doc := {"Version": "2012-10-17", "Statement": trust(["arn:aws:iam::111111111111:root"])}
	inp := fixture.plan(member_rcs(stack_set("SERVICE_MANAGED", true, with_properties({"AssumeRolePolicyDocument": doc}))))
	fixture.has("[PLAN-BG-05] メンバーの break-glass の StackSet のテンプレート", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_member_role_trust_without_mfa if {
	doc := {"Version": "2012-10-17", "Statement": [{"Effect": "Allow", "Principal": {"AWS": user_arns}, "Action": "sts:AssumeRole"}]}
	inp := fixture.plan(member_rcs(stack_set("SERVICE_MANAGED", true, with_properties({"AssumeRolePolicyDocument": doc}))))
	fixture.has("[PLAN-BG-05] メンバーの break-glass の StackSet のテンプレート", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_member_role_trust_with_deny_only if {
	doc := {"Version": "2012-10-17", "Statement": [{"Effect": "Deny", "Principal": "*", "Action": "sts:AssumeRole"}]}
	inp := fixture.plan(member_rcs(stack_set("SERVICE_MANAGED", true, with_properties({"AssumeRolePolicyDocument": doc}))))
	fixture.has("[PLAN-BG-05] メンバーの break-glass の StackSet のテンプレート", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_member_instances_other_stack_set if {
	rcs := [x | some x in member_rcs(stack_set("SERVICE_MANAGED", true, valid_member)); x.type != "aws_cloudformation_stack_instances"]
	inp := fixture.plan(array.concat(rcs, [instances("other")]))
	fixture.has("[PLAN-BG-05] メンバーの break-glass の stack instance", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_declared_stack_set_missing if {
	rcs := [x | some x in member_rcs(stack_set("SERVICE_MANAGED", true, valid_member)); x.type != "aws_cloudformation_stack_set"]
	fixture.has("[PLAN-BG-00] 宣言した break-glass の module.b.aws_cloudformation_stack_set.this", member_messages(rcs))
}

test_violation_member_role_other_path if {
	fixture.has(template_violation, member_messages(member_rcs(stack_set("SERVICE_MANAGED", true, with_properties({"Path": "/other/"})))))
}

test_violation_member_role_longer_session if {
	fixture.has(template_violation, member_messages(member_rcs(stack_set("SERVICE_MANAGED", true, with_properties({"MaxSessionDuration": 43200})))))
}

test_violation_member_template_transform if {
	set := json.patch(valid_stack_set, [{"op": "replace", "path": "/change/after/template_body", "value": json.marshal({"AWSTemplateFormatVersion": "2010-09-09", "Transform": "AWS::Include", "Resources": valid_member})}])
	fixture.has(template_violation, member_messages(member_rcs(set)))
}

test_violation_member_resource_extra_key if {
	resources := {"BreakGlassRole": {"Type": "AWS::IAM::Role", "Properties": member_properties, "DeletionPolicy": "Retain"}}
	fixture.has(template_violation, member_messages(member_rcs(stack_set("SERVICE_MANAGED", true, resources))))
}

test_violation_member_template_not_json if {
	set := json.patch(valid_stack_set, [{"op": "replace", "path": "/change/after/template_body", "value": "not-json"}])
	fixture.has(template_violation, member_messages(member_rcs(set)))
}

test_violation_member_template_unknown if {
	set := json.patch(valid_stack_set, [{"op": "remove", "path": "/change/after/template_body"}])
	fixture.has(template_violation, member_messages(member_rcs(set)))
}

test_violation_member_template_empty_resources if {
	fixture.has(template_violation, member_messages(member_rcs(stack_set("SERVICE_MANAGED", true, {}))))
}

description_violation := "[PLAN-BG-05] メンバーの break-glass の StackSet の description"

test_violation_member_stack_set_description_not_ascii if {
	set := json.patch(valid_stack_set, [{"op": "replace", "path": "/change/after/description", "value": "メンバーアカウントの break-glass の role"}])
	fixture.has(description_violation, member_messages(member_rcs(set)))
}

test_violation_member_stack_set_description_control_character if {
	set := json.patch(valid_stack_set, [{"op": "replace", "path": "/change/after/description", "value": "Break-glass\nrole"}])
	fixture.has(description_violation, member_messages(member_rcs(set)))
}

test_pass_member_stack_set_without_description if {
	set := json.patch(valid_stack_set, [{"op": "remove", "path": "/change/after/description"}])
	not fixture.has(description_violation, member_messages(member_rcs(set)))
}

test_violation_member_stack_set_auto_deployment_missing if {
	set := json.patch(valid_stack_set, [{"op": "replace", "path": "/change/after/auto_deployment", "value": []}])
	fixture.has("[PLAN-BG-05] メンバーの break-glass の StackSet が、service-managed", member_messages(member_rcs(set)))
}

test_violation_member_role_second_allow_other_principal if {
	doc := {"Version": "2012-10-17", "Statement": array.concat(trust(user_arns), trust(["arn:aws:iam::222222222222:root"]))}
	fixture.has(template_violation, member_messages(member_rcs(stack_set("SERVICE_MANAGED", true, with_properties({"AssumeRolePolicyDocument": doc})))))
}

test_pass_member_role_statement_as_object if {
	doc := {"Version": "2012-10-17", "Statement": trust(user_arns)[0]}
	not fixture.has(template_violation, member_messages(member_rcs(stack_set("SERVICE_MANAGED", true, with_properties({"AssumeRolePolicyDocument": doc})))))
}

test_violation_member_instances_target_other_ou if {
	inst := instances_with({"stack_set_name": "tabp-break-glass", "deployment_targets": [{"organizational_unit_ids": ["ou-exam-security"], "accounts": null}]})
	fixture.has("[PLAN-BG-05] メンバーの break-glass の stack instance の展開先", member_messages(member_rcs_with(valid_stack_set, inst)))
}

test_violation_member_instances_with_accounts if {
	inst := instances_with({"stack_set_name": "tabp-break-glass", "deployment_targets": [{"organizational_unit_ids": ["r-exam"], "accounts": ["333333333333"]}]})
	fixture.has("[PLAN-BG-05] メンバーの break-glass の stack instance の展開先", member_messages(member_rcs_with(valid_stack_set, inst)))
}

test_violation_member_instances_without_stack_set_name if {
	inst := instances_with({"deployment_targets": [{"organizational_unit_ids": ["r-exam"], "accounts": null}]})
	fixture.has("[PLAN-BG-05] メンバーの break-glass の stack instance が、break-glass の StackSet を名指していません", member_messages(member_rcs_with(valid_stack_set, inst)))
}

test_violation_empty_stack_instances_declaration if {
	empty := {"organization": decl.organization, "break_glass": object.union(decl.break_glass, {"stack_instances": ""})}
	rcs := [x | some x in member_rcs(valid_stack_set); x.type != "aws_cloudformation_stack_instances"]
	msgs := p.deny with input as fixture.plan(rcs) with data.policy_plan as {"organization_baseline": empty}
	fixture.has("[PLAN-BG-00] 宣言した break-glass の ", msgs)
}

# 宣言が無いことを、stack instance を見ないまま通さない。
test_violation_undeclared_stack_instances if {
	undeclared := {"organization": decl.organization, "break_glass": object.remove(decl.break_glass, ["stack_instances"])}
	msgs := p.deny with input as fixture.plan(member_rcs(valid_stack_set)) with data.policy_plan as {"organization_baseline": undeclared}
	fixture.has("[PLAN-BG-00] 宣言した break-glass の ", msgs)
}

test_violation_declared_stack_instances_missing if {
	rcs := [x | some x in member_rcs(valid_stack_set); x.type != "aws_cloudformation_stack_instances"]
	fixture.has("[PLAN-BG-00] 宣言した break-glass の module.b.aws_cloudformation_stack_instances.this", member_messages(rcs))
}

own := "arn:aws:iam::111111111111:user/break-glass/${aws:username}"

manage_own(resource) := object.union(self_manage, {"Sid": "ManageOwn", "Resource": resource})

deactivate_own(resource) := object.union(deactivate, {"Sid": "DeactivateOwnMfa", "Resource": resource})

denied(sid) := sprintf("[PLAN-BG-02] break-glass の group の Allow %s が、role の引き受けと自分の資格情報の管理の外です", [sid])

user_address(n) := user(n, "").address

# 差し替え先の address が参照形に無ければ未定義にし、空振りした差し替えでテストが通らないようにする。
with_changes(group, replacements) := changes if {
	base := rcs(group, trust(user_arns), [admin])
	every address, _ in replacements {
		some r in base
		r.address == address
	}
	changes := [object.get(replacements, r.address, r) | some r in base]
}

users_as(after1, after2) := {
	user_address(1): change(user_address(1), "aws_iam_user", after1),
	user_address(2): change(user_address(2), "aws_iam_user", after2),
}

manage_own_group(statement) := [assume, statement, deactivate, password_policy, long_term_credentials_deny]

deactivate_own_group(statement) := [assume, self_manage, statement, password_policy, long_term_credentials_deny]

not_own_resources := [
	"arn:aws:iam::111111111111:user/*${aws:username}",
	"arn:aws:iam::111111111111:user/admins/${aws:username}",
	"arn:aws:iam::*:user/break-glass/${aws:username}",
	user_arns[0],
	[user_arns[0]],
	[own, "arn:aws:iam::111111111111:user/*${aws:username}"],
	[],
	null,
]

test_violation_self_manage_resource_not_own if {
	every resource in not_own_resources {
		inp := fixture.plan(with_changes(manage_own_group(manage_own(resource)), {}))
		fixture.has(denied("ManageOwn"), p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
	}
}

test_violation_deactivate_resource_not_own if {
	every resource in not_own_resources {
		inp := fixture.plan(with_changes(deactivate_own_group(deactivate_own(resource)), {}))
		fixture.has(denied("DeactivateOwnMfa"), p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
	}
}

test_violation_self_manage_without_resource if {
	every statement in [
		object.remove(manage_own(own), ["Resource"]),
		object.union(object.remove(manage_own(own), ["Resource"]), {"NotResource": own}),
	] {
		inp := fixture.plan(with_changes(manage_own_group(statement), {}))
		fixture.has(denied("ManageOwn"), p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
	}
}

# 空の Action は every を真にするので、自分の管理の枝に Action が在ることを求める。
test_violation_self_manage_empty_action if {
	inp := fixture.plan(with_changes(manage_own_group(object.union(manage_own(own), {"Action": []})), {}))
	fixture.has(denied("ManageOwn"), p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_own_resource_not_derivable if {
	every users in [
		users_as({"name": "break-glass-1"}, {"name": "break-glass-2", "arn": user_arns[1]}),
		users_as(null, {"name": "break-glass-2", "arn": user_arns[1]}),
		users_as({"name": 1, "arn": user_arns[0]}, {"name": "break-glass-2", "arn": user_arns[1]}),
		users_as({"name": null, "arn": user_arns[0]}, {"name": "break-glass-2", "arn": user_arns[1]}),
		users_as({"name": "break-glass-1", "arn": 1}, {"name": "break-glass-2", "arn": user_arns[1]}),
		users_as({"name": "break-glass-1", "arn": null}, {"name": "break-glass-2", "arn": user_arns[1]}),
		users_as({"name": "break-glass-1", "arn": user_arns[0]}, {"name": "break-glass-2", "arn": "arn:aws:iam::111111111111:user/other/break-glass-2"}),
		users_as({"name": "break-glass-1", "arn": user_arns[1]}, {"name": "break-glass-2", "arn": user_arns[0]}),
		users_as({"name": "", "arn": "arn:aws:iam::111111111111:user/break-glass/"}, {"name": "", "arn": "arn:aws:iam::111111111111:user/break-glass/"}),
	] {
		inp := fixture.plan(with_changes(manage_own_group(manage_own(own)), users))
		fixture.has(denied("ManageOwn"), p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
	}
}

test_violation_own_resource_users_not_two if {
	every users in [[], [decl.break_glass.users[0]], [decl.break_glass.users[0], decl.break_glass.users[0]]] {
		d := {"break_glass": object.union(decl.break_glass, {"users": users})}
		inp := fixture.plan(with_changes(manage_own_group(manage_own(own)), {}))
		fixture.has(denied("ManageOwn"), p.deny) with input as inp with data.policy_plan as {"organization_baseline": d}
	}
}

test_violation_own_resource_three_users if {
	d := {"break_glass": object.union(decl.break_glass, {"users": array.concat(decl.break_glass.users, [user_address(3)])})}
	inp := fixture.plan(array.concat(with_changes(manage_own_group(manage_own(own)), {}), [user(3, "arn:aws:iam::111111111111:user/break-glass/break-glass-3")]))
	fixture.has(denied("ManageOwn"), p.deny) with input as inp with data.policy_plan as {"organization_baseline": d}
}

test_violation_own_resource_user_change_missing if {
	inp := fixture.plan([r | some r in with_changes(manage_own_group(manage_own(own)), {}); r.address != user_address(2)])
	fixture.has(denied("ManageOwn"), p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_own_resource_from_unfixed_arn if {
	every path in ["/break*/", "/${aws:PrincipalTag/p}/"] {
		users := users_as(
			{"name": "break-glass-1", "arn": sprintf("arn:aws:iam::111111111111:user%sbreak-glass-1", [path])},
			{"name": "break-glass-2", "arn": sprintf("arn:aws:iam::111111111111:user%sbreak-glass-2", [path])},
		)
		resource := sprintf("arn:aws:iam::111111111111:user%s${aws:username}", [path])
		inp := fixture.plan(with_changes(manage_own_group(manage_own(resource)), users))
		fixture.has(denied("ManageOwn"), p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
	}
}

# 接頭辞は partition・iam・12桁のアカウント・user の path を書き切る形に限る。
test_violation_own_resource_from_malformed_arn if {
	every prefix in [
		"x/",
		"xarn:aws:iam::111111111111:user/break-glass/",
		"arn::iam::111111111111:user/break-glass/",
		"arn:aws:iam::111111111111:user/break-glass//",
		"arn:aws:s3::111111111111:user/break-glass/",
		"arn:aws:iam::11111111111:user/break-glass/",
		"arn:aws:iam::111111111111:role/break-glass/",
	] {
		users := users_as({"name": "break-glass-1", "arn": concat("", [prefix, "break-glass-1"])}, {"name": "break-glass-2", "arn": concat("", [prefix, "break-glass-2"])})
		inp := fixture.plan(with_changes(manage_own_group(manage_own(concat("", [prefix, "${aws:username}"]))), users))
		fixture.has(denied("ManageOwn"), p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
	}
}

test_pass_own_resource_as_array if {
	group := [assume, manage_own([own]), deactivate_own([own]), password_policy, long_term_credentials_deny]
	count(p.deny) == 0 with input as fixture.plan(with_changes(group, {})) with data.policy_plan as {"organization_baseline": decl}
}

test_pass_own_resource_path_and_partition if {
	every prefix in [
		"arn:aws:iam::111111111111:user/",
		"arn:aws:iam::111111111111:user/a/b/",
		"arn:aws-us-gov:iam::111111111111:user/break-glass/",
	] {
		users := users_as({"name": "break-glass-1", "arn": concat("", [prefix, "break-glass-1"])}, {"name": "break-glass-2", "arn": concat("", [prefix, "break-glass-2"])})
		resource := concat("", [prefix, "${aws:username}"])
		group := [assume, manage_own(resource), deactivate_own(resource), password_policy, long_term_credentials_deny]
		deny := p.deny with input as fixture.plan(with_changes(group, users)) with data.policy_plan as {"organization_baseline": decl}
		not fixture.has("[PLAN-BG-02]", deny)
	}
}

user_not_fixed(address) := sprintf("[PLAN-BG-01] break-glass の user %s が、", [address])

reported(prefix, msgs) := true if {
	fixture.has(prefix, msgs)
} else := false

trust_denied := "[PLAN-BG-03] break-glass の role の信頼条件が、break-glass の user と aws:MultiFactorAuthPresent に限っていません"

users_trusted_as(arn1, arn2, principals) := object.union(
	users_as({"name": "break-glass-1", "arn": arn1}, {"name": "break-glass-2", "arn": arn2}),
	{"module.b.aws_iam_role.this": role(trust(principals))},
)

test_violation_user_arn_not_fixed if {
	every c in [
		{"arns": ["arn:aws:iam::111111111111:user/break*/break-glass-1", "arn:aws:iam::111111111111:user/break*/break-glass-2"], "bad": {1, 2}},
		{"arns": ["arn:aws:iam::111111111111:user/break?glass/break-glass-1", user_arns[1]], "bad": {1}},
		{"arns": [user_arns[0], "arn:aws:iam::111111111111:user/${aws:PrincipalTag/p}/break-glass-2"], "bad": {2}},
		{"arns": ["arn:aws:iam::111111111111:user/*", "arn:aws:iam::*:user/break-glass/break-glass-2"], "bad": {1, 2}},
	] {
		inp := fixture.plan(with_changes(valid_group, users_trusted_as(c.arns[0], c.arns[1], c.arns)))
		deny := p.deny with input as inp with data.policy_plan as {"organization_baseline": decl}
		fixture.has(trust_denied, deny)
		every n in [1, 2] {
			(n in c.bad) == reported(user_not_fixed(user_address(n)), deny)
		}
	}
}

# 信頼条件が固定の形の user だけを指していても、user が2人そろわなければ通さない。
test_violation_trust_with_one_fixed_user if {
	bad := "arn:aws:iam::111111111111:user/break*/break-glass-2"
	inp := fixture.plan(with_changes(valid_group, users_trusted_as(user_arns[0], bad, [user_arns[0]])))
	fixture.has(trust_denied, p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_user_after_null if {
	inp := fixture.plan(with_changes(valid_group, users_as(null, {"name": "break-glass-2", "arn": user_arns[1]})))
	fixture.has(user_not_fixed(user_address(1)), p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_pass_missing_user_not_reported_as_unfixed if {
	inp := fixture.plan([r | some r in rcs(valid_group, trust(user_arns), [admin]); r.address != user_address(2)])
	deny := p.deny with input as inp with data.policy_plan as {"organization_baseline": decl}
	fixture.has("[PLAN-BG-00]", deny)
	not fixture.has(user_not_fixed(user_address(2)), deny)
}

test_pass_user_arn_path_and_partition_not_reported_as_unfixed if {
	every prefix in [
		"arn:aws:iam::111111111111:user/",
		"arn:aws:iam::111111111111:user/a/b/",
		"arn:aws-us-gov:iam::111111111111:user/break-glass/",
	] {
		arns := [concat("", [prefix, "break-glass-1"]), concat("", [prefix, "break-glass-2"])]
		deny := p.deny with input as fixture.plan(with_changes(valid_group, users_trusted_as(arns[0], arns[1], arns))) with data.policy_plan as {"organization_baseline": decl}
		not fixture.has("[PLAN-BG-01]", deny)
		not fixture.has("[PLAN-BG-03]", deny)
	}
}

test_violation_user_not_shaped if {
	every after1 in [
		{"name": "", "arn": "arn:aws:iam::111111111111:user/break-glass/"},
		{"name": 1, "arn": user_arns[0]},
		{"name": "break-glass-1", "arn": user_arns[1]},
		{"name": "break-glass-1", "arn": "arn:aws:iam::111111111111:role/break-glass/break-glass-1"},
		{"name": "break-glass-1", "arn": "arn:aws:iam::11111111111:user/break-glass/break-glass-1"},
	] {
		inp := fixture.plan(with_changes(valid_group, users_as(after1, {"name": "break-glass-2", "arn": user_arns[1]})))
		deny := p.deny with input as inp with data.policy_plan as {"organization_baseline": decl}
		fixture.has(user_not_fixed(user_address(1)), deny)
		not fixture.has(user_not_fixed(user_address(2)), deny)
	}
}

test_violation_second_admin_attachment_on_break_glass_role if {
	second := change("module.b.aws_iam_role_policy_attachment.second", "aws_iam_role_policy_attachment", {"role": "tabp-break-glass", "policy_arn": admin})
	fixture.has("[PLAN-BG-04] module.b.aws_iam_role_policy_attachment.second が AdministratorAccess を break-glass の role の外へ", p.deny) with input as fixture.plan(array.concat(rcs(valid_group, trust(user_arns), [admin]), [second])) with data.policy_plan as {"organization_baseline": decl}
}

test_violation_attachment_without_policy_arn if {
	inp := fixture.plan([user(1, user_arns[0]), user(2, user_arns[1]), group_policy(valid_group), role(trust(user_arns)), attachments([admin]), change("module.b.aws_iam_role_policy_attachment.administrator_access", "aws_iam_role_policy_attachment", {"role": "tabp-break-glass"})])
	fixture.has("[PLAN-BG-04] break-glass の role の attachment が付ける policy", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_attachment_declaration_key_absent if {
	d := {"break_glass": object.remove(decl.break_glass, ["role_policy_attachment"])}
	fixture.has("[PLAN-BG-00]", p.deny) with input as valid with data.policy_plan as {"organization_baseline": d}
}

login_messages(authorize, token) := msgs if {
	group := [assume, self_manage, deactivate, password_policy, authorize, token, long_term_credentials_deny]
	msgs := p.deny with input as fixture.plan(rcs(group, trust(user_arns), [admin])) with data.policy_plan as {"organization_baseline": decl}
}

test_pass_without_login_statements if {
	not fixture.has("[PLAN-BG-02]", group_messages(with_deny([long_term_credentials_deny])))
}

test_violation_login_remote_client if {
	remote := "arn:aws:signin:ap-northeast-1:111111111111:oauth2/public-client/remote"
	fixture.has("[PLAN-BG-02]", login_messages(object.union(login_authorize, {"Resource": remote}), login_token))
}

test_violation_login_wildcard_resource if {
	fixture.has("[PLAN-BG-02]", login_messages(object.union(login_authorize, {"Resource": "*"}), login_token))
}

test_violation_login_wildcard_region if {
	wildcard := "arn:aws:signin:*:111111111111:oauth2/public-client/localhost"
	fixture.has("[PLAN-BG-02]", login_messages(login_authorize, object.union(login_token, {"Resource": wildcard})))
}

test_violation_login_other_account if {
	other := "arn:aws:signin:ap-northeast-1:222222222222:oauth2/public-client/localhost"
	fixture.has("[PLAN-BG-02]", login_messages(object.union(login_authorize, {"Resource": other}), login_token))
}

test_violation_login_two_resources if {
	two := [login_client, "arn:aws:signin:ap-northeast-1:111111111111:oauth2/public-client/remote"]
	fixture.has("[PLAN-BG-02]", login_messages(object.union(login_authorize, {"Resource": two}), login_token))
}

test_violation_login_token_without_grant_condition if {
	fixture.has("[PLAN-BG-02]", login_messages(login_authorize, object.remove(login_token, ["Condition"])))
}

test_violation_login_token_other_grant if {
	grant := {"StringEquals": {"signin:OAuthGrantType": ["authorization_code", "refresh_token", "client_credentials"]}}
	fixture.has("[PLAN-BG-02]", login_messages(login_authorize, object.union(login_token, {"Condition": grant})))
}

test_violation_login_other_signin_action if {
	fixture.has("[PLAN-BG-02]", login_messages(object.union(login_authorize, {"Action": "signin:*"}), login_token))
}

test_violation_login_both_actions_in_one_statement if {
	both := object.union(login_token, {"Action": ["signin:AuthorizeOAuth2Access", "signin:CreateOAuth2Token"]})
	fixture.has("[PLAN-BG-02]", login_messages(login_authorize, both))
}
