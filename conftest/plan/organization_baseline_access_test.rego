package plan.organization_baseline_access_test

import data.plan.fixture
import data.plan.organization_baseline_access as p

m := "module.a"

ps_address(k) := sprintf("%s.aws_ssoadmin_permission_set.this[\"%s\"]", [m, k])

ex_address(k) := sprintf("%s.aws_ssoadmin_managed_policy_attachments_exclusive.this[\"%s\"]", [m, k])

inline_address := sprintf("%s.aws_ssoadmin_permission_set_inline_policy.organization_admin", [m])

sets := {
	"read_only": {"suffix": "read-only", "session": "PT8H", "policy": "arn:aws:iam::aws:policy/ReadOnlyAccess", "arn": "arn:aws:sso:::permissionSet/ssoins-0/ps-a"},
	"terraform_plan": {"suffix": "terraform-plan", "session": "PT1H", "policy": "arn:aws:iam::aws:policy/ReadOnlyAccess", "arn": "arn:aws:sso:::permissionSet/ssoins-0/ps-b"},
	"billing": {"suffix": "billing", "session": "PT1H", "policy": "arn:aws:iam::aws:policy/job-function/Billing", "arn": "arn:aws:sso:::permissionSet/ssoins-0/ps-c"},
	"support": {"suffix": "support", "session": "PT1H", "policy": "arn:aws:iam::aws:policy/AWSSupportAccess", "arn": "arn:aws:sso:::permissionSet/ssoins-0/ps-d"},
	"organization_admin": {"suffix": "organization-admin", "session": "PT1H", "policy": "arn:aws:iam::aws:policy/AdministratorAccess", "arn": "arn:aws:sso:::permissionSet/ssoins-0/ps-e"},
}

admin_arn := sets.organization_admin.arn

protected_resources := [
	"arn:aws:iam::111111111111:role/tabp-admin-ci-*",
	"arn:aws:iam::111111111111:policy/tabp-admin-ci-*",
	"arn:aws:iam::111111111111:oidc-provider/token.actions.githubusercontent.com",
	"arn:aws:s3:::state",
	"arn:aws:s3:::state/*",
	"arn:aws:kms:ap-northeast-1:111111111111:key/k",
]

organization := "module.o.aws_organizations_organization.this"

ci_module := "module.deployment_identity."

decl := {"deployment_identity": {"module": ci_module}, "organization_baseline": {
	"organization": organization,
	"access": {
		"permission_sets": {k: {
			"address": ps_address(k),
			"exclusive": ex_address(k),
			"name_suffix": v.suffix,
			"session_duration": v.session,
			"managed_policy_arns": [v.policy],
		} |
			some k, v in sets
		},
		"inline_policy": inline_address,
		"protected_resources": protected_resources,
	},
}}

change(address, type, after) := {"address": address, "type": type, "change": {"actions": ["create"], "after": after, "after_unknown": {}}}

org := change(organization, "aws_organizations_organization", {"master_account_id": "111111111111"})

permission_set(k, attrs) := change(ps_address(k), "aws_ssoadmin_permission_set", object.union({"arn": sets[k].arn, "name": sprintf("tabp-%s", [sets[k].suffix]), "session_duration": sets[k].session, "relay_state": null}, attrs))

exclusive(k, policies) := change(ex_address(k), "aws_ssoadmin_managed_policy_attachments_exclusive", {"permission_set_arn": sets[k].arn, "managed_policy_arns": policies})

valid_statements := [
	{"Sid": "DenyAssumeRoot", "Effect": "Deny", "Action": "sts:AssumeRoot", "Resource": "*"},
	{"Sid": "DenyCiIdentityChange", "Effect": "Deny", "NotAction": ["iam:Get*", "iam:List*"], "Resource": ["arn:aws:iam::111111111111:role/tabp-admin-ci-*", "arn:aws:iam::111111111111:policy/tabp-admin-ci-*", "arn:aws:iam::111111111111:oidc-provider/token.actions.githubusercontent.com"]},
	{"Sid": "DenyBootstrapBucketChange", "Effect": "Deny", "NotAction": ["s3:Get*", "s3:List*"], "Resource": ["arn:aws:s3:::state", "arn:aws:s3:::state/*"]},
	{"Sid": "DenyStateKeyChange", "Effect": "Deny", "NotAction": ["kms:Describe*", "kms:Get*", "kms:List*"], "Resource": "arn:aws:kms:ap-northeast-1:111111111111:key/k"},
]

inline_on(address, arn, statements) := change(address, "aws_ssoadmin_permission_set_inline_policy", {"permission_set_arn": arn, "inline_policy": json.marshal({"Version": "2012-10-17", "Statement": statements})})

inline(statements) := inline_on(inline_address, admin_arn, statements)

assignment(key, k, type, target) := change(
	sprintf("%s.aws_ssoadmin_account_assignment.this[\"%s\"]", [m, key]),
	"aws_ssoadmin_account_assignment",
	{"permission_set_arn": sets[k].arn, "principal_type": type, "principal_id": "00000000-0000-0000-0000-000000000001", "target_id": target, "target_type": "AWS_ACCOUNT"},
)

valid_assignments := [
	assignment("organization_admin/owner", "organization_admin", "USER", "111111111111"),
	assignment("read_only/owner", "read_only", "USER", "111111111111"),
	assignment("security/read_only/operators", "read_only", "GROUP", "222222222222"),
]

base_sets := [permission_set(k, {}) | some k, _ in sets]

base_exclusives := [exclusive(k, [v.policy]) | some k, v in sets]

ci_role(key, name) := change(sprintf("%smodule.g.aws_iam_role.this[\"%s\"]", [ci_module, key]), "aws_iam_role", {"name": name, "path": "/"})

ci_policy(key, name) := change(sprintf("%smodule.g.aws_iam_policy.%s", [ci_module, key]), "aws_iam_policy", {"name": name, "path": "/"})

ci_oidc := change(sprintf("%smodule.g.aws_iam_openid_connect_provider.this", [ci_module]), "aws_iam_openid_connect_provider", {"url": "https://token.actions.githubusercontent.com"})

ci_rcs := [ci_role("plan", "tabp-admin-ci-plan"), ci_role("apply", "tabp-admin-ci-apply"), ci_policy("manage", "tabp-admin-ci-apply-identity-manage"), ci_oidc]

rcs_with(sets_rcs, exclusives_rcs, inline_rcs, assignments_rcs) := array.concat(array.concat(array.concat(array.concat(array.concat([org], ci_rcs), sets_rcs), exclusives_rcs), inline_rcs), assignments_rcs)

valid_rcs := rcs_with(base_sets, base_exclusives, [inline(valid_statements)], valid_assignments)

messages(rs) := msgs if {
	msgs := p.deny with input as fixture.plan(rs) with data.policy_plan as decl
}

without_set(k) := [r | some r in base_sets; r.address != ps_address(k)]

replace_set(k, attrs) := array.concat(without_set(k), [permission_set(k, attrs)])

replace_exclusive(k, policies) := array.concat([r | some r in base_exclusives; r.address != ex_address(k)], [exclusive(k, policies)])

test_pass_reference_shape if {
	count(messages(valid_rcs)) == 0
}

# 00: 宣言と入力の欠け。どれも何も見ないまま通さない。
test_violation_malformed_decl if {
	d := {"deployment_identity": {"module": ci_module}, "organization_baseline": {"organization": organization, "access": object.remove(decl.organization_baseline.access, ["protected_resources"])}}
	fixture.has("[PLAN-IDC-00] 宣言 organization_baseline.access の形が違います", p.deny) with input as fixture.plan(valid_rcs) with data.policy_plan as d
}

test_violation_missing_decl if {
	fixture.has("[PLAN-IDC-00] 宣言 organization_baseline.access の形が違います", p.deny) with input as fixture.plan(valid_rcs) with data.policy_plan as {}
}

test_violation_decl_four_sets if {
	d := {"deployment_identity": {"module": ci_module}, "organization_baseline": {"organization": organization, "access": object.union(object.remove(decl.organization_baseline.access, ["permission_sets"]), {"permission_sets": object.remove(decl.organization_baseline.access.permission_sets, ["billing"])})}}
	fixture.has("[PLAN-IDC-00] 宣言 organization_baseline.access の形が違います", p.deny) with input as fixture.plan(valid_rcs) with data.policy_plan as d
}

test_violation_missing_permission_set if {
	fixture.has(sprintf("[PLAN-IDC-00] 宣言した Identity Center の %s が入力にありません", [ps_address("support")]), messages(rcs_with(without_set("support"), base_exclusives, [inline(valid_statements)], valid_assignments)))
}

test_violation_missing_inline_policy if {
	fixture.has(sprintf("[PLAN-IDC-00] 宣言した Identity Center の %s が入力にありません", [inline_address]), messages(rcs_with(base_sets, base_exclusives, [], valid_assignments)))
}

test_violation_no_assignments if {
	fixture.has("[PLAN-IDC-00] Identity Center の割当が入力にありません", messages(rcs_with(base_sets, base_exclusives, [inline(valid_statements)], [])))
}

test_violation_unknown_permission_set_arn if {
	unknown := {"address": ps_address("billing"), "type": "aws_ssoadmin_permission_set", "change": {"actions": ["create"], "after": {"name": "tabp-billing", "session_duration": "PT1H", "relay_state": null}, "after_unknown": {"arn": true}}}
	fixture.has(sprintf("[PLAN-IDC-00] permission set %s の ARN が plan で決まりません", [ps_address("billing")]), messages(rcs_with(array.concat(without_set("billing"), [unknown]), base_exclusives, [inline(valid_statements)], valid_assignments)))
}

test_violation_unknown_target if {
	a := object.union(assignment("security/read_only/operators", "read_only", "GROUP", null), {"change": {"actions": ["create"], "after": {"permission_set_arn": sets.read_only.arn, "principal_type": "GROUP", "target_type": "AWS_ACCOUNT"}, "after_unknown": {"target_id": true}}})
	fixture.has("[PLAN-IDC-00] 割当 module.a.aws_ssoadmin_account_assignment.this[\"security/read_only/operators\"] の target_id が plan で決まりません", messages(rcs_with(base_sets, base_exclusives, [inline(valid_statements)], array.concat(valid_assignments, [a]))))
}

test_violation_undeclared_assignment_target_set if {
	a := object.union(assignment("x", "read_only", "GROUP", "222222222222"), {"change": {"actions": ["create"], "after": {"permission_set_arn": "arn:aws:sso:::permissionSet/ssoins-0/ps-z", "principal_type": "GROUP", "target_id": "222222222222", "target_type": "AWS_ACCOUNT"}, "after_unknown": {}}})
	fixture.has("[PLAN-IDC-00] 割当 module.a.aws_ssoadmin_account_assignment.this[\"x\"] の permission set が、宣言した permission set ではありません", messages(rcs_with(base_sets, base_exclusives, [inline(valid_statements)], array.concat(valid_assignments, [a]))))
}

test_violation_unknown_management_account if {
	rs := array.concat([change(organization, "aws_organizations_organization", {})], [r | some r in valid_rcs; r.address != organization])
	fixture.has("[PLAN-IDC-00] 組織の管理アカウントの ID が入力で決まりません", messages(rs))
}

# 01
test_pass_closed_list if {
	count([msg | some msg in messages(valid_rcs); startswith(msg, "[PLAN-IDC-01]")]) == 0
}

test_violation_wrong_session if {
	fixture.has(sprintf("[PLAN-IDC-01] permission set %s のセッションの長さが PT8H ではありません", [ps_address("read_only")]), messages(rcs_with(replace_set("read_only", {"session_duration": "PT12H"}), base_exclusives, [inline(valid_statements)], valid_assignments)))
}

test_violation_relay_state if {
	fixture.has(sprintf("[PLAN-IDC-01] permission set %s が relay_state を書いています", [ps_address("billing")]), messages(rcs_with(replace_set("billing", {"relay_state": "https://console.aws.amazon.com/"}), base_exclusives, [inline(valid_statements)], valid_assignments)))
}

test_violation_wrong_name if {
	fixture.has(sprintf("[PLAN-IDC-01] permission set %s の名前が <project>-support ではありません", [ps_address("support")]), messages(rcs_with(replace_set("support", {"name": "tabp-support-access"}), base_exclusives, [inline(valid_statements)], valid_assignments)))
}

test_violation_project_too_long if {
	fixture.has(sprintf("[PLAN-IDC-01] permission set %s の名前が <project>-support ではありません", [ps_address("support")]), messages(rcs_with(replace_set("support", {"name": "abcdefghijklmn-support"}), base_exclusives, [inline(valid_statements)], valid_assignments)))
}

test_violation_extra_permission_set if {
	extra := change(sprintf("%s.aws_ssoadmin_permission_set.this[\"power_user\"]", [m]), "aws_ssoadmin_permission_set", {"arn": "arn:aws:sso:::permissionSet/ssoins-0/ps-f", "name": "tabp-power-user", "session_duration": "PT1H"})
	fixture.has("[PLAN-IDC-01] module.a.aws_ssoadmin_permission_set.this[\"power_user\"] は閉じた一覧の外の permission set です", messages(array.concat(valid_rcs, [extra])))
}

# 02
test_pass_exclusive_only if {
	count([msg | some msg in messages(valid_rcs); startswith(msg, "[PLAN-IDC-02]")]) == 0
}

test_violation_extra_policy if {
	rs := rcs_with(base_sets, replace_exclusive("support", ["arn:aws:iam::aws:policy/AWSSupportAccess", "arn:aws:iam::aws:policy/ReadOnlyAccess"]), [inline(valid_statements)], valid_assignments)
	fixture.has(sprintf("[PLAN-IDC-02] %s の managed policy が宣言の一覧と一致しません", [ex_address("support")]), messages(rs))
}

test_violation_customer_managed_policy if {
	rs := rcs_with(base_sets, replace_exclusive("billing", ["arn:aws:iam::111111111111:policy/tabp-billing"]), [inline(valid_statements)], valid_assignments)
	fixture.has(sprintf("[PLAN-IDC-02] %s の managed policy が宣言の一覧と一致しません", [ex_address("billing")]), messages(rs))
}

test_violation_unused_decl_entry if {
	rs := rcs_with(base_sets, replace_exclusive("billing", []), [inline(valid_statements)], valid_assignments)
	fixture.has(sprintf("[PLAN-IDC-02] %s の managed policy が宣言の一覧と一致しません", [ex_address("billing")]), messages(rs))
}

test_violation_exclusive_on_other_set if {
	wrong := change(ex_address("billing"), "aws_ssoadmin_managed_policy_attachments_exclusive", {"permission_set_arn": sets.support.arn, "managed_policy_arns": [sets.billing.policy]})
	rs := rcs_with(base_sets, array.concat([r | some r in base_exclusives; r.address != ex_address("billing")], [wrong]), [inline(valid_statements)], valid_assignments)
	fixture.has(sprintf("[PLAN-IDC-02] %s が、宣言した permission set %s を指していません", [ex_address("billing"), ps_address("billing")]), messages(rs))
}

test_violation_undeclared_exclusive if {
	extra := change(sprintf("%s.aws_ssoadmin_managed_policy_attachments_exclusive.extra", [m]), "aws_ssoadmin_managed_policy_attachments_exclusive", {"permission_set_arn": sets.read_only.arn, "managed_policy_arns": [sets.read_only.policy]})
	fixture.has("[PLAN-IDC-02] module.a.aws_ssoadmin_managed_policy_attachments_exclusive.extra は宣言の外の managed policy の排他の付与です", messages(array.concat(valid_rcs, [extra])))
}

test_violation_per_policy_attachment if {
	extra := change(sprintf("%s.aws_ssoadmin_managed_policy_attachment.this", [m]), "aws_ssoadmin_managed_policy_attachment", {"permission_set_arn": sets.read_only.arn, "managed_policy_arn": "arn:aws:iam::aws:policy/AdministratorAccess"})
	fixture.has("[PLAN-IDC-02] module.a.aws_ssoadmin_managed_policy_attachment.this は、排他の付与の外で permission set に権限を付けます", messages(array.concat(valid_rcs, [extra])))
}

test_violation_permissions_boundary if {
	extra := change(sprintf("%s.aws_ssoadmin_permissions_boundary_attachment.this", [m]), "aws_ssoadmin_permissions_boundary_attachment", {"permission_set_arn": sets.read_only.arn})
	fixture.has("[PLAN-IDC-02] module.a.aws_ssoadmin_permissions_boundary_attachment.this は、排他の付与の外で permission set に権限を付けます", messages(array.concat(valid_rcs, [extra])))
}

# 03
test_pass_denies_only if {
	count([msg | some msg in messages(valid_rcs); startswith(msg, "[PLAN-IDC-03]")]) == 0
}

test_violation_no_assume_root_deny if {
	rs := rcs_with(base_sets, base_exclusives, [inline([s | some s in valid_statements; s.Sid != "DenyAssumeRoot"])], valid_assignments)
	fixture.has(sprintf("[PLAN-IDC-03] AdministratorAccess を付ける permission set %s に、Resource \"*\" で条件なしの sts:AssumeRoot の Deny がありません", [admin_arn]), messages(rs))
}

test_violation_conditional_assume_root_deny if {
	conditional := {"Sid": "DenyAssumeRoot", "Effect": "Deny", "Action": "sts:AssumeRoot", "Resource": "*", "Condition": {"StringEquals": {"aws:RequestedRegion": "us-east-1"}}}
	rs := rcs_with(base_sets, base_exclusives, [inline(array.concat([conditional], [s | some s in valid_statements; s.Sid != "DenyAssumeRoot"]))], valid_assignments)
	fixture.has(sprintf("[PLAN-IDC-03] AdministratorAccess を付ける permission set %s に、Resource \"*\" で条件なしの sts:AssumeRoot の Deny がありません", [admin_arn]), messages(rs))
}

test_violation_admin_by_policy_not_key if {
	rs := rcs_with(base_sets, replace_exclusive("support", [sets.support.policy, "arn:aws:iam::aws:policy/AdministratorAccess"]), [inline(valid_statements)], valid_assignments)
	fixture.has(sprintf("[PLAN-IDC-03] AdministratorAccess を付ける permission set %s に、Resource \"*\" で条件なしの sts:AssumeRoot の Deny がありません", [sets.support.arn]), messages(rs))
}

test_violation_inline_on_read_only if {
	extra := inline_on(sprintf("%s.aws_ssoadmin_permission_set_inline_policy.read_only", [m]), sets.read_only.arn, [{"Sid": "DenyAssumeRoot", "Effect": "Deny", "Action": "sts:AssumeRoot", "Resource": "*"}])
	fixture.has("[PLAN-IDC-03] module.a.aws_ssoadmin_permission_set_inline_policy.read_only が、AdministratorAccess を付けない permission set に inline policy を付けます", messages(array.concat(valid_rcs, [extra])))
}

test_violation_allow_statement if {
	rs := rcs_with(base_sets, base_exclusives, [inline(array.concat(valid_statements, [{"Sid": "AllowAll", "Effect": "Allow", "Action": "s3:ListBucket", "Resource": "*"}]))], valid_assignments)
	fixture.has(sprintf("[PLAN-IDC-03] %s の inline policy に Deny でない文 AllowAll があります", [inline_address]), messages(rs))
}

test_violation_conditional_deny if {
	conditional := {"Sid": "DenyStateKeyChange", "Effect": "Deny", "NotAction": ["kms:Describe*"], "Resource": "arn:aws:kms:ap-northeast-1:111111111111:key/k", "Condition": {"Bool": {"aws:ViaAWSService": "false"}}}
	rs := rcs_with(base_sets, base_exclusives, [inline(array.concat([s | some s in valid_statements; s.Sid != "DenyStateKeyChange"], [conditional]))], valid_assignments)
	fixture.has(sprintf("[PLAN-IDC-03] %s の inline policy の Deny DenyStateKeyChange が条件を持ちます", [inline_address]), messages(rs))
}

test_violation_write_in_notaction if {
	leaky := {"Sid": "DenyBootstrapBucketChange", "Effect": "Deny", "NotAction": ["s3:Get*", "s3:List*", "s3:PutBucketPolicy"], "Resource": ["arn:aws:s3:::state", "arn:aws:s3:::state/*"]}
	rs := rcs_with(base_sets, base_exclusives, [inline(array.concat([s | some s in valid_statements; s.Sid != "DenyBootstrapBucketChange"], [leaky]))], valid_assignments)
	fixture.has(sprintf("[PLAN-IDC-03] %s の inline policy の Deny DenyBootstrapBucketChange の NotAction に、読みの動詞のほかの s3:putbucketpolicy があります", [inline_address]), messages(rs))
}

test_violation_protected_resource_not_denied if {
	rs := rcs_with(base_sets, base_exclusives, [inline([s | some s in valid_statements; s.Sid != "DenyStateKeyChange"])], valid_assignments)
	fixture.has("[PLAN-IDC-03] AdministratorAccess を付ける permission set の inline policy が、arn:aws:kms:ap-northeast-1:111111111111:key/k の書き込みを拒んでいません", messages(rs))
}

test_violation_unreadable_inline if {
	broken := change(inline_address, "aws_ssoadmin_permission_set_inline_policy", {"permission_set_arn": admin_arn, "inline_policy": "not json"})
	fixture.has(sprintf("[PLAN-IDC-03] %s の inline policy が plan で決まらないか、JSON として読めません", [inline_address]), messages(rcs_with(base_sets, base_exclusives, [broken], valid_assignments)))
}

# 04
test_pass_user_and_groups if {
	count([msg | some msg in messages(valid_rcs); startswith(msg, "[PLAN-IDC-04]")]) == 0
}

test_violation_group_to_management if {
	a := assignment("read_only/operators", "read_only", "GROUP", "111111111111")
	fixture.has("[PLAN-IDC-04] module.a.aws_ssoadmin_account_assignment.this[\"read_only/operators\"] が、管理アカウントへ USER の外で割り当てます", messages(array.concat(valid_rcs, [a])))
}

test_violation_admin_to_member if {
	a := assignment("security/organization_admin/owner", "organization_admin", "USER", "222222222222")
	fixture.has("[PLAN-IDC-04] module.a.aws_ssoadmin_account_assignment.this[\"security/organization_admin/owner\"] が、AdministratorAccess を付ける permission set を管理アカウントの USER の外へ割り当てます", messages(array.concat(valid_rcs, [a])))
}

test_violation_admin_group_to_management if {
	a := assignment("organization_admin/operators", "organization_admin", "GROUP", "111111111111")
	fixture.has("[PLAN-IDC-04] module.a.aws_ssoadmin_account_assignment.this[\"organization_admin/operators\"] が、AdministratorAccess を付ける permission set を管理アカウントの USER の外へ割り当てます", messages(array.concat(valid_rcs, [a])))
}

test_violation_no_admin if {
	rs := rcs_with(base_sets, base_exclusives, [inline(valid_statements)], [r | some r in valid_assignments; r.change.after.permission_set_arn != admin_arn])
	fixture.has("[PLAN-IDC-04] AdministratorAccess を付ける permission set の、管理アカウントへの USER の割当がありません", messages(rs))
}

# key を変えても、付く policy で AdministratorAccess の permission set と読む。
test_violation_admin_policy_on_member_set if {
	rs := rcs_with(base_sets, replace_exclusive("read_only", [sets.read_only.policy, "arn:aws:iam::aws:policy/AdministratorAccess"]), [inline(valid_statements)], valid_assignments)
	fixture.has("[PLAN-IDC-04] module.a.aws_ssoadmin_account_assignment.this[\"security/read_only/operators\"] が、AdministratorAccess を付ける permission set を管理アカウントの USER の外へ割り当てます", messages(rs))
}

# 00: 宣言の中身の欠け。
shape_message := "[PLAN-IDC-00] 宣言 organization_baseline.access の形が違います"

with_access(a) := {"deployment_identity": {"module": ci_module}, "organization_baseline": {"organization": organization, "access": a}}

with_set_field(k, field, value) := with_access(object.union(decl.organization_baseline.access, {"permission_sets": object.union(decl.organization_baseline.access.permission_sets, {k: object.union(decl.organization_baseline.access.permission_sets[k], {field: value})})}))

test_violation_decl_empty_protected_resources if {
	fixture.has(shape_message, p.deny) with input as fixture.plan(valid_rcs) with data.policy_plan as with_access(object.union(decl.organization_baseline.access, {"protected_resources": []}))
}

test_violation_decl_blank_protected_resource if {
	fixture.has(shape_message, p.deny) with input as fixture.plan(valid_rcs) with data.policy_plan as with_access(object.union(decl.organization_baseline.access, {"protected_resources": [""]}))
}

test_violation_decl_extra_key if {
	fixture.has(shape_message, p.deny) with input as fixture.plan(valid_rcs) with data.policy_plan as with_access(object.union(decl.organization_baseline.access, {"extra": "x"}))
}

test_violation_decl_access_not_object if {
	fixture.has(shape_message, p.deny) with input as fixture.plan(valid_rcs) with data.policy_plan as with_access([])
}

test_violation_decl_empty_managed_policy_arns if {
	fixture.has(shape_message, p.deny) with input as fixture.plan(valid_rcs) with data.policy_plan as with_set_field("billing", "managed_policy_arns", [])
}

test_violation_decl_empty_session if {
	fixture.has(shape_message, p.deny) with input as fixture.plan(valid_rcs) with data.policy_plan as with_set_field("billing", "session_duration", "")
}

test_violation_decl_bad_name_suffix if {
	fixture.has(shape_message, p.deny) with input as fixture.plan(valid_rcs) with data.policy_plan as with_set_field("billing", "name_suffix", "Billing")
}

# 00: 入力の欠け。
test_violation_empty_plan if {
	msgs := messages([])
	fixture.has("[PLAN-IDC-00] Identity Center の割当が入力にありません", msgs)
	fixture.has(sprintf("[PLAN-IDC-00] 宣言した Identity Center の %s が入力にありません", [inline_address]), msgs)
}

test_violation_missing_exclusive if {
	rs := rcs_with(base_sets, [r | some r in base_exclusives; r.address != ex_address("support")], [inline(valid_statements)], valid_assignments)
	fixture.has(sprintf("[PLAN-IDC-00] 宣言した Identity Center の %s が入力にありません", [ex_address("support")]), messages(rs))
}

test_violation_missing_organization if {
	fixture.has("[PLAN-IDC-00] 組織の管理アカウントの ID が入力で決まりません", messages([r | some r in valid_rcs; r.address != organization]))
}

test_violation_deleted_only_permission_set if {
	deleted := object.union(permission_set("support", {}), {"change": {"actions": ["delete"], "after": null, "after_unknown": {}}})
	fixture.has(sprintf("[PLAN-IDC-00] 宣言した Identity Center の %s が入力にありません", [ps_address("support")]), messages(rcs_with(array.concat(without_set("support"), [deleted]), base_exclusives, [inline(valid_statements)], valid_assignments)))
}

unknown_assignment(attr) := object.union(object.remove(valid_assignments[2], ["change"]), {"change": {"actions": ["create"], "after": object.remove(valid_assignments[2].change.after, [attr]), "after_unknown": {attr: true}}})

assignment_unknown_message(attr) := sprintf("[PLAN-IDC-00] 割当 module.a.aws_ssoadmin_account_assignment.this[\"security/read_only/operators\"] の %s が plan で決まりません", [attr])

test_violation_unknown_assignment_permission_set_arn if {
	fixture.has(assignment_unknown_message("permission_set_arn"), messages(rcs_with(base_sets, base_exclusives, [inline(valid_statements)], [valid_assignments[0], valid_assignments[1], unknown_assignment("permission_set_arn")])))
}

test_violation_unknown_assignment_principal_type if {
	fixture.has(assignment_unknown_message("principal_type"), messages(rcs_with(base_sets, base_exclusives, [inline(valid_statements)], [valid_assignments[0], valid_assignments[1], unknown_assignment("principal_type")])))
}

test_violation_unknown_assignment_target_type if {
	fixture.has(assignment_unknown_message("target_type"), messages(rcs_with(base_sets, base_exclusives, [inline(valid_statements)], [valid_assignments[0], valid_assignments[1], unknown_assignment("target_type")])))
}

test_violation_missing_assignment_target_id if {
	a := object.union(object.remove(valid_assignments[2], ["change"]), {"change": {"actions": ["create"], "after": object.remove(valid_assignments[2].change.after, ["target_id"]), "after_unknown": {}}})
	fixture.has(assignment_unknown_message("target_id"), messages(rcs_with(base_sets, base_exclusives, [inline(valid_statements)], [valid_assignments[0], valid_assignments[1], a])))
}

# 01
test_pass_project_thirteen_chars if {
	count([msg | some msg in messages(rcs_with(replace_set("support", {"name": "abcdefghijklm-support"}), base_exclusives, [inline(valid_statements)], valid_assignments)); startswith(msg, "[PLAN-IDC-01]")]) == 0
}

name_message := sprintf("[PLAN-IDC-01] permission set %s の名前が <project>-support ではありません", [ps_address("support")])

test_violation_empty_project if {
	fixture.has(name_message, messages(rcs_with(replace_set("support", {"name": "-support"}), base_exclusives, [inline(valid_statements)], valid_assignments)))
}

test_violation_uppercase_project if {
	fixture.has(name_message, messages(rcs_with(replace_set("support", {"name": "Tabp-support"}), base_exclusives, [inline(valid_statements)], valid_assignments)))
}

test_violation_missing_name if {
	nameless := change(ps_address("support"), "aws_ssoadmin_permission_set", {"arn": sets.support.arn, "session_duration": sets.support.session, "relay_state": null})
	fixture.has(name_message, messages(rcs_with(array.concat(without_set("support"), [nameless]), base_exclusives, [inline(valid_statements)], valid_assignments)))
}

test_violation_unknown_name if {
	unknown := {"address": ps_address("support"), "type": "aws_ssoadmin_permission_set", "change": {"actions": ["create"], "after": {"arn": sets.support.arn, "session_duration": sets.support.session, "relay_state": null}, "after_unknown": {"name": true}}}
	fixture.has(name_message, messages(rcs_with(array.concat(without_set("support"), [unknown]), base_exclusives, [inline(valid_statements)], valid_assignments)))
}

test_violation_long_session_on_admin if {
	fixture.has(sprintf("[PLAN-IDC-01] permission set %s のセッションの長さが PT1H ではありません", [ps_address("organization_admin")]), messages(rcs_with(replace_set("organization_admin", {"session_duration": "PT8H"}), base_exclusives, [inline(valid_statements)], valid_assignments)))
}

test_violation_missing_session if {
	sessionless := change(ps_address("billing"), "aws_ssoadmin_permission_set", {"arn": sets.billing.arn, "name": "tabp-billing", "relay_state": null})
	fixture.has(sprintf("[PLAN-IDC-01] permission set %s のセッションの長さが PT1H ではありません", [ps_address("billing")]), messages(rcs_with(array.concat(without_set("billing"), [sessionless]), base_exclusives, [inline(valid_statements)], valid_assignments)))
}

# 02
test_violation_customer_managed_policy_attachment if {
	extra := change(sprintf("%s.aws_ssoadmin_customer_managed_policy_attachment.this", [m]), "aws_ssoadmin_customer_managed_policy_attachment", {"permission_set_arn": sets.read_only.arn})
	fixture.has("[PLAN-IDC-02] module.a.aws_ssoadmin_customer_managed_policy_attachment.this は、排他の付与の外で permission set に権限を付けます", messages(array.concat(valid_rcs, [extra])))
}

test_violation_customer_managed_policy_attachments_exclusive if {
	extra := change(sprintf("%s.aws_ssoadmin_customer_managed_policy_attachments_exclusive.this", [m]), "aws_ssoadmin_customer_managed_policy_attachments_exclusive", {"permission_set_arn": sets.read_only.arn})
	fixture.has("[PLAN-IDC-02] module.a.aws_ssoadmin_customer_managed_policy_attachments_exclusive.this は、排他の付与の外で permission set に権限を付けます", messages(array.concat(valid_rcs, [extra])))
}

test_violation_unknown_exclusive_permission_set_arn if {
	unknown := {"address": ex_address("organization_admin"), "type": "aws_ssoadmin_managed_policy_attachments_exclusive", "change": {"actions": ["create"], "after": {"managed_policy_arns": [sets.organization_admin.policy]}, "after_unknown": {"permission_set_arn": true}}}
	rs := rcs_with(base_sets, array.concat([r | some r in base_exclusives; r.address != ex_address("organization_admin")], [unknown]), [inline(valid_statements)], valid_assignments)
	fixture.has(sprintf("[PLAN-IDC-02] %s が、宣言した permission set %s を指していません", [ex_address("organization_admin"), ps_address("organization_admin")]), messages(rs))
}

# 03 と 04: AdministratorAccess を付ける permission set が1つも無い。
test_violation_no_administrator_set if {
	rs := rcs_with(base_sets, replace_exclusive("organization_admin", [sets.read_only.policy]), [inline(valid_statements)], valid_assignments)
	msgs := messages(rs)
	fixture.has("[PLAN-IDC-04] AdministratorAccess を付ける permission set の、管理アカウントへの USER の割当がありません", msgs)
	fixture.has(sprintf("[PLAN-IDC-03] %s が、AdministratorAccess を付けない permission set に inline policy を付けます", [inline_address]), msgs)
}

# 03: inline policy の文書の形。
inline_document(body) := change(inline_address, "aws_ssoadmin_permission_set_inline_policy", {"permission_set_arn": admin_arn, "inline_policy": json.marshal(body)})

unreadable_message := sprintf("[PLAN-IDC-03] %s の inline policy が plan で決まらないか、JSON として読めません", [inline_address])

assume_root_message := sprintf("[PLAN-IDC-03] AdministratorAccess を付ける permission set %s に、Resource \"*\" で条件なしの sts:AssumeRoot の Deny がありません", [admin_arn])

test_violation_inline_without_statement if {
	fixture.has(unreadable_message, messages(rcs_with(base_sets, base_exclusives, [inline_document({"Version": "2012-10-17"})], valid_assignments)))
}

test_violation_inline_unknown if {
	unknown := {"address": inline_address, "type": "aws_ssoadmin_permission_set_inline_policy", "change": {"actions": ["create"], "after": {"permission_set_arn": admin_arn}, "after_unknown": {"inline_policy": true}}}
	fixture.has(unreadable_message, messages(rcs_with(base_sets, base_exclusives, [unknown], valid_assignments)))
}

test_violation_inline_empty_statement_object if {
	fixture.has(sprintf("[PLAN-IDC-03] %s の inline policy に Deny でない文  があります", [inline_address]), messages(rcs_with(base_sets, base_exclusives, [inline_document({"Version": "2012-10-17", "Statement": {}})], valid_assignments)))
}

test_violation_inline_empty_statement_array if {
	fixture.has(assume_root_message, messages(rcs_with(base_sets, base_exclusives, [inline([])], valid_assignments)))
}

test_violation_inline_single_statement_object if {
	body := {"Version": "2012-10-17", "Statement": {"Sid": "AllowAll", "Effect": "Allow", "Action": "s3:ListBucket", "Resource": "*"}}
	fixture.has(sprintf("[PLAN-IDC-03] %s の inline policy に Deny でない文 AllowAll があります", [inline_address]), messages(rcs_with(base_sets, base_exclusives, [inline_document(body)], valid_assignments)))
}

test_violation_statement_without_effect if {
	rs := rcs_with(base_sets, base_exclusives, [inline(array.concat(valid_statements, [{"Sid": "NoEffect", "Action": "s3:ListBucket", "Resource": "*"}]))], valid_assignments)
	fixture.has(sprintf("[PLAN-IDC-03] %s の inline policy に Deny でない文 NoEffect があります", [inline_address]), messages(rs))
}

test_violation_unknown_inline_permission_set_arn if {
	unknown := {"address": inline_address, "type": "aws_ssoadmin_permission_set_inline_policy", "change": {"actions": ["create"], "after": {"inline_policy": json.marshal({"Version": "2012-10-17", "Statement": valid_statements})}, "after_unknown": {"permission_set_arn": true}}}
	fixture.has(sprintf("[PLAN-IDC-03] %s の inline policy を付ける permission set が plan で決まりません", [inline_address]), messages(rcs_with(base_sets, base_exclusives, [unknown], valid_assignments)))
}

# 03: AssumeRoot の Deny の形。
with_assume_root(statement) := rcs_with(base_sets, base_exclusives, [inline(array.concat([statement], [s | some s in valid_statements; s.Sid != "DenyAssumeRoot"]))], valid_assignments)

test_pass_assume_root_array_forms if {
	count([msg | some msg in messages(with_assume_root({"Sid": "DenyAssumeRoot", "Effect": "Deny", "Action": ["sts:AssumeRoot"], "Resource": ["*"]})); startswith(msg, "[PLAN-IDC-03]")]) == 0
}

test_violation_assume_root_service_wildcard if {
	fixture.has(assume_root_message, messages(with_assume_root({"Sid": "DenyAssumeRoot", "Effect": "Deny", "Action": "sts:*", "Resource": "*"})))
}

test_violation_assume_root_with_other_action if {
	fixture.has(assume_root_message, messages(with_assume_root({"Sid": "DenyAssumeRoot", "Effect": "Deny", "Action": ["sts:AssumeRoot", "sts:AssumeRole"], "Resource": "*"})))
}

test_violation_assume_root_narrowed_resource if {
	fixture.has(assume_root_message, messages(with_assume_root({"Sid": "DenyAssumeRoot", "Effect": "Deny", "Action": "sts:AssumeRoot", "Resource": "arn:aws:iam::222222222222:root"})))
}

# 03: 守る ARN の Deny の形。
with_state_key_deny(statement) := rcs_with(base_sets, base_exclusives, [inline(array.concat([s | some s in valid_statements; s.Sid != "DenyStateKeyChange"], [statement]))], valid_assignments)

state_key := "arn:aws:kms:ap-northeast-1:111111111111:key/k"

test_pass_not_action_string if {
	count([msg | some msg in messages(with_state_key_deny({"Sid": "DenyStateKeyChange", "Effect": "Deny", "NotAction": "kms:Describe*", "Resource": state_key})); startswith(msg, "[PLAN-IDC-03]")]) == 0
}

test_violation_kms_write_in_notaction if {
	fixture.has(sprintf("[PLAN-IDC-03] %s の inline policy の Deny DenyStateKeyChange の NotAction に、読みの動詞のほかの kms:decrypt があります", [inline_address]), messages(with_state_key_deny({"Sid": "DenyStateKeyChange", "Effect": "Deny", "NotAction": ["kms:Describe*", "kms:Decrypt"], "Resource": state_key})))
}

test_violation_protected_by_action if {
	msgs := messages(with_state_key_deny({"Sid": "DenyStateKeyChange", "Effect": "Deny", "Action": "kms:ScheduleKeyDeletion", "Resource": state_key}))
	fixture.has(sprintf("[PLAN-IDC-03] %s の inline policy の Deny DenyStateKeyChange が %s を、条件なしで NotAction の読みの動詞だけを残す形で拒んでいません", [inline_address, state_key]), msgs)
	fixture.has(sprintf("[PLAN-IDC-03] AdministratorAccess を付ける permission set の inline policy が、%s の書き込みを拒んでいません", [state_key]), msgs)
}

test_violation_protected_by_conditional_deny if {
	msgs := messages(with_state_key_deny({"Sid": "DenyStateKeyChange", "Effect": "Deny", "NotAction": ["kms:Describe*"], "Resource": state_key, "Condition": {"Bool": {"aws:ViaAWSService": "false"}}}))
	fixture.has(sprintf("[PLAN-IDC-03] %s の inline policy の Deny DenyStateKeyChange が %s を、条件なしで NotAction の読みの動詞だけを残す形で拒んでいません", [inline_address, state_key]), msgs)
	fixture.has(sprintf("[PLAN-IDC-03] AdministratorAccess を付ける permission set の inline policy が、%s の書き込みを拒んでいません", [state_key]), msgs)
}

# 04
test_violation_user_to_member if {
	a := assignment("security/read_only/owner", "read_only", "USER", "222222222222")
	fixture.has("[PLAN-IDC-04] module.a.aws_ssoadmin_account_assignment.this[\"security/read_only/owner\"] が、メンバーアカウントへ GROUP の外で割り当てます", messages(array.concat(valid_rcs, [a])))
}

# 03: CI の role・policy・OIDC provider が、Deny の pattern に入る。
without_ci(address) := [r | some r in valid_rcs; r.address != address]

test_violation_no_ci_identities if {
	fixture.has("[PLAN-IDC-00] 管理アカウントの CI の role・policy・OIDC provider（宣言 deployment_identity.module の下）が入力にありません", messages([r | some r in valid_rcs; not startswith(r.address, ci_module)]))
}

test_violation_ci_role_outside_pattern if {
	stray := ci_role("deploy", "tabp-admin-deploy")
	fixture.has(sprintf("[PLAN-IDC-03] 管理アカウントの CI の %s（role/tabp-admin-deploy）が、organization_admin の Deny が守る ARN の pattern の外にあります", [stray.address]), messages(array.concat(valid_rcs, [stray])))
}

test_violation_ci_role_with_path if {
	stray := change(sprintf("%smodule.g.aws_iam_role.this[\"pathed\"]", [ci_module]), "aws_iam_role", {"name": "tabp-admin-ci-pathed", "path": "/ci/"})
	fixture.has(sprintf("[PLAN-IDC-03] 管理アカウントの CI の %s（role/ci/tabp-admin-ci-pathed）が、organization_admin の Deny が守る ARN の pattern の外にあります", [stray.address]), messages(array.concat(valid_rcs, [stray])))
}

test_violation_ci_policy_outside_pattern if {
	stray := ci_policy("other", "tabp-admin-other")
	fixture.has(sprintf("[PLAN-IDC-03] 管理アカウントの CI の %s（policy/tabp-admin-other）が、organization_admin の Deny が守る ARN の pattern の外にあります", [stray.address]), messages(array.concat(valid_rcs, [stray])))
}

test_violation_ci_oidc_provider_not_covered if {
	stray := change(sprintf("%smodule.g.aws_iam_openid_connect_provider.other", [ci_module]), "aws_iam_openid_connect_provider", {"url": "https://example.com"})
	fixture.has(sprintf("[PLAN-IDC-03] 管理アカウントの CI の %s（oidc-provider/example.com）が、organization_admin の Deny が守る ARN の pattern の外にあります", [stray.address]), messages(array.concat(valid_rcs, [stray])))
}

test_violation_unknown_ci_role_name if {
	unknown := {"address": sprintf("%smodule.g.aws_iam_role.this[\"plan\"]", [ci_module]), "type": "aws_iam_role", "change": {"actions": ["create"], "after": {"path": "/"}, "after_unknown": {"name": true}}}
	fixture.has(sprintf("[PLAN-IDC-00] 管理アカウントの CI の %s の名前が plan で決まりません", [unknown.address]), messages(array.concat(without_ci(unknown.address), [unknown])))
}

test_pass_ci_identities_covered if {
	count([msg | some msg in messages(valid_rcs); contains(msg, "管理アカウントの CI の")]) == 0
}
