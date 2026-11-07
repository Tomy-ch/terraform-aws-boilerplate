package plan.deployment_identity_plan_role_test

import data.plan.deployment_identity_plan_role as p
import data.plan.fixture

decl := {"plan_role": {"role": "module.x.aws_iam_role.this[\"plan\"]", "policy": "module.x.aws_iam_role_policy.this[\"plan\"]"}}

plan_bucket := "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an"

write_output := {"Effect": "Allow", "Action": "s3:PutObject", "Resource": [sprintf("%s/pr/*", [plan_bucket]), sprintf("%s/branch/*", [plan_bucket])]}

rc(type, address, after) := {"address": address, "type": type, "change": {"actions": ["create"], "after": after, "after_unknown": {}}}

role := rc("aws_iam_role", "module.x.aws_iam_role.this[\"plan\"]", {"name": "ci-plan"})

policies_exclusive := rc("aws_iam_role_policies_exclusive", "module.x.aws_iam_role_policies_exclusive.this[\"plan\"]", {"role_name": "ci-plan", "policy_names": ["plan"]})

attachments_exclusive := rc("aws_iam_role_policy_attachments_exclusive", "module.x.aws_iam_role_policy_attachments_exclusive.this[\"plan\"]", {"role_name": "ci-plan", "policy_arns": []})

plan_policy(statements) := object.union(fixture.role_policy(statements), {"change": {"after": object.union(fixture.role_policy(statements).change.after, {"name": "plan", "role": "ci-plan"})}})

with_role(statements, others) := fixture.plan(array.concat([role, policies_exclusive, attachments_exclusive, plan_policy(statements)], others))

test_pass_reads_and_plan_output_write if {
	statements := [fixture.allow(["s3:GetObject", "s3:ListBucket", "iam:GetRole", "ec2:DescribeVpcs", "s3:HeadObject", "kms:Decrypt"]), write_output]
	count(p.deny) == 0 with input as with_role(statements, []) with data.policy_plan.deployment_identity as decl
}

test_pass_deny_statement_is_not_checked if {
	deny := {"Effect": "Deny", "Action": "s3:DeleteObject", "Resource": "*"}
	count(p.deny) == 0 with input as with_role([deny, write_output], []) with data.policy_plan.deployment_identity as decl
}

test_pass_other_role_policy_is_not_checked if {
	apply := fixture.doc("aws_iam_role_policy", "module.x.aws_iam_role_policy.this[\"apply\"]", "policy", {"Statement": [fixture.allow("s3:DeleteObject")]})
	apply_on_apply := object.union(apply, {"change": {"after": object.union(apply.change.after, {"role": "ci-apply"})}})
	count(p.deny) == 0 with input as with_role([write_output], [apply_on_apply]) with data.policy_plan.deployment_identity as decl
}

test_violation_plan_role_policy_missing if {
	fixture.has("[PLAN-ROLE-00]", p.deny) with input as fixture.plan([role, policies_exclusive, attachments_exclusive]) with data.policy_plan.deployment_identity as decl
}

fixture_only := fixture.plan([plan_policy([write_output])])

test_violation_plan_role_missing if {
	fixture.has("[PLAN-ROLE-00]", p.deny) with input as fixture.plan([plan_policy([write_output])]) with data.policy_plan.deployment_identity as decl
}

test_violation_declaration_missing if {
	fixture.has("[PLAN-ROLE-00]", p.deny) with input as with_role([write_output], []) with data.policy_plan.deployment_identity as {}
}

test_violation_not_action if {
	s := {"Effect": "Allow", "NotAction": "s3:DeleteObject", "Resource": "*"}
	fixture.has("[PLAN-ROLE-01]", p.deny) with input as with_role([s], []) with data.policy_plan.deployment_identity as decl
}

test_violation_not_resource if {
	s := {"Effect": "Allow", "Action": "s3:PutObject", "NotResource": "arn:aws:s3:::x/*"}
	fixture.has("[PLAN-ROLE-01]", p.deny) with input as with_role([s], []) with data.policy_plan.deployment_identity as decl
}

test_violation_mutating_action if {
	every a in ["s3:DeleteObject", "iam:CreateRole", "S3:PUTBUCKETPOLICY", "*", "s3:*", "s3:Put*", "kms:Encrypt", "kms:GenerateDataKey", ["s3:GetObject", "s3:DeleteObject"]] {
		fixture.has("[PLAN-ROLE-02]", p.deny) with input as with_role([fixture.allow(a)], []) with data.policy_plan.deployment_identity as decl
	}
}

test_violation_put_object_outside_plan_output_keys if {
	every r in [
		"*",
		"arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/identity/terraform.tfstate",
		sprintf("%s/*", [plan_bucket]),
		sprintf("%s/pr/1/x.txt", [plan_bucket]),
		"arn:aws:s3:::*-tfplan-*/pr/*",
		sprintf("%s-*/pr/*", [plan_bucket]),
	] {
		s := {"Effect": "Allow", "Action": "s3:PutObject", "Resource": r}
		fixture.has("[PLAN-ROLE-03]", p.deny) with input as with_role([s], []) with data.policy_plan.deployment_identity as decl
	}
}

test_violation_put_object_without_resource if {
	s := {"Effect": "Allow", "Action": "s3:PutObject"}
	fixture.has("[PLAN-ROLE-03]", p.deny) with input as with_role([s], []) with data.policy_plan.deployment_identity as decl
}

test_violation_extra_policy_on_plan_role if {
	extra := rc("aws_iam_role_policy", "module.x.aws_iam_role_policy.extra", {"role": "ci-plan", "policy": "{}"})
	attachment := rc("aws_iam_role_policy_attachment", "module.x.aws_iam_role_policy_attachment.admin", {"role": "ci-plan", "policy_arn": "arn:aws:iam::aws:policy/AdministratorAccess"})
	every other in [extra, attachment] {
		fixture.has("[PLAN-ROLE-04]", p.deny) with input as with_role([write_output], [other]) with data.policy_plan.deployment_identity as decl
	}
}

test_violation_unknown_role_of_policy if {
	extra := {"address": "module.x.aws_iam_role_policy.extra", "type": "aws_iam_role_policy", "change": {"actions": ["create"], "after": {"policy": "{}"}, "after_unknown": {"role": true}}}
	fixture.has("[PLAN-ROLE-04]", p.deny) with input as with_role([write_output], [extra]) with data.policy_plan.deployment_identity as decl
}

test_violation_exclusive_missing if {
	every rcs in [[role, policies_exclusive, plan_policy([write_output])], [role, attachments_exclusive, plan_policy([write_output])]] {
		fixture.has("[PLAN-ROLE-04]", p.deny) with input as fixture.plan(rcs) with data.policy_plan.deployment_identity as decl
	}
}

test_violation_exclusive_allows_more if {
	names := rc("aws_iam_role_policies_exclusive", "module.x.aws_iam_role_policies_exclusive.this[\"plan\"]", {"role_name": "ci-plan", "policy_names": ["plan", "extra"]})
	arns := rc("aws_iam_role_policy_attachments_exclusive", "module.x.aws_iam_role_policy_attachments_exclusive.this[\"plan\"]", {"role_name": "ci-plan", "policy_arns": ["arn:aws:iam::aws:policy/AdministratorAccess"]})
	every rcs in [[role, names, attachments_exclusive, plan_policy([write_output])], [role, policies_exclusive, arns, plan_policy([write_output])]] {
		fixture.has("[PLAN-ROLE-04]", p.deny) with input as fixture.plan(rcs) with data.policy_plan.deployment_identity as decl
	}
}
