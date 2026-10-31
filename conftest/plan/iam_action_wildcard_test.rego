package plan.iam_action_wildcard_test

import data.plan.fixture
import data.plan.iam_action_wildcard as p

test_violation_star if {
	fixture.has("[PLAN-IAM-01]", p.deny) with input as fixture.plan([fixture.role_policy([fixture.allow("*")])])
}

test_violation_service_star_in_array_uppercase if {
	fixture.has("[PLAN-IAM-01]", p.deny) with input as fixture.plan([fixture.role_policy([fixture.allow(["s3:GetObject", "IAM:*"])])])
}

test_violation_equivalent_wildcards if {
	every a in ["**", "s3:**", "iam:?*", "s3:*?"] {
		fixture.has("[PLAN-IAM-01]", p.deny) with input as fixture.plan([fixture.role_policy([fixture.allow(a)])])
	}
}

test_violation_user_and_group_policy if {
	user := fixture.doc("aws_iam_user_policy", "module.x.aws_iam_user_policy.this", "policy", {"Statement": [fixture.allow("*")]})
	group := fixture.doc("aws_iam_group_policy", "module.x.aws_iam_group_policy.this", "policy", {"Statement": [fixture.allow("*")]})
	count([m | some m in p.deny; startswith(m, "[PLAN-IAM-01]")]) == 2 with input as fixture.plan([user, group])
}

test_violation_not_action if {
	s := {"Effect": "Allow", "NotAction": "iam:*", "Resource": "*"}
	fixture.has("[PLAN-IAM-02]", p.deny) with input as fixture.plan([fixture.role_policy([s])])
}

test_violation_single_statement_object if {
	rc := fixture.doc("aws_iam_policy", "module.x.aws_iam_policy.this", "policy", {"Statement": fixture.allow("kms:*")})
	fixture.has("[PLAN-IAM-01]", p.deny) with input as fixture.plan([rc])
}

test_violation_trust_and_bucket_policy if {
	trust := fixture.doc("aws_iam_role", "module.x.aws_iam_role.this", "assume_role_policy", {"Statement": [fixture.allow("sts:*")]})
	bucket := fixture.doc("aws_s3_bucket_policy", "module.x.aws_s3_bucket_policy.this", "policy", {"Statement": [fixture.allow("s3:*")]})
	count([m | some m in p.deny; startswith(m, "[PLAN-IAM-01]")]) == 2 with input as fixture.plan([trust, bucket])
}

test_violation_no_documents if {
	rc := {"address": "module.x.aws_s3_bucket.this", "type": "aws_s3_bucket", "change": {"actions": ["create"], "after": {}, "after_unknown": {}}}
	fixture.has("[PLAN-IAM-00]", p.deny) with input as fixture.plan([rc])
}

test_pass_specific_actions if {
	count(p.deny) == 0 with input as fixture.plan([fixture.role_policy([fixture.allow(["s3:GetObject", "s3:PutObject"])])])
}

# ADR-0301 決定8 は Deny の wildcard と部分 wildcard を禁じない。
test_pass_deny_wildcard_and_partial_wildcard if {
	deny_all := {"Effect": "Deny", "Action": ["iam:*", "*"], "Resource": "*"}
	count(p.deny) == 0 with input as fixture.plan([fixture.role_policy([deny_all, fixture.allow(["s3:Get*", "s3:Put*", "s3:?etObject"])])])
}

test_violation_star_service_wildcard if {
	fixture.has("[PLAN-IAM-01]", p.deny) with input as fixture.plan([fixture.role_policy([fixture.allow("*:*")])])
}

test_pass_deny_not_action if {
	s := {"Effect": "Deny", "NotAction": "s3:GetObject", "Resource": "*"}
	count(p.deny) == 0 with input as fixture.plan([fixture.role_policy([s])])
}

test_pass_delete_only_change if {
	gone := {"address": "module.x.aws_iam_role_policy.old", "type": "aws_iam_role_policy", "change": {"actions": ["delete"], "after": null, "before": {"policy": json.marshal({"Statement": [fixture.allow("*")]})}, "after_unknown": {}}}
	count(p.deny) == 0 with input as fixture.plan([fixture.role_policy([fixture.allow("s3:GetObject")]), gone])
}
