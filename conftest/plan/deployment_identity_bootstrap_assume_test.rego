package plan.deployment_identity_bootstrap_assume_test

import data.plan.deployment_identity_bootstrap_assume as p
import data.plan.fixture

plan_policy := "module.deployment_identity.module.g.aws_iam_role_policy.this[\"plan\"]"

members_policy := "module.deployment_identity.module.g.aws_iam_policy.members[0]"

decl := {
	"documents": {"aws_iam_role_policy": ["policy"], "aws_iam_policy": ["policy"], "aws_iam_role": ["assume_role_policy"]},
	"deployment_identity": {
		"plan_role": {"policy": plan_policy},
		"bootstrap_assume": {"apply_identity_policy": members_policy},
	},
}

read_role := "arn:aws:iam::111111111111:role/bootstrap/tabp-bootstrap-read"

access_role := "arn:aws:iam::111111111111:role/OrganizationAccountAccessRole"

assume(resources) := {"Sid": "Assume", "Effect": "Allow", "Action": "sts:AssumeRole", "Resource": resources}

read := {"Sid": "ListBuckets", "Effect": "Allow", "Action": "s3:ListBucket", "Resource": "arn:aws:s3:::b"}

plan_doc(statements) := fixture.doc("aws_iam_role_policy", plan_policy, "policy", {"Statement": statements})

members_doc(statements) := fixture.doc("aws_iam_policy", members_policy, "policy", {"Statement": statements})

other_doc(address, statements) := fixture.doc("aws_iam_policy", address, "policy", {"Statement": statements})

valid := [plan_doc([read, assume([read_role])]), members_doc([assume([access_role])])]

messages(rs) := msgs if {
	msgs := p.deny with input as fixture.plan(rs) with data.policy_plan as decl
}

test_pass_reference_shape if {
	count(messages(valid)) == 0
}

test_pass_trust_policy_is_not_a_permission if {
	trust := fixture.doc("aws_iam_role", "module.deployment_identity.module.g.aws_iam_role.this[\"plan\"]", "assume_role_policy", {"Statement": [{"Effect": "Allow", "Action": "sts:AssumeRole", "Principal": {"AWS": "*"}}]})
	count(messages(array.concat(valid, [trust]))) == 0
}

test_pass_other_usecase_is_out_of_scope if {
	other := other_doc("module.ops_notification.aws_iam_policy.this", [assume(["arn:aws:iam::111111111111:role/x"])])
	count(messages(array.concat(valid, [other]))) == 0
}

test_violation_declaration_missing if {
	d := {"documents": decl.documents, "deployment_identity": {"plan_role": {"policy": plan_policy}}}
	fixture.has("[PLAN-BA-00] 宣言", p.deny) with input as fixture.plan(valid) with data.policy_plan as d
}

test_violation_declared_policy_absent if {
	fixture.has(sprintf("[PLAN-BA-00] 宣言した引き受けの policy %s", [members_policy]), messages([plan_doc([assume([read_role])])]))
}

test_violation_plan_assumes_access_role if {
	fixture.has("[PLAN-BA-01]", messages([plan_doc([assume([read_role, access_role])]), members_doc([assume([access_role])])]))
}

test_violation_plan_assume_wildcard_account if {
	fixture.has("[PLAN-BA-01]", messages([plan_doc([assume(["arn:aws:iam::*:role/bootstrap/tabp-bootstrap-read"])]), members_doc([assume([access_role])])]))
}

test_violation_plan_assume_other_path if {
	fixture.has("[PLAN-BA-01]", messages([plan_doc([assume(["arn:aws:iam::111111111111:role/tabp-bootstrap-read"])]), members_doc([assume([access_role])])]))
}

test_violation_plan_assume_action_wildcard if {
	s := object.union(assume([read_role]), {"Action": "sts:Assume*"})
	fixture.has("[PLAN-BA-01]", messages([plan_doc([s]), members_doc([assume([access_role])])]))
}

test_violation_plan_assume_with_other_action if {
	s := object.union(assume([read_role]), {"Action": ["sts:AssumeRole", "sts:TagSession"]})
	fixture.has("[PLAN-BA-01]", messages([plan_doc([s]), members_doc([assume([access_role])])]))
}

test_violation_plan_assume_without_resource if {
	fixture.has("[PLAN-BA-01]", messages([plan_doc([assume([])]), members_doc([assume([access_role])])]))
}

test_violation_apply_identity_assumes_other_role if {
	fixture.has("[PLAN-BA-02]", messages([plan_doc([assume([read_role])]), members_doc([assume([access_role, "arn:aws:iam::111111111111:role/x"])])]))
}

test_violation_apply_identity_assume_wildcard_account if {
	fixture.has("[PLAN-BA-02]", messages([plan_doc([assume([read_role])]), members_doc([assume(["arn:aws:iam::*:role/OrganizationAccountAccessRole"])])]))
}

test_violation_other_ci_policy_assumes if {
	other := other_doc("module.deployment_identity.module.g.aws_iam_policy.governance[0]", [assume([access_role])])
	fixture.has("[PLAN-BA-03] p.json: module.deployment_identity.module.g.aws_iam_policy.governance[0]", messages(array.concat(valid, [other])))
}

test_violation_other_ci_policy_service_wildcard if {
	other := other_doc("module.deployment_identity.module.g.aws_iam_policy.manage", [{"Effect": "Allow", "Action": "sts:*", "Resource": "*"}])
	fixture.has("[PLAN-BA-03]", messages(array.concat(valid, [other])))
}

test_violation_other_ci_policy_not_action if {
	other := other_doc("module.deployment_identity.module.g.aws_iam_policy.manage", [{"Effect": "Allow", "NotAction": "s3:*", "Resource": "*"}])
	fixture.has("[PLAN-BA-03]", messages(array.concat(valid, [other])))
}

test_pass_not_action_excluding_assume if {
	other := other_doc("module.deployment_identity.module.g.aws_iam_policy.manage", [{"Effect": "Allow", "NotAction": "sts:*", "Resource": "*"}])
	not fixture.has("[PLAN-BA-03]", messages(array.concat(valid, [other])))
}

test_pass_deny_is_not_an_assume if {
	other := other_doc("module.deployment_identity.module.g.aws_iam_policy.manage", [{"Effect": "Deny", "Action": "sts:AssumeRole", "Resource": "*"}])
	count(messages(array.concat(valid, [other]))) == 0
}

test_violation_accounts_differ if {
	fixture.has("[PLAN-BA-04]", messages([plan_doc([assume([read_role])]), members_doc([read])]))
}

test_violation_no_assume_in_reference if {
	fixture.has("[PLAN-BA-05]", messages([plan_doc([read]), members_doc([read])]))
}

test_violation_empty_input if {
	msgs := messages([])
	fixture.has(sprintf("[PLAN-BA-00] 宣言した引き受けの policy %s", [plan_policy]), msgs)
	fixture.has(sprintf("[PLAN-BA-00] 宣言した引き受けの policy %s", [members_policy]), msgs)
	fixture.has("[PLAN-BA-05]", msgs)
}

test_violation_assume_only_wildcard_accounts if {
	msgs := messages([plan_doc([assume(["arn:aws:iam::*:role/bootstrap/tabp-bootstrap-read"])]), members_doc([assume([access_role])])])
	fixture.has("[PLAN-BA-01]", msgs)
	fixture.has("[PLAN-BA-05]", msgs)
}

test_violation_declaration_shapes if {
	every d in [
		{"documents": decl.documents, "deployment_identity": {"bootstrap_assume": {"apply_identity_policy": members_policy}}},
		{"documents": decl.documents, "deployment_identity": {"plan_role": {"policy": ""}, "bootstrap_assume": {"apply_identity_policy": members_policy}}},
		{"documents": decl.documents, "deployment_identity": {"plan_role": {"policy": plan_policy}, "bootstrap_assume": {"apply_identity_policy": 1}}},
		{"documents": decl.documents},
	] {
		fixture.has("[PLAN-BA-00] 宣言", p.deny) with input as fixture.plan(valid) with data.policy_plan as d
	}
}

test_violation_declared_plan_policy_absent if {
	fixture.has(sprintf("[PLAN-BA-00] 宣言した引き受けの policy %s", [plan_policy]), messages([members_doc([assume([access_role])])]))
}

test_violation_accounts_differ_apply_identity_only if {
	fixture.has("[PLAN-BA-04]", messages([plan_doc([read]), members_doc([assume([access_role])])]))
}

test_violation_other_ci_policy_session_actions if {
	every action in ["sts:TagSession", "sts:SetSourceIdentity"] {
		other := other_doc("module.deployment_identity.module.g.aws_iam_policy.manage", [{"Effect": "Allow", "Action": action, "Resource": "*"}])
		fixture.has("[PLAN-BA-03]", messages(array.concat(valid, [other])))
	}
}

test_violation_plan_assume_with_tag_session if {
	s := object.union(assume([read_role]), {"Action": ["sts:AssumeRole", "sts:TagSession"]})
	fixture.has("[PLAN-BA-01]", messages([plan_doc([s]), members_doc([assume([access_role])])]))
}
