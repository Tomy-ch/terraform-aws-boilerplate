package plan.organization_baseline_bootstrap_read_test

import data.plan.fixture
import data.plan.organization_baseline_bootstrap_read as p

plan_policy := "module.d.aws_iam_role_policy.this[\"plan\"]"

decl := {
	"documents": {"aws_iam_role_policy": ["policy"]},
	"deployment_identity": {"plan_role": {"policy": plan_policy}},
	"organization_baseline": {
		"organization": "module.o.aws_organizations_organization.this",
		"policies": {
			"deny-bootstrap-role": "module.o.aws_organizations_policy.this[\"deny-bootstrap-role\"]",
			"protect-governance-roles": "module.o.aws_organizations_policy.this[\"protect-governance-roles\"]",
		},
		"bootstrap_read": {
			"stack_set": "module.b.aws_cloudformation_stack_set.this",
			"stack_instances": "module.b.aws_cloudformation_stack_instances.this",
			"management_only_actions": ["organizations:DescribeOrganization", "s3:GetObject"],
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

read := {"Sid": "ListBuckets", "Effect": "Allow", "Action": "s3:ListBucket", "Resource": {"Fn::Sub": "arn:${AWS::Partition}:s3:::tabp-*"}}

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

rcs(set, inst, bootstrap_deny_scp, protect_scp, role_policy) := [organization, set, inst, bootstrap_deny_scp, protect_scp, role_policy]

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
	d := object.union(decl, {"organization_baseline": object.union(decl.organization_baseline, {"bootstrap_read": object.union(decl.organization_baseline.bootstrap_read, {"management_only_actions": ["organizations:*"]})})})
	fixture.has("[PLAN-BR-00] 宣言 organization_baseline.bootstrap_read", p.deny) with input as fixture.plan(valid_rcs) with data.policy_plan as d
}

test_violation_stack_set_missing if {
	fixture.has("[PLAN-BR-00] 宣言した立ち上げの読み取りの module.b.aws_cloudformation_stack_set.this", messages([organization, instances_with(root_target), valid_bootstrap_scp, protect(pattern), valid_plan_role]))
}

# 突き合わせる相手が無いまま緑にしない。
test_violation_plan_role_missing if {
	fixture.has("[PLAN-BR-00] 突き合わせる plan の role", messages([organization, stack_set_with({}, properties([trust], [read])), instances_with(root_target), valid_bootstrap_scp, protect(pattern)]))
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
	d := object.union(decl, {"organization_baseline": object.union(decl.organization_baseline, {"bootstrap_read": object.union(decl.organization_baseline.bootstrap_read, {"management_only_actions": ["organizations:DescribeOrganization", "s3:GetObject", "s3:ListBucket"]})})})
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
