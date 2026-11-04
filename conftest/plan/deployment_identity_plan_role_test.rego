package plan.deployment_identity_plan_role_test

import data.plan.deployment_identity_plan_role as p
import data.plan.fixture

decl := {"plan_role_policy": "module.x.aws_iam_role_policy.this[\"plan\"]"}

plan_bucket := "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an"

write_output := {"Effect": "Allow", "Action": "s3:PutObject", "Resource": [sprintf("%s/pr/*", [plan_bucket]), sprintf("%s/branch/*", [plan_bucket])]}

test_pass_reads_and_plan_output_write if {
	statements := [fixture.allow(["s3:GetObject", "s3:ListBucket", "iam:GetRole", "ec2:DescribeVpcs", "s3:HeadObject"]), write_output]
	count(p.deny) == 0 with input as fixture.plan([fixture.role_policy(statements)]) with data.policy_plan.deployment_identity as decl
}

test_pass_deny_statement_is_not_checked if {
	deny := {"Effect": "Deny", "Action": "s3:DeleteObject", "Resource": "*"}
	count(p.deny) == 0 with input as fixture.plan([fixture.role_policy([deny, write_output])]) with data.policy_plan.deployment_identity as decl
}

test_pass_other_role_policy_is_not_checked if {
	apply := fixture.doc("aws_iam_role_policy", "module.x.aws_iam_role_policy.this[\"apply\"]", "policy", {"Statement": [fixture.allow("s3:DeleteObject")]})
	count(p.deny) == 0 with input as fixture.plan([fixture.role_policy([write_output]), apply]) with data.policy_plan.deployment_identity as decl
}

test_violation_plan_role_policy_missing if {
	fixture.has("[PLAN-ROLE-00]", p.deny) with input as fixture.plan([]) with data.policy_plan.deployment_identity as decl
}

test_violation_declaration_missing if {
	fixture.has("[PLAN-ROLE-00]", p.deny) with input as fixture.plan([fixture.role_policy([write_output])]) with data.policy_plan.deployment_identity as {}
}

test_violation_not_action if {
	s := {"Effect": "Allow", "NotAction": "s3:DeleteObject", "Resource": "*"}
	fixture.has("[PLAN-ROLE-01]", p.deny) with input as fixture.plan([fixture.role_policy([s])]) with data.policy_plan.deployment_identity as decl
}

test_violation_mutating_action if {
	every a in ["s3:DeleteObject", "iam:CreateRole", "S3:PUTBUCKETPOLICY", "*", "s3:*", "kms:Decrypt"] {
		fixture.has("[PLAN-ROLE-02]", p.deny) with input as fixture.plan([fixture.role_policy([fixture.allow(a)])]) with data.policy_plan.deployment_identity as decl
	}
}

test_violation_put_object_outside_plan_output_keys if {
	every r in ["*", "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/identity/terraform.tfstate", sprintf("%s/*", [plan_bucket]), sprintf("%s/pr/1/x.txt", [plan_bucket])] {
		s := {"Effect": "Allow", "Action": "s3:PutObject", "Resource": r}
		fixture.has("[PLAN-ROLE-03]", p.deny) with input as fixture.plan([fixture.role_policy([s])]) with data.policy_plan.deployment_identity as decl
	}
}
