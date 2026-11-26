package plan.organization_baseline_bootstrap_read_test

import data.plan.fixture
import data.plan.organization_baseline_bootstrap_read as p

plan_policy := "module.d.aws_iam_role_policy.this[\"plan\"]"

plan_role_address := "module.d.aws_iam_role.this[\"plan\"]"

decl := {
	"documents": {"aws_iam_role_policy": ["policy"]},
	"deployment_identity": {"plan_role": {"role": plan_role_address, "policy": plan_policy}},
	"organization_baseline": {
		"organization": "module.o.aws_organizations_organization.this",
		"policies": {
			"deny-bootstrap-role": "module.o.aws_organizations_policy.this[\"deny-bootstrap-role\"]",
			"protect-governance-roles": "module.o.aws_organizations_policy.this[\"protect-governance-roles\"]",
		},
		"bootstrap_read": {
			"stack_set": "module.b.aws_cloudformation_stack_set.this",
			"stack_instances": "module.b.aws_cloudformation_stack_instances.this",
			"management_only_actions": [{"action": "organizations:DescribeOrganization", "reason": "組織"}, {"action": "s3:GetObject", "reason": "state"}],
		},
	},
}

change(address, type, after) := {"address": address, "type": type, "change": {"actions": ["create"], "after": after, "after_unknown": {}}}

organization := change("module.o.aws_organizations_organization.this", "aws_organizations_organization", {"master_account_id": "111111111111", "roots": [{"id": "r-exam"}]})

plan_role(statements) := fixture.doc("aws_iam_role_policy", plan_policy, "policy", {"Statement": statements})

valid_plan_role := plan_role([
	{"Sid": "ListBuckets", "Effect": "Allow", "Action": "s3:ListBucket", "Resource": "arn:aws:s3:::b"},
	{"Sid": "ReadState", "Effect": "Allow", "Action": "s3:GetObject", "Resource": "arn:aws:s3:::b/k"},
	{"Sid": "ReadOrganization", "Effect": "Allow", "Action": "organizations:DescribeOrganization", "Resource": "*"},
	{"Sid": "Deny", "Effect": "Deny", "Action": "s3:PutObject", "Resource": "*"},
])

trust := {
	"Effect": "Allow",
	"Principal": {"AWS": "arn:aws:iam::111111111111:root"},
	"Action": "sts:AssumeRole",
	"Condition": {"ArnEquals": {"aws:PrincipalArn": "arn:aws:iam::111111111111:role/tabp-admin-ci-plan"}},
}

read := {"Sid": "ListBuckets", "Effect": "Allow", "Action": "s3:ListBucket", "Resource": [{"Fn::Sub": "arn:${AWS::Partition}:s3:::tabp-*-tfstate-${AWS::AccountId}-*"}]}

properties(trusts, reads) := {
	"RoleName": "tabp-bootstrap-read",
	"Path": "/bootstrap/",
	"MaxSessionDuration": 3600,
	"AssumeRolePolicyDocument": {"Version": "2012-10-17", "Statement": trusts},
	"Policies": [{"PolicyName": "bootstrap-read", "PolicyDocument": {"Version": "2012-10-17", "Statement": reads}}],
}

stack_set_with(attrs, props) := change("module.b.aws_cloudformation_stack_set.this", "aws_cloudformation_stack_set", object.union({
	"name": "tabp-bootstrap-read",
	"description": "Read-only role for bootstrapping member accounts",
	"permission_model": "SERVICE_MANAGED",
	"auto_deployment": [{"enabled": true, "retain_stacks_on_account_removal": false}],
	"template_body": json.marshal({"AWSTemplateFormatVersion": "2010-09-09", "Resources": {"BootstrapReadRole": {"Type": "AWS::IAM::Role", "Properties": props}}}),
}, attrs))

instances_with(target) := change("module.b.aws_cloudformation_stack_instances.this", "aws_cloudformation_stack_instances", {"stack_set_name": "tabp-bootstrap-read", "deployment_targets": [target]})

root_target := {"organizational_unit_ids": ["r-exam"], "accounts": null}

pattern := "arn:aws:iam::*:role/bootstrap/tabp-bootstrap-read"

scp(key, statements) := change(sprintf("module.o.aws_organizations_policy.this[\"%s\"]", [key]), "aws_organizations_policy", {"content": json.marshal({"Statement": statements})})

bootstrap_scp(principals) := scp("deny-bootstrap-role", [{"Sid": "D", "Effect": "Deny", "Action": "*", "Resource": "*", "Condition": {"ArnLike": {"aws:PrincipalArn": principals}}}])

protect(resource) := scp("protect-governance-roles", [{
	"Sid": "P", "Effect": "Deny", "Resource": resource,
	"Action": ["iam:Attach*", "iam:Create*", "iam:Delete*", "iam:Detach*", "iam:PassRole", "iam:Put*", "iam:Update*"],
	"Condition": {"ArnNotLike": {"aws:PrincipalArn": "arn:aws:iam::*:role/stacksets-exec-*"}},
}])

valid_bootstrap_scp := bootstrap_scp(["arn:aws:iam::*:role/OrganizationAccountAccessRole", pattern])

plan_role_resource := change(plan_role_address, "aws_iam_role", {"name": "tabp-admin-ci-plan", "path": "/"})

rcs(set, inst, bootstrap_deny_scp, protect_scp, role_policy) := [organization, plan_role_resource, set, inst, bootstrap_deny_scp, protect_scp, role_policy]

valid_rcs := rcs(stack_set_with({}, properties([trust], [read])), instances_with(root_target), valid_bootstrap_scp, protect(pattern), valid_plan_role)

messages(rs) := msgs if {
	msgs := p.deny with input as fixture.plan(rs) with data.policy_plan as decl
}

test_pass_reference_shape if {
	count(messages(valid_rcs)) == 0
}

test_violation_declaration_missing if {
	d := object.union(object.remove(decl, ["organization_baseline"]), {"organization_baseline": object.remove(decl.organization_baseline, ["bootstrap_read"])})
	fixture.has("[PLAN-BR-00] 宣言 organization_baseline.bootstrap_read", p.deny) with input as fixture.plan(valid_rcs) with data.policy_plan as d
}

test_violation_management_only_wildcard if {
	d := object.union(decl, {"organization_baseline": object.union(decl.organization_baseline, {"bootstrap_read": object.union(decl.organization_baseline.bootstrap_read, {"management_only_actions": [{"action": "organizations:*", "reason": "組織"}]})})})
	fixture.has("[PLAN-BR-00] 宣言 organization_baseline.bootstrap_read", p.deny) with input as fixture.plan(valid_rcs) with data.policy_plan as d
}

test_violation_stack_set_missing if {
	fixture.has("[PLAN-BR-00] 宣言した立ち上げの読み取りの module.b.aws_cloudformation_stack_set.this", messages([organization, plan_role_resource, instances_with(root_target), valid_bootstrap_scp, protect(pattern), valid_plan_role]))
}

# 突き合わせる相手が無いまま緑にしない。
test_violation_plan_role_missing if {
	fixture.has("[PLAN-BR-00] 突き合わせる plan の role", messages([organization, plan_role_resource, stack_set_with({}, properties([trust], [read])), instances_with(root_target), valid_bootstrap_scp, protect(pattern)]))
}

test_violation_template_not_single_role if {
	set := change("module.b.aws_cloudformation_stack_set.this", "aws_cloudformation_stack_set", {"permission_model": "SERVICE_MANAGED", "auto_deployment": [{"enabled": true}], "name": "tabp-bootstrap-read", "template_body": "not json"})
	fixture.has("[PLAN-BR-00] 立ち上げの読み取りの StackSet のテンプレート", messages(rcs(set, instances_with(root_target), valid_bootstrap_scp, protect(pattern), valid_plan_role)))
}

test_violation_self_managed if {
	set := stack_set_with({"permission_model": "SELF_MANAGED"}, properties([trust], [read]))
	fixture.has("[PLAN-BR-01] 立ち上げの読み取りの StackSet が、service-managed", messages(rcs(set, instances_with(root_target), valid_bootstrap_scp, protect(pattern), valid_plan_role)))
}

test_violation_non_ascii_description if {
	set := stack_set_with({"description": "読み取り"}, properties([trust], [read]))
	fixture.has("[PLAN-BR-01] 立ち上げの読み取りの StackSet の description", messages(rcs(set, instances_with(root_target), valid_bootstrap_scp, protect(pattern), valid_plan_role)))
}

test_violation_instances_to_ou if {
	inst := instances_with({"organizational_unit_ids": ["ou-exam-security"], "accounts": null})
	fixture.has("[PLAN-BR-01] 立ち上げの読み取りの stack instance", messages(rcs(stack_set_with({}, properties([trust], [read])), inst, valid_bootstrap_scp, protect(pattern), valid_plan_role)))
}

test_violation_trust_wildcard_principal if {
	t := object.union(trust, {"Condition": {"ArnLike": {"aws:PrincipalArn": "arn:aws:iam::111111111111:role/*"}}})
	fixture.has("[PLAN-BR-02]", messages(rcs(stack_set_with({}, properties([t], [read])), instances_with(root_target), valid_bootstrap_scp, protect(pattern), valid_plan_role)))
}

test_violation_trust_other_account if {
	t := object.union(trust, {"Principal": {"AWS": "arn:aws:iam::222222222222:root"}})
	fixture.has("[PLAN-BR-02]", messages(rcs(stack_set_with({}, properties([t], [read])), instances_with(root_target), valid_bootstrap_scp, protect(pattern), valid_plan_role)))
}

test_violation_write_action if {
	w := {"Sid": "Write", "Effect": "Allow", "Action": "s3:PutBucketPolicy", "Resource": "*"}
	fixture.has("[PLAN-BR-03] 立ち上げの読み取りの role の文 Write", messages(rcs(stack_set_with({}, properties([trust], [read, w])), instances_with(root_target), valid_bootstrap_scp, protect(pattern), valid_plan_role)))
}

test_violation_wildcard_action if {
	w := {"Sid": "Wild", "Effect": "Allow", "Action": "s3:Get*", "Resource": "*"}
	fixture.has("[PLAN-BR-03] 立ち上げの読み取りの role の文 Wild", messages(rcs(stack_set_with({}, properties([trust], [read, w])), instances_with(root_target), valid_bootstrap_scp, protect(pattern), valid_plan_role)))
}

test_violation_state_object_read if {
	w := {"Sid": "State", "Effect": "Allow", "Action": "s3:GetObject", "Resource": "*"}
	fixture.has("[PLAN-BR-03] 立ち上げの読み取りの role が s3:getobject を許します", messages(rcs(stack_set_with({}, properties([trust], [read, w])), instances_with(root_target), valid_bootstrap_scp, protect(pattern), valid_plan_role)))
}

# plan の role に読みが増えたのに、読み取りの role が追随していない。
test_violation_plan_role_read_not_covered if {
	r := plan_role([
		{"Sid": "ListBuckets", "Effect": "Allow", "Action": "s3:ListBucket", "Resource": "arn:aws:s3:::b"},
		{"Sid": "ReadState", "Effect": "Allow", "Action": "s3:GetObject", "Resource": "arn:aws:s3:::b/k"},
		{"Sid": "ReadOrganization", "Effect": "Allow", "Action": "organizations:DescribeOrganization", "Resource": "*"},
		{"Sid": "ReadSlr", "Effect": "Allow", "Action": "iam:GetRole", "Resource": "*"},
	])
	fixture.has("[PLAN-BR-04] plan の role の iam:getrole", messages(rcs(stack_set_with({}, properties([trust], [read])), instances_with(root_target), valid_bootstrap_scp, protect(pattern), r)))
}

test_violation_management_only_unused if {
	r := plan_role([
		{"Sid": "ListBuckets", "Effect": "Allow", "Action": "s3:ListBucket", "Resource": "arn:aws:s3:::b"},
		{"Sid": "ReadState", "Effect": "Allow", "Action": "s3:GetObject", "Resource": "arn:aws:s3:::b/k"},
	])
	fixture.has("[PLAN-BR-05] 宣言 management_only_actions の organizations:describeorganization を、plan の role", messages(rcs(stack_set_with({}, properties([trust], [read])), instances_with(root_target), valid_bootstrap_scp, protect(pattern), r)))
}

test_violation_management_only_in_reader if {
	d := object.union(decl, {"organization_baseline": object.union(decl.organization_baseline, {"bootstrap_read": object.union(decl.organization_baseline.bootstrap_read, {"management_only_actions": [{"action": "organizations:DescribeOrganization", "reason": "組織"}, {"action": "s3:GetObject", "reason": "state"}, {"action": "s3:ListBucket", "reason": "x"}]})})})
	r := plan_role([
		{"Sid": "ListBuckets", "Effect": "Allow", "Action": "s3:ListBucket", "Resource": "arn:aws:s3:::b"},
		{"Sid": "ReadState", "Effect": "Allow", "Action": "s3:GetObject", "Resource": "arn:aws:s3:::b/k"},
		{"Sid": "ReadOrganization", "Effect": "Allow", "Action": "organizations:DescribeOrganization", "Resource": "*"},
	])
	fixture.has("[PLAN-BR-05] 宣言 management_only_actions の s3:listbucket を、立ち上げの読み取りの role が持っています", p.deny) with input as fixture.plan(rcs(stack_set_with({}, properties([trust], [read])), instances_with(root_target), valid_bootstrap_scp, protect(pattern), r)) with data.policy_plan as d
}

test_violation_bootstrap_role_not_denied if {
	d := bootstrap_scp(["arn:aws:iam::*:role/OrganizationAccountAccessRole"])
	fixture.has("[PLAN-BR-06] deny-bootstrap-role", messages(rcs(stack_set_with({}, properties([trust], [read])), instances_with(root_target), d, protect(pattern), valid_plan_role)))
}

test_violation_bootstrap_role_not_protected if {
	fixture.has("[PLAN-BR-06] protect-governance-roles", messages(rcs(stack_set_with({}, properties([trust], [read])), instances_with(root_target), valid_bootstrap_scp, protect("arn:aws:iam::*:role/break-glass/tabp-break-glass"), valid_plan_role)))
}

# 以下、宣言・入力の欠けと壊れ。どれも何も見ないまま通さない。
with_bootstrap_read(patch) := object.union(object.remove(decl, ["organization_baseline"]), {"organization_baseline": object.union(object.remove(decl.organization_baseline, ["bootstrap_read"]), {"bootstrap_read": object.union(decl.organization_baseline.bootstrap_read, patch)})})

test_violation_management_only_empty if {
	fixture.has("[PLAN-BR-00] 宣言 organization_baseline.bootstrap_read", p.deny) with input as fixture.plan(valid_rcs) with data.policy_plan as with_bootstrap_read({"management_only_actions": []})
}

test_violation_management_only_not_array if {
	fixture.has("[PLAN-BR-00] 宣言 organization_baseline.bootstrap_read", p.deny) with input as fixture.plan(valid_rcs) with data.policy_plan as with_bootstrap_read({"management_only_actions": "s3:GetObject"})
}

test_violation_stack_instances_not_string if {
	fixture.has("[PLAN-BR-00] 宣言 organization_baseline.bootstrap_read", p.deny) with input as fixture.plan(valid_rcs) with data.policy_plan as with_bootstrap_read({"stack_instances": 1})
}

test_violation_stack_instances_missing if {
	fixture.has("[PLAN-BR-00] 宣言した立ち上げの読み取りの module.b.aws_cloudformation_stack_instances.this", messages([organization, plan_role_resource, stack_set_with({}, properties([trust], [read])), valid_bootstrap_scp, protect(pattern), valid_plan_role]))
}

template_with(resources) := change("module.b.aws_cloudformation_stack_set.this", "aws_cloudformation_stack_set", {
	"name": "tabp-bootstrap-read",
	"permission_model": "SERVICE_MANAGED",
	"auto_deployment": [{"enabled": true}],
	"template_body": json.marshal({"AWSTemplateFormatVersion": "2010-09-09", "Resources": resources}),
})

role_resource := {"Type": "AWS::IAM::Role", "Properties": properties([trust], [read])}

test_violation_template_two_resources if {
	set := template_with({"A": role_resource, "B": role_resource})
	fixture.has("[PLAN-BR-00] 立ち上げの読み取りの StackSet のテンプレート", messages(rcs(set, instances_with(root_target), valid_bootstrap_scp, protect(pattern), valid_plan_role)))
}

test_violation_template_resource_not_iam_role if {
	set := template_with({"A": {"Type": "AWS::IAM::User", "Properties": {}}})
	fixture.has("[PLAN-BR-00] 立ち上げの読み取りの StackSet のテンプレート", messages(rcs(set, instances_with(root_target), valid_bootstrap_scp, protect(pattern), valid_plan_role)))
}

test_violation_template_without_resources if {
	set := template_with({})
	fixture.has("[PLAN-BR-00] 立ち上げの読み取りの StackSet のテンプレート", messages(rcs(set, instances_with(root_target), valid_bootstrap_scp, protect(pattern), valid_plan_role)))
}

# inline policy が無いと読み取りの文が0件になり、BR-03 が空振りする。テンプレートの形として落とす。
test_violation_policies_missing if {
	props := object.remove(properties([trust], [read]), ["Policies"])
	fixture.has("[PLAN-BR-00] 立ち上げの読み取りの StackSet のテンプレート", messages(rcs(stack_set_with({}, props), instances_with(root_target), valid_bootstrap_scp, protect(pattern), valid_plan_role)))
}

test_violation_plan_role_deny_only if {
	r := plan_role([{"Sid": "Deny", "Effect": "Deny", "Action": "s3:PutObject", "Resource": "*"}])
	fixture.has("[PLAN-BR-00] 突き合わせる plan の role", messages(rcs(stack_set_with({}, properties([trust], [read])), instances_with(root_target), valid_bootstrap_scp, protect(pattern), r)))
}

test_violation_plan_role_declaration_missing if {
	d := object.union(object.remove(decl, ["deployment_identity"]), {"deployment_identity": {}})
	fixture.has("[PLAN-BR-00] 突き合わせる plan の role", p.deny) with input as fixture.plan(valid_rcs) with data.policy_plan as d
}

test_violation_organization_missing if {
	msgs := messages([plan_role_resource, stack_set_with({}, properties([trust], [read])), instances_with(root_target), valid_bootstrap_scp, protect(pattern), valid_plan_role])
	fixture.has("[PLAN-BR-01] 立ち上げの読み取りの stack instance", msgs)
	fixture.has("[PLAN-BR-02]", msgs)
}

test_violation_auto_deployment_disabled if {
	set := stack_set_with({"auto_deployment": [{"enabled": false, "retain_stacks_on_account_removal": false}]}, properties([trust], [read]))
	fixture.has("[PLAN-BR-01] 立ち上げの読み取りの StackSet が、service-managed", messages(rcs(set, instances_with(root_target), valid_bootstrap_scp, protect(pattern), valid_plan_role)))
}

test_pass_description_absent if {
	set := stack_set_with({"description": null}, properties([trust], [read]))
	count(messages(rcs(set, instances_with(root_target), valid_bootstrap_scp, protect(pattern), valid_plan_role))) == 0
}

test_violation_description_control_character if {
	set := stack_set_with({"description": "Read-only\nrole"}, properties([trust], [read]))
	fixture.has("[PLAN-BR-01] 立ち上げの読み取りの StackSet の description", messages(rcs(set, instances_with(root_target), valid_bootstrap_scp, protect(pattern), valid_plan_role)))
}

test_violation_instances_name_mismatch if {
	inst := change("module.b.aws_cloudformation_stack_instances.this", "aws_cloudformation_stack_instances", {"stack_set_name": "other", "deployment_targets": [root_target]})
	fixture.has("[PLAN-BR-01] 立ち上げの読み取りの stack instance", messages(rcs(stack_set_with({}, properties([trust], [read])), inst, valid_bootstrap_scp, protect(pattern), valid_plan_role)))
}

test_violation_stack_set_name_differs_from_role_name if {
	set := stack_set_with({"name": "tabp-other"}, properties([trust], [read]))
	inst := change("module.b.aws_cloudformation_stack_instances.this", "aws_cloudformation_stack_instances", {"stack_set_name": "tabp-other", "deployment_targets": [root_target]})
	fixture.has("[PLAN-BR-01] 立ち上げの読み取りの stack instance", messages(rcs(set, inst, valid_bootstrap_scp, protect(pattern), valid_plan_role)))
}

test_violation_instances_two_targets if {
	inst := change("module.b.aws_cloudformation_stack_instances.this", "aws_cloudformation_stack_instances", {"stack_set_name": "tabp-bootstrap-read", "deployment_targets": [root_target, root_target]})
	fixture.has("[PLAN-BR-01] 立ち上げの読み取りの stack instance", messages(rcs(stack_set_with({}, properties([trust], [read])), inst, valid_bootstrap_scp, protect(pattern), valid_plan_role)))
}

test_violation_instances_with_accounts if {
	inst := instances_with({"organizational_unit_ids": ["r-exam"], "accounts": ["111111111111"]})
	fixture.has("[PLAN-BR-01] 立ち上げの読み取りの stack instance", messages(rcs(stack_set_with({}, properties([trust], [read])), inst, valid_bootstrap_scp, protect(pattern), valid_plan_role)))
}

trust_violation(t) if {
	fixture.has("[PLAN-BR-02]", messages(rcs(stack_set_with({}, properties(t, [read])), instances_with(root_target), valid_bootstrap_scp, protect(pattern), valid_plan_role)))
}

test_violation_trust_arnequals_with_wildcard if {
	trust_violation([object.union(trust, {"Condition": {"ArnEquals": {"aws:PrincipalArn": "arn:aws:iam::111111111111:role/tabp-*"}}})])
}

test_violation_trust_arnequals_with_question_mark if {
	trust_violation([object.union(trust, {"Condition": {"ArnEquals": {"aws:PrincipalArn": "arn:aws:iam::111111111111:role/tabp-?"}}})])
}

test_violation_trust_arnequals_user if {
	trust_violation([object.union(trust, {"Condition": {"ArnEquals": {"aws:PrincipalArn": "arn:aws:iam::111111111111:user/tabp"}}})])
}

test_violation_trust_arnequals_list if {
	trust_violation([object.union(trust, {"Condition": {"ArnEquals": {"aws:PrincipalArn": ["arn:aws:iam::111111111111:role/tabp-admin-ci-plan"]}}})])
}

test_violation_trust_two_statements if {
	trust_violation([trust, trust])
}

test_violation_trust_action_wildcard if {
	trust_violation([object.union(trust, {"Action": "sts:*"})])
}

test_violation_trust_extra_key if {
	trust_violation([object.union(trust, {"Sid": "X"})])
}

test_violation_role_managed_policy if {
	props := object.union(properties([trust], [read]), {"ManagedPolicyArns": ["arn:aws:iam::aws:policy/ReadOnlyAccess"]})
	fixture.has("[PLAN-BR-02]", messages(rcs(stack_set_with({}, props), instances_with(root_target), valid_bootstrap_scp, protect(pattern), valid_plan_role)))
}

read_violation(s, prefix) if {
	fixture.has(prefix, messages(rcs(stack_set_with({}, properties([trust], [read, s])), instances_with(root_target), valid_bootstrap_scp, protect(pattern), valid_plan_role)))
}

test_violation_statement_deny_effect if {
	read_violation({"Sid": "X", "Effect": "Deny", "Action": "s3:GetBucketAcl", "Resource": "*"}, "[PLAN-BR-03] 立ち上げの読み取りの role の文 X")
}

test_violation_statement_not_action if {
	read_violation({"Sid": "X", "Effect": "Allow", "NotAction": "s3:PutObject", "Resource": "*"}, "[PLAN-BR-03] 立ち上げの読み取りの role の文 X")
}

test_violation_statement_condition if {
	read_violation({"Sid": "X", "Effect": "Allow", "Action": "s3:GetBucketAcl", "Resource": "*", "Condition": {}}, "[PLAN-BR-03] 立ち上げの読み取りの role の文 X")
}

test_violation_question_mark_action if {
	read_violation({"Sid": "X", "Effect": "Allow", "Action": "s3:GetBucket?cl", "Resource": "*"}, "[PLAN-BR-03] 立ち上げの読み取りの role の文 X")
}

test_violation_action_list_one_bad_element if {
	read_violation({"Sid": "X", "Effect": "Allow", "Action": ["s3:GetBucketAcl", "s3:PutBucketAcl"], "Resource": "*"}, "[PLAN-BR-03] 立ち上げの読み取りの role の文 X")
}

test_violation_action_without_colon if {
	read_violation({"Sid": "X", "Effect": "Allow", "Action": "GetBucketAcl", "Resource": "*"}, "[PLAN-BR-03] 立ち上げの読み取りの role の文 X")
}

test_violation_object_version_read if {
	read_violation({"Sid": "X", "Effect": "Allow", "Action": "s3:GetObjectVersion", "Resource": "*"}, "[PLAN-BR-03] 立ち上げの読み取りの role が s3:getobjectversion を許します")
}

# kms:GetPublicKey は動詞が読み取りの形なので、暗号操作の一覧でだけ落ちる。
test_violation_crypto_read if {
	read_violation({"Sid": "X", "Effect": "Allow", "Action": "kms:GetPublicKey", "Resource": "*"}, "[PLAN-BR-03] 立ち上げの読み取りの role が kms:getpublickey を許します")
}

test_violation_secret_value_read if {
	read_violation({"Sid": "X", "Effect": "Allow", "Action": "secretsmanager:GetSecretValue", "Resource": "*"}, "[PLAN-BR-03] 立ち上げの読み取りの role が secretsmanager:getsecretvalue を許します")
}

scp_violation(bootstrap, protect_scp, prefix) if {
	fixture.has(prefix, messages(rcs(stack_set_with({}, properties([trust], [read])), instances_with(root_target), bootstrap, protect_scp, valid_plan_role)))
}

test_violation_bootstrap_scp_absent if {
	fixture.has("[PLAN-BR-06] deny-bootstrap-role", messages([organization, plan_role_resource, stack_set_with({}, properties([trust], [read])), instances_with(root_target), protect(pattern), valid_plan_role]))
}

test_violation_protect_scp_absent if {
	fixture.has("[PLAN-BR-06] protect-governance-roles", messages([organization, plan_role_resource, stack_set_with({}, properties([trust], [read])), instances_with(root_target), valid_bootstrap_scp, valid_plan_role]))
}

test_violation_scp_content_not_json if {
	broken := change("module.o.aws_organizations_policy.this[\"deny-bootstrap-role\"]", "aws_organizations_policy", {"content": "not json"})
	scp_violation(broken, protect(pattern), "[PLAN-BR-06] deny-bootstrap-role")
}

test_pass_bootstrap_scp_principal_as_string if {
	single := scp("deny-bootstrap-role", [{"Sid": "D", "Effect": "Deny", "Action": "*", "Resource": "*", "Condition": {"ArnLike": {"aws:PrincipalArn": pattern}}}])
	not fixture.has("[PLAN-BR-06] deny-bootstrap-role", messages(rcs(stack_set_with({}, properties([trust], [read])), instances_with(root_target), single, protect(pattern), valid_plan_role)))
}

test_violation_bootstrap_scp_action_not_star if {
	s := scp("deny-bootstrap-role", [{"Sid": "D", "Effect": "Deny", "Action": "iam:*", "Resource": "*", "Condition": {"ArnLike": {"aws:PrincipalArn": [pattern]}}}])
	scp_violation(s, protect(pattern), "[PLAN-BR-06] deny-bootstrap-role")
}

test_violation_bootstrap_scp_allow if {
	s := scp("deny-bootstrap-role", [{"Sid": "D", "Effect": "Allow", "Action": "*", "Resource": "*", "Condition": {"ArnLike": {"aws:PrincipalArn": [pattern]}}}])
	scp_violation(s, protect(pattern), "[PLAN-BR-06] deny-bootstrap-role")
}

test_violation_protect_without_exception_condition if {
	s := scp("protect-governance-roles", [{"Sid": "P", "Effect": "Deny", "Resource": pattern, "Action": ["iam:Attach*", "iam:Create*", "iam:Delete*", "iam:PassRole", "iam:Put*", "iam:Update*"]}])
	scp_violation(valid_bootstrap_scp, s, "[PLAN-BR-06] protect-governance-roles")
}

test_violation_protect_other_exception if {
	s := scp("protect-governance-roles", [{"Sid": "P", "Effect": "Deny", "Resource": pattern, "Action": ["iam:Attach*", "iam:Create*", "iam:Delete*", "iam:PassRole", "iam:Put*", "iam:Update*"], "Condition": {"ArnNotLike": {"aws:PrincipalArn": "arn:aws:iam::*:role/*"}}}])
	scp_violation(valid_bootstrap_scp, s, "[PLAN-BR-06] protect-governance-roles")
}

test_violation_protect_missing_passrole if {
	s := scp("protect-governance-roles", [{"Sid": "P", "Effect": "Deny", "Resource": pattern, "Action": ["iam:Attach*", "iam:Create*", "iam:Delete*", "iam:Put*", "iam:Update*"], "Condition": {"ArnNotLike": {"aws:PrincipalArn": "arn:aws:iam::*:role/stacksets-exec-*"}}}])
	scp_violation(valid_bootstrap_scp, s, "[PLAN-BR-06] protect-governance-roles")
}

test_violation_management_only_without_reason if {
	fixture.has("[PLAN-BR-00] 宣言 organization_baseline.bootstrap_read", p.deny) with input as fixture.plan(valid_rcs) with data.policy_plan as with_bootstrap_read({"management_only_actions": [{"action": "s3:GetObject", "reason": " "}]})
}

test_violation_management_only_plain_string if {
	fixture.has("[PLAN-BR-00] 宣言 organization_baseline.bootstrap_read", p.deny) with input as fixture.plan(valid_rcs) with data.policy_plan as with_bootstrap_read({"management_only_actions": ["s3:GetObject"]})
}

# 信頼する主体は、宣言した plan の role と同じ名前でなければならない。
test_violation_trust_other_role if {
	trust_violation([object.union(trust, {"Condition": {"ArnEquals": {"aws:PrincipalArn": "arn:aws:iam::111111111111:role/tabp-admin-ci-apply"}}})])
}

test_violation_trust_plan_role_missing if {
	fixture.has("[PLAN-BR-02]", p.deny) with input as fixture.plan([organization, stack_set_with({}, properties([trust], [read])), instances_with(root_target), valid_bootstrap_scp, protect(pattern), valid_plan_role]) with data.policy_plan as decl
}

test_violation_policies_two if {
	props := object.union(properties([trust], [read]), {"Policies": [{"PolicyName": "a", "PolicyDocument": {"Statement": [read]}}, {"PolicyName": "b", "PolicyDocument": {"Fn::Transform": {}}}]})
	fixture.has("[PLAN-BR-00] 立ち上げの読み取りの StackSet のテンプレート", messages(rcs(stack_set_with({}, props), instances_with(root_target), valid_bootstrap_scp, protect(pattern), valid_plan_role)))
}

test_violation_policies_empty_statement if {
	props := object.union(properties([trust], [read]), {"Policies": [{"PolicyName": "a", "PolicyDocument": {"Statement": []}}]})
	fixture.has("[PLAN-BR-00] 立ち上げの読み取りの StackSet のテンプレート", messages(rcs(stack_set_with({}, props), instances_with(root_target), valid_bootstrap_scp, protect(pattern), valid_plan_role)))
}

test_violation_service_outside_read_services if {
	read_violation({"Sid": "X", "Effect": "Allow", "Action": "ssm:GetParameter", "Resource": [{"Fn::Sub": "arn:${AWS::Partition}:ssm:*:${AWS::AccountId}:parameter/x"}]}, "[PLAN-BR-03] 立ち上げの読み取りの role の文 X")
}

test_violation_resource_wildcard if {
	read_violation({"Sid": "X", "Effect": "Allow", "Action": "iam:GetRole", "Resource": "*"}, "[PLAN-BR-03] 立ち上げの読み取りの role の文 X")
}

test_violation_resource_other_account if {
	read_violation({"Sid": "X", "Effect": "Allow", "Action": "iam:GetRole", "Resource": [{"Fn::Sub": "arn:${AWS::Partition}:iam::*:role/x"}]}, "[PLAN-BR-03] 立ち上げの読み取りの role の文 X")
}

test_pass_list_aliases_wildcard if {
	s := {"Sid": "ListKeyAliases", "Effect": "Allow", "Action": "kms:ListAliases", "Resource": "*"}
	not fixture.has("[PLAN-BR-03]", messages(rcs(stack_set_with({}, properties([trust], [read, s])), instances_with(root_target), valid_bootstrap_scp, protect(pattern), valid_plan_role)))
}

test_violation_bootstrap_scp_extra_condition if {
	s := scp("deny-bootstrap-role", [{"Sid": "D", "Effect": "Deny", "Action": "*", "Resource": "*", "Condition": {"ArnLike": {"aws:PrincipalArn": [pattern]}, "StringEquals": {"aws:RequestedRegion": "us-east-1"}}}])
	scp_violation(s, protect(pattern), "[PLAN-BR-06] deny-bootstrap-role")
}

# アカウント ID を含んでも、アカウントの位置が * なら置き先に限っていない。
test_violation_resource_account_id_elsewhere if {
	read_violation({"Sid": "X", "Effect": "Allow", "Action": "iam:GetRole", "Resource": [{"Fn::Sub": "arn:${AWS::Partition}:iam::*:role/x-${AWS::AccountId}"}]}, "[PLAN-BR-03] 立ち上げの読み取りの role の文 X")
}

test_violation_bucket_without_account_id if {
	read_violation({"Sid": "X", "Effect": "Allow", "Action": "s3:GetBucketAcl", "Resource": [{"Fn::Sub": "arn:${AWS::Partition}:s3:::tabp-*-tfstate-*"}]}, "[PLAN-BR-03] 立ち上げの読み取りの role の文 X")
}

plan_role_messages(attrs, t) := msgs if {
	r := change(plan_role_address, "aws_iam_role", attrs)
	msgs := p.deny with input as fixture.plan([organization, r, stack_set_with({}, properties(t, [read])), instances_with(root_target), valid_bootstrap_scp, protect(pattern), valid_plan_role]) with data.policy_plan as decl
}

# path を書かない role は AWS が "/" に置く。plan では null で表れる。
test_pass_plan_role_path_null if {
	not fixture.has("[PLAN-BR-02]", plan_role_messages({"name": "tabp-admin-ci-plan", "path": null}, [trust]))
}

test_violation_plan_role_path_not_in_trust if {
	fixture.has("[PLAN-BR-02]", plan_role_messages({"name": "tabp-admin-ci-plan", "path": "/ci/"}, [trust]))
}

test_pass_plan_role_path_in_trust if {
	t := object.union(trust, {"Condition": {"ArnEquals": {"aws:PrincipalArn": "arn:aws:iam::111111111111:role/ci/tabp-admin-ci-plan"}}})
	not fixture.has("[PLAN-BR-02]", plan_role_messages({"name": "tabp-admin-ci-plan", "path": "/ci/"}, [t]))
}
