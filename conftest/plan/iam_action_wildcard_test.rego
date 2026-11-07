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
	count(p.deny) == 0 with input as fixture.plan([fixture.role_policy([fixture.allow(["s3:GetObject", "s3:PutObject"])])]) with data.policy_plan.resource_wildcard_actions as []
}

# ADR-0301 決定8 は Deny の wildcard と部分 wildcard を禁じない。
test_pass_deny_wildcard_and_partial_wildcard if {
	deny_all := {"Effect": "Deny", "Action": ["iam:*", "*"], "Resource": "*"}
	count(p.deny) == 0 with input as fixture.plan([fixture.role_policy([deny_all, fixture.allow(["s3:Get*", "s3:Put*", "s3:?etObject"])])]) with data.policy_plan.resource_wildcard_actions as []
}

test_violation_resource_wildcard_outside_list if {
	s := {"Effect": "Allow", "Action": ["kms:ListAliases", "kms:DescribeKey"], "Resource": "*"}
	msgs := p.deny with input as fixture.plan([fixture.role_policy([s])]) with data.policy_plan.resource_wildcard_actions as ["kms:ListAliases"]
	fixture.has("[PLAN-IAM-03]", msgs)
	count([m | some m in msgs; startswith(m, "[PLAN-IAM-03]")]) == 1
}

test_violation_resource_wildcard_in_array if {
	s := {"Effect": "Allow", "Action": "s3:GetObject", "Resource": ["arn:aws:s3:::b/*", "*"]}
	fixture.has("[PLAN-IAM-03]", p.deny) with input as fixture.plan([fixture.role_policy([s])]) with data.policy_plan.resource_wildcard_actions as ["kms:CreateKey"]
}

test_violation_resource_wildcard_without_declaration if {
	s := {"Effect": "Allow", "Action": "kms:CreateKey", "Resource": "*"}
	fixture.has("[PLAN-IAM-03]", p.deny) with input as fixture.plan([fixture.role_policy([s])]) with data.policy_plan as object.remove(data.policy_plan, ["resource_wildcard_actions"])
}

# 宣言は大文字小文字を区別しない。Deny と resource-based policy の "*" は見ない。
test_pass_resource_wildcard if {
	allow := {"Effect": "Allow", "Action": "KMS:CreateKey", "Resource": "*", "Condition": {"StringEquals": {"kms:KeySpec": "SYMMETRIC_DEFAULT"}}}
	deny := {"Effect": "Deny", "Action": "kms:PutKeyPolicy", "Resource": "*"}
	key_policy := fixture.doc("aws_kms_key_policy", "module.x.aws_kms_key_policy.this", "policy", {"Statement": [{"Effect": "Allow", "Principal": {"AWS": "arn:aws:iam::123456789012:root"}, "Action": "kms:DescribeKey", "Resource": "*"}]})
	bucket := fixture.doc("aws_s3_bucket_policy", "module.x.aws_s3_bucket_policy.this", "policy", {"Statement": [{"Effect": "Allow", "Action": "s3:GetObject", "Resource": "*"}]})
	count(p.deny) == 0 with input as fixture.plan([fixture.role_policy([allow, deny]), key_policy, bucket]) with data.policy_plan.resource_wildcard_actions as ["kms:createkey"]
}

test_violation_kms_crypto_action if {
	every a in ["kms:Decrypt", "KMS:GenerateDataKey", "kms:ReEncryptFrom", "kms:CreateGrant", "kms:Gen*", "kms:?ecrypt", "kms:Get*"] {
		fixture.has("[PLAN-IAM-04]", p.deny) with input as fixture.plan([fixture.role_policy([fixture.allow(a)])])
	}
}

test_violation_kms_crypto_action_in_managed_policy if {
	rc := fixture.doc("aws_iam_policy", "module.x.aws_iam_policy.this", "policy", {"Statement": [fixture.allow("kms:Encrypt")]})
	fixture.has("[PLAN-IAM-04]", p.deny) with input as fixture.plan([rc])
}

# 鍵の管理の Action、Deny の暗号操作、Key Policy の暗号操作は見ない。
test_pass_kms_without_crypto_in_identity_policy if {
	manage := fixture.allow(["kms:DescribeKey", "kms:PutKeyPolicy", "kms:GetKeyPolicy", "kms:List*"])
	deny := {"Effect": "Deny", "Action": "kms:Decrypt", "Resource": "*"}
	key_policy := fixture.doc("aws_kms_key_policy", "module.x.aws_kms_key_policy.this", "policy", {"Statement": [{"Effect": "Allow", "Principal": {"AWS": "arn:aws:iam::123456789012:role/r"}, "Action": "kms:Decrypt", "Resource": "*"}]})
	count(p.deny) == 0 with input as fixture.plan([fixture.role_policy([manage, deny]), key_policy]) with data.policy_plan.resource_wildcard_actions as []
}

test_violation_user_and_group_policy_resource_wildcard_and_crypto if {
	s := {"Effect": "Allow", "Action": ["kms:Decrypt", "s3:GetObject"], "Resource": "*"}
	user := fixture.doc("aws_iam_user_policy", "module.x.aws_iam_user_policy.this", "policy", {"Statement": [s]})
	group := fixture.doc("aws_iam_group_policy", "module.x.aws_iam_group_policy.this", "policy", {"Statement": [s]})
	msgs := p.deny with input as fixture.plan([user, group]) with data.policy_plan.resource_wildcard_actions as []
	count([m | some m in msgs; startswith(m, "[PLAN-IAM-03]")]) == 4
	count([m | some m in msgs; startswith(m, "[PLAN-IAM-04]")]) == 2
}

test_violation_not_resource if {
	s := {"Effect": "Allow", "Action": "kms:DescribeKey", "NotResource": "arn:aws:kms:ap-northeast-1:123456789012:key/x"}
	fixture.has("[PLAN-IAM-05]", p.deny) with input as fixture.plan([fixture.role_policy([s])]) with data.policy_plan.resource_wildcard_actions as []
}

test_pass_deny_not_resource if {
	s := {"Effect": "Deny", "Action": "s3:DeleteObject", "NotResource": "arn:aws:s3:::b/keep/*"}
	count(p.deny) == 0 with input as fixture.plan([fixture.role_policy([s, fixture.allow("s3:GetObject")])]) with data.policy_plan.resource_wildcard_actions as []
}

test_violation_unused_resource_wildcard_declaration if {
	s := {"Effect": "Allow", "Action": "kms:CreateKey", "Resource": "*"}
	msgs := p.deny with input as fixture.plan([fixture.role_policy([s])]) with data.policy_plan.resource_wildcard_actions as ["kms:CreateKey", "kms:ListAliases"]
	fixture.has("[PLAN-IAM-06] data.policy_plan.resource_wildcard_actions の kms:listaliases ", msgs)
	not fixture.has("[PLAN-IAM-06] data.policy_plan.resource_wildcard_actions の kms:createkey ", msgs)
}

test_violation_star_service_wildcard if {
	fixture.has("[PLAN-IAM-01]", p.deny) with input as fixture.plan([fixture.role_policy([fixture.allow("*:*")])])
}

test_pass_deny_not_action if {
	s := {"Effect": "Deny", "NotAction": "s3:GetObject", "Resource": "*"}
	count(p.deny) == 0 with input as fixture.plan([fixture.role_policy([s])]) with data.policy_plan.resource_wildcard_actions as []
}

test_pass_delete_only_change if {
	gone := {"address": "module.x.aws_iam_role_policy.old", "type": "aws_iam_role_policy", "change": {"actions": ["delete"], "after": null, "before": {"policy": json.marshal({"Statement": [fixture.allow("*")]})}, "after_unknown": {}}}
	count(p.deny) == 0 with input as fixture.plan([fixture.role_policy([fixture.allow("s3:GetObject")]), gone]) with data.policy_plan.resource_wildcard_actions as []
}
