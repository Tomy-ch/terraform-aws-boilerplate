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

long_term_credentials_deny := {"Effect": "Deny", "Action": ["iam:CreateAccessKey"], "Resource": "*"}

deactivate := {"Effect": "Allow", "Action": "iam:DeactivateMFADevice", "Resource": "arn:aws:iam::111111111111:user/break-glass/${aws:username}", "Condition": mfa}

valid_group := [assume, self_manage, deactivate, password_policy, long_term_credentials_deny]

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

test_violation_declared_stack_instances_missing if {
	rcs := [x | some x in member_rcs(valid_stack_set); x.type != "aws_cloudformation_stack_instances"]
	fixture.has("[PLAN-BG-00] 宣言した break-glass の module.b.aws_cloudformation_stack_instances.this", member_messages(rcs))
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
