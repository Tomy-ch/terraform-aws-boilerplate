package plan.organization_baseline_bootstrap_read_test

import data.plan.fixture
import data.plan.organization_baseline_bootstrap_read as p

plan_policy := "module.d.aws_iam_role_policy.this[\"plan\"]"

plan_role_address := "module.d.aws_iam_role.this[\"plan\"]"

decl := {
	"documents": {"aws_iam_role_policy": ["policy"], "aws_iam_policy": ["policy"]},
	"deployment_identity": {
		"module": "module.d.",
		"management_only_policies": ["module.d.aws_iam_policy.governance[0]"],
		"plan_role": {"role": plan_role_address, "policy": plan_policy, "managed_policies": ["module.d.aws_iam_policy.read"]},
		"apply_role": {"bootstrap": {
			"modules": ["module.s.", "module.d."],
			"types": {
				"aws_s3_bucket": {"class": "target"},
				"aws_kms_key": {"class": "key"},
				"aws_iam_role": {"class": "target"},
				"aws_iam_service_linked_role": {"class": "service_linked"},
			},
			"children": {},
			"prerequisite_module": "module.d.",
			"prerequisite_services": ["management.chatbot.amazonaws.com"],
		}},
	},
	"state_backend": {"module": "module.s.", "instances": {"sb": [member]}},
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

# PLAN-BR-07 のケースのほかは、テンプレートに常に足す（properties）。
member_key_read := {"Sid": "ReadStateKey", "Effect": "Allow", "Action": "kms:DescribeKey", "Resource": {"Fn::Sub": "arn:${AWS::Partition}:kms:${AWS::Region}:${AWS::AccountId}:key/*"}}

member_role_read := {"Sid": "ReadCiRoles", "Effect": "Allow", "Action": "iam:GetRole", "Resource": {"Fn::Sub": "arn:${AWS::Partition}:iam::${AWS::AccountId}:role/tabp-*-ci-*"}}

member_slr_read := {"Sid": "ReadServiceLinkedRoles", "Effect": "Allow", "Action": "iam:GetRole", "Resource": {"Fn::Sub": "arn:${AWS::Partition}:iam::${AWS::AccountId}:role/aws-service-role/management.chatbot.amazonaws.com/AWSServiceRoleForAWSChatbot"}}

member_reads := [member_key_read, member_role_read, member_slr_read]

properties(trusts, reads) := properties_with(trusts, array.concat(reads, member_reads))

properties_with(trusts, reads) := {
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

instances_with(target) := instances_in(target, ["ap-northeast-1"])

instances_in(target, regions) := change("module.b.aws_cloudformation_stack_instances.this", "aws_cloudformation_stack_instances", {"stack_set_name": "tabp-bootstrap-read", "deployment_targets": [target], "regions": regions})

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
	msgs := p.deny with input as array.concat(fixture.plan(rs), member_files(member_sb, member_di)) with data.policy_plan as decl
}

# --- メンバーの形の instance（reference の security）。state-backend と deployment-identity の側は別の root のファイルに在る ---

member := {"name": "security", "state_backend": "module.s_m.", "deployment_identity": "module.d_m.", "deployment_identity_root": "di"}

member_account := "111111111111"

bucket_arn := "arn:aws:s3:::tabp-security-tfstate-111111111111-ap-northeast-1-an"

key_arn := "arn:aws:kms:ap-northeast-1:111111111111:key/1234abcd-12ab-34cd-56ef-1234567890ab"

role_arn := "arn:aws:iam::111111111111:role/tabp-security-ci-plan"

slr_arn := "arn:aws:iam::111111111111:role/aws-service-role/management.chatbot.amazonaws.com/AWSServiceRoleForAWSChatbot"

member_bucket := change("module.s_m.module.s3.aws_s3_bucket.this", "aws_s3_bucket", {"arn": bucket_arn})

member_key := change("module.s_m.module.kms.aws_kms_key.this", "aws_kms_key", {"arn": key_arn})

member_role := change("module.d_m.aws_iam_role.this[\"plan\"]", "aws_iam_role", {"arn": role_arn, "name": "tabp-security-ci-plan"})

member_slr := change("module.d_m.aws_iam_service_linked_role.chatbot[0]", "aws_iam_service_linked_role", {"arn": slr_arn, "aws_service_name": "management.chatbot.amazonaws.com"})

# メンバーの plan の role の inline policy と、付けた customer managed policy（Chatbot の SLR の読みは managed の側に在る）。
member_plan_statements := [
	{"Sid": "ListBuckets", "Effect": "Allow", "Action": "s3:ListBucket", "Resource": "arn:aws:s3:::tabp-security-tfstate-111111111111-*"},
	{"Sid": "ReadStateKey", "Effect": "Allow", "Action": "kms:DescribeKey", "Resource": "arn:aws:kms:ap-northeast-1:111111111111:key/*"},
	{"Sid": "ListKeyAliases", "Effect": "Allow", "Action": "kms:ListAliases", "Resource": "*"},
	{"Sid": "ReadCiRoles", "Effect": "Allow", "Action": "iam:GetRole", "Resource": "arn:aws:iam::111111111111:role/tabp-security-ci-*"},
]

member_plan_policy(statements) := fixture.doc("aws_iam_role_policy", "module.d_m.aws_iam_role_policy.this[\"plan\"]", "policy", {"Statement": statements})

member_managed_policy(statements) := fixture.doc("aws_iam_policy", "module.d_m.aws_iam_policy.read", "policy", {"Statement": statements})

member_slr_statement := {"Sid": "ReadChatbotServiceLinkedRole", "Effect": "Allow", "Action": "iam:GetRole", "Resource": slr_arn}

member_sb := [member_bucket, member_key]

member_di := [member_role, member_slr, member_plan_policy(member_plan_statements), member_managed_policy([member_slr_statement])]

member_files(sb, di) := [
	{"path": "sb.json", "contents": {"plan_format_version": "1.2", "resource_changes": sb}},
	{"path": "di.json", "contents": {"plan_format_version": "1.2", "resource_changes": di}},
]

valid_stack_set := stack_set_with({}, properties([trust], [read]))

member_messages(set, inst, sb, di) := msgs if {
	msgs := p.deny with input as array.concat(fixture.plan(rcs(set, inst, valid_bootstrap_scp, protect(pattern), valid_plan_role)), member_files(sb, di)) with data.policy_plan as decl
}

not_covered(x, a) := sprintf("[PLAN-BR-07] instance security の plan の role が構成物 %s に許す %s を、立ち上げの読み取りの role がその ARN に許しません", [x, a])

br07(msgs) := {m | some m in msgs; startswith(m, "[PLAN-BR-07]")}

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
		{"Sid": "ReadPolicies", "Effect": "Allow", "Action": "iam:GetPolicy", "Resource": "*"},
	])
	fixture.has("[PLAN-BR-04] plan の role の iam:getpolicy", messages(rcs(stack_set_with({}, properties([trust], [read])), instances_with(root_target), valid_bootstrap_scp, protect(pattern), r)))
}

# plan の role の読みを持つ customer managed policy も突き合わせる（deployment-identity/ADR-0201 決定21）。
managed_address := "module.d.aws_iam_policy.governance_read[0]"

with_managed := object.union(decl, {
	"organization_baseline": object.union(decl.organization_baseline, {"bootstrap_read": object.union(decl.organization_baseline.bootstrap_read, {"plan_managed_policies": [managed_address]})}),
})

managed_policy(statements) := fixture.doc("aws_iam_policy", managed_address, "policy", {"Statement": statements})

inline_without_organization := plan_role([
	{"Sid": "ListBuckets", "Effect": "Allow", "Action": "s3:ListBucket", "Resource": "arn:aws:s3:::b"},
	{"Sid": "ReadState", "Effect": "Allow", "Action": "s3:GetObject", "Resource": "arn:aws:s3:::b/k"},
])

managed_messages(policy) := msgs if {
	rs := array.concat(rcs(stack_set_with({}, properties([trust], [read])), instances_with(root_target), valid_bootstrap_scp, protect(pattern), inline_without_organization), [policy])
	msgs := p.deny with input as array.concat(fixture.plan(rs), member_files(member_sb, member_di)) with data.policy_plan as with_managed
}

test_pass_management_only_in_managed_policy if {
	count(managed_messages(managed_policy([{"Sid": "ReadOrganization", "Effect": "Allow", "Action": "organizations:DescribeOrganization", "Resource": "*"}]))) == 0
}

test_violation_managed_policy_read_not_covered if {
	m := managed_policy([
		{"Sid": "ReadOrganization", "Effect": "Allow", "Action": "organizations:DescribeOrganization", "Resource": "*"},
		{"Sid": "ReadPolicies", "Effect": "Allow", "Action": "iam:GetPolicy", "Resource": "*"},
	])
	fixture.has("[PLAN-BR-04] plan の role の iam:getpolicy", managed_messages(m))
}

test_violation_managed_policy_missing if {
	msgs := p.deny with input as array.concat(fixture.plan(valid_rcs), member_files(member_sb, member_di)) with data.policy_plan as with_managed
	fixture.has("[PLAN-BR-00] 宣言 plan_managed_policies の module.d.aws_iam_policy.governance_read[0]", msgs)
}

test_violation_managed_policies_not_array if {
	d := object.union(decl, {"organization_baseline": object.union(decl.organization_baseline, {"bootstrap_read": object.union(decl.organization_baseline.bootstrap_read, {"plan_managed_policies": managed_address})})})
	fixture.has("[PLAN-BR-00] 宣言 organization_baseline.bootstrap_read", p.deny) with input as fixture.plan(valid_rcs) with data.policy_plan as d
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

# --- PLAN-BR-07: メンバーの構成物の ARN ごとの読み ---

test_pass_member_resources_covered if {
	count(member_messages(valid_stack_set, instances_with(root_target), member_sb, member_di)) == 0
}

test_violation_service_linked_role_not_covered if {
	set := stack_set_with({}, properties_with([trust], [read, member_key_read, member_role_read]))
	br07(member_messages(set, instances_with(root_target), member_sb, member_di)) == {not_covered(slr_arn, "iam:getrole")}
}

test_violation_action_on_other_statement if {
	other := object.union(member_slr_read, {"Action": "iam:GetPolicy"})
	set := stack_set_with({}, properties_with([trust], [read, member_key_read, member_role_read, other]))
	fixture.has(not_covered(slr_arn, "iam:getrole"), member_messages(set, instances_with(root_target), member_sb, member_di))
}

# pattern を管理アカウントの env に狭めると、メンバーの env の名前に当たらない。
test_violation_env_narrowed_pattern if {
	narrowed := object.union(member_role_read, {"Resource": {"Fn::Sub": "arn:${AWS::Partition}:iam::${AWS::AccountId}:role/tabp-admin-ci-*"}})
	set := stack_set_with({}, properties_with([trust], [read, member_key_read, narrowed, member_slr_read]))
	fixture.has(not_covered(role_arn, "iam:getrole"), member_messages(set, instances_with(root_target), member_sb, member_di))
}

# 正規の instance（管理アカウントの形）の構成物は照らさない。管理アカウントにだけ在る policy の読みを要求しない。
test_pass_canonical_resources_ignored if {
	canonical_role := change("module.d.aws_iam_role.this[\"governance\"]", "aws_iam_role", {"arn": "arn:aws:iam::111111111111:role/tabp-admin-governance"})
	msgs := p.deny with input as array.concat(fixture.plan(array.concat(valid_rcs, [canonical_role])), member_files(member_sb, member_di)) with data.policy_plan as decl
	count(msgs) == 0
}

test_pass_management_only_action_subtracted if {
	di := [member_role, member_slr, member_plan_policy(array.concat(member_plan_statements, [{"Sid": "State", "Effect": "Allow", "Action": "s3:GetObject", "Resource": "arn:aws:s3:::tabp-security-tfstate-111111111111-*"}])), member_managed_policy([member_slr_statement])]
	count(br07(member_messages(valid_stack_set, instances_with(root_target), member_sb, di))) == 0
}

# Resource "*" の文（kms:ListAliases）は特定の ARN への認可ではない。鍵の ARN に要求しない。
test_pass_resource_wildcard_statement_excluded if {
	di := [member_role, member_slr, member_plan_policy(array.concat(member_plan_statements, [{"Sid": "Wild", "Effect": "Allow", "Action": "kms:GetKeyPolicy", "Resource": "*"}])), member_managed_policy([member_slr_statement])]
	count(br07(member_messages(valid_stack_set, instances_with(root_target), member_sb, di))) == 0
}

# ${AWS::AccountId} は IAM と KMS の ARN のアカウントで置き換える。Bucket の名前の中のアカウントが違えば当たらない。
test_violation_bucket_other_account if {
	other := change("module.s_m.module.s3.aws_s3_bucket.this", "aws_s3_bucket", {"arn": "arn:aws:s3:::tabp-security-tfstate-222222222222-ap-northeast-1-an"})
	di := [member_role, member_slr, member_plan_policy(array.concat(member_plan_statements, [{"Sid": "Other", "Effect": "Allow", "Action": "s3:ListBucket", "Resource": "arn:aws:s3:::tabp-security-tfstate-222222222222-*"}])), member_managed_policy([member_slr_statement])]
	fixture.has(not_covered("arn:aws:s3:::tabp-security-tfstate-222222222222-ap-northeast-1-an", "s3:listbucket"), member_messages(valid_stack_set, instances_with(root_target), [other, member_key], di))
}

# ${AWS::Region} は stack instance の region で置き換える。鍵の region が違えば当たらない。
test_violation_stack_instance_region if {
	fixture.has(not_covered(key_arn, "kms:describekey"), member_messages(valid_stack_set, instances_in(root_target, ["us-west-2"]), member_sb, member_di))
}

test_violation_stack_instance_regions_not_single if {
	every regions in [[], ["ap-northeast-1", "us-west-2"]] {
		fixture.has("[PLAN-BR-00] 立ち上げの読み取りの stack instance の region", member_messages(valid_stack_set, instances_in(root_target, regions), member_sb, member_di))
	}
}

test_violation_member_instance_missing if {
	msgs := p.deny with input as fixture.plan(valid_rcs) with data.policy_plan as decl
	fixture.has("[PLAN-BR-00] 入力に、構成物の ARN を照らすメンバーの形の instance", msgs)
}

test_violation_member_instance_management_shape if {
	d := json.patch(decl, [{"op": "add", "path": "/deployment_identity/management_only_policies", "value": ["module.d.aws_iam_policy.governance[0]"]}])
	governance := change("module.d_m.aws_iam_policy.governance[0]", "aws_iam_policy", {"arn": "arn:aws:iam::111111111111:policy/tabp-security-ci-apply-identity-governance"})
	msgs := p.deny with input as array.concat(fixture.plan(valid_rcs), member_files(member_sb, array.concat(member_di, [governance]))) with data.policy_plan as d
	fixture.has("[PLAN-BR-00] instance security が管理アカウントの形です", msgs)
}

test_violation_member_side_missing if {
	fixture.has("[PLAN-BR-00] instance security の state_backend の側に", member_messages(valid_stack_set, instances_with(root_target), [], member_di))
	fixture.has("[PLAN-BR-00] instance security の deployment_identity の側に", member_messages(valid_stack_set, instances_with(root_target), member_sb, [member_plan_policy(member_plan_statements)]))
}

test_violation_member_arn_not_concrete if {
	every after in [{}, {"arn": null}, {"arn": "arn:aws:s3:::tabp-security-tfstate-*"}, {"arn": "${x}"}] {
		bucket := change("module.s_m.module.s3.aws_s3_bucket.this", "aws_s3_bucket", after)
		fixture.has("[PLAN-BR-00] instance security の構成物 module.s_m.module.s3.aws_s3_bucket.this の ARN", member_messages(valid_stack_set, instances_with(root_target), [bucket, member_key], member_di))
	}
}

test_violation_member_accounts_ambiguous if {
	key := change("module.s_m.module.kms.aws_kms_key.this", "aws_kms_key", {"arn": "arn:aws:kms:ap-northeast-1:222222222222:key/1234abcd-12ab-34cd-56ef-1234567890ab"})
	fixture.has("[PLAN-BR-00] instance security の構成物の IAM と KMS の ARN のアカウント", member_messages(valid_stack_set, instances_with(root_target), [member_bucket, key], member_di))
}

test_violation_plan_role_grants_nothing if {
	statements := [s | some s in member_plan_statements; s.Sid != "ReadStateKey"]
	di := [member_role, member_slr, member_plan_policy(statements), member_managed_policy([member_slr_statement])]
	fixture.has(sprintf("[PLAN-BR-00] instance security の plan の role が、構成物 %s に Resource を限った読みを1つも許していません", [key_arn]), member_messages(valid_stack_set, instances_with(root_target), member_sb, di))
}

# NotResource の Allow は、どの pattern も当たらない ARN の読みを許す。
test_violation_plan_role_not_resource if {
	nr := {"Sid": "NotResource", "Effect": "Allow", "Action": "iam:ListRoleTags", "NotResource": "arn:aws:iam::111111111111:role/other"}
	di := [member_role, member_slr, member_plan_policy(array.concat(member_plan_statements, [nr])), member_managed_policy([member_slr_statement])]
	fixture.has(not_covered(role_arn, "iam:listroletags"), member_messages(valid_stack_set, instances_with(root_target), member_sb, di))
	excluded := object.union(nr, {"NotResource": role_arn})
	di2 := [member_role, member_slr, member_plan_policy(array.concat(member_plan_statements, [excluded])), member_managed_policy([member_slr_statement])]
	not fixture.has(not_covered(role_arn, "iam:listroletags"), member_messages(valid_stack_set, instances_with(root_target), member_sb, di2))
}

test_violation_unknown_substitution_not_counted if {
	odd := object.union(member_slr_read, {"Resource": {"Fn::Sub": "arn:${AWS::Partition}:iam::${AWS::AccountId}:role/${Other}"}})
	set := stack_set_with({}, properties_with([trust], [read, member_key_read, member_role_read, odd]))
	fixture.has(not_covered(slr_arn, "iam:getrole"), member_messages(set, instances_with(root_target), member_sb, member_di))
}

test_violation_management_only_policies_missing if {
	d := json.patch(decl, [{"op": "remove", "path": "/deployment_identity/management_only_policies"}])
	msgs := p.deny with input as array.concat(fixture.plan(valid_rcs), member_files(member_sb, member_di)) with data.policy_plan as d
	fixture.has("[PLAN-BR-00] 宣言 deployment_identity.management_only_policies が無いか空です", msgs)
}

# Resource に "*" と ARN が混ざる文は、ARN の要素で要求する。
test_violation_mixed_wildcard_resource_still_requires if {
	mixed := {"Sid": "Mixed", "Effect": "Allow", "Action": "iam:ListRoleTags", "Resource": ["*", "arn:aws:iam::111111111111:role/tabp-security-ci-*"]}
	di := [member_role, member_slr, member_plan_policy(array.concat(member_plan_statements, [mixed])), member_managed_policy([member_slr_statement])]
	fixture.has(not_covered(role_arn, "iam:listroletags"), member_messages(valid_stack_set, instances_with(root_target), member_sb, di))
}

test_violation_stack_instance_regions_absent if {
	inst := change("module.b.aws_cloudformation_stack_instances.this", "aws_cloudformation_stack_instances", {"stack_set_name": "tabp-bootstrap-read", "deployment_targets": [root_target]})
	fixture.has("[PLAN-BR-00] 立ち上げの読み取りの stack instance の region []", member_messages(valid_stack_set, inst, member_sb, member_di))
}

test_violation_member_accounts_none if {
	bucket_only := [member_bucket]
	di := [member_plan_policy(member_plan_statements), member_slr_policy_only]
	fixture.has("[PLAN-BR-00] instance security の構成物の IAM と KMS の ARN のアカウント [] が", member_messages(valid_stack_set, instances_with(root_target), bucket_only, di))
}

member_slr_policy_only := member_managed_policy([member_slr_statement])
