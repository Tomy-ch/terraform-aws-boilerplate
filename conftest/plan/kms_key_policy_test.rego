package plan.kms_key_policy_test

import data.plan.fixture
import data.plan.kms_key_policy as p

root := {"Effect": "Allow", "Principal": {"AWS": "arn:aws:iam::123456789012:root"}, "Action": ["kms:DescribeKey", "kms:PutKeyPolicy"], "Resource": "*"}

via_s3 := {
	"Effect": "Allow",
	"Principal": {"AWS": ["arn:aws:iam::123456789012:role/a", "arn:aws:iam::123456789012:role/b"]},
	"Action": ["kms:Decrypt", "kms:GenerateDataKey"],
	"Resource": "*",
	"Condition": {"StringEquals": {"kms:ViaService": "s3.ap-northeast-1.amazonaws.com"}},
}

logs := {
	"Effect": "Allow",
	"Principal": {"Service": "logs.ap-northeast-1.amazonaws.com"},
	"Action": ["kms:Decrypt", "kms:Encrypt"],
	"Resource": "*",
	"Condition": {"ArnEquals": {"kms:EncryptionContext:aws:logs:arn": ["arn:aws:logs:ap-northeast-1:123456789012:log-group:/x"]}},
}

key_policy(statements) := fixture.doc("aws_kms_key_policy", "module.kms.aws_kms_key_policy.this", "policy", {"Version": "2012-10-17", "Statement": statements})

test_pass_module_shape if {
	count(p.deny) == 0 with input as fixture.plan([key_policy([root, via_s3, logs])])
}

# 文書ごとに数えるので、Key Policy の無い入力でも、Key Policy が2つの入力でも何も言わない。
test_pass_no_key_policy if {
	count(p.deny) == 0 with input as fixture.plan([fixture.role_policy([fixture.allow("s3:GetObject")])])
}

test_pass_two_key_policies if {
	other := fixture.doc("aws_kms_key_policy", "module.other.aws_kms_key_policy.this", "policy", {"Statement": [root, logs]})
	count(p.deny) == 0 with input as fixture.plan([key_policy([root, via_s3]), other])
}

test_pass_condition_keys_case_insensitive if {
	s := json.patch(via_s3, [{"op": "replace", "path": "/Condition", "value": {"StringEquals": {"KMS:VIASERVICE": "s3.ap-northeast-1.amazonaws.com"}}}])
	count(p.deny) == 0 with input as fixture.plan([key_policy([root, s])])
}

test_violation_star_principal if {
	s := {"Effect": "Allow", "Principal": "*", "Action": "kms:Decrypt", "Resource": "*"}
	fixture.has("[PLAN-KMS-01]", p.deny) with input as fixture.plan([key_policy([root, s])])
}

test_violation_star_in_aws_principals if {
	s := json.patch(via_s3, [{"op": "replace", "path": "/Principal/AWS", "value": ["arn:aws:iam::123456789012:role/a", "*"]}])
	fixture.has("[PLAN-KMS-01]", p.deny) with input as fixture.plan([key_policy([root, s])])
}

test_violation_no_account_delegation if {
	fixture.has("[PLAN-KMS-02]", p.deny) with input as fixture.plan([key_policy([via_s3])])
}

test_violation_two_account_delegations if {
	fixture.has("[PLAN-KMS-02]", p.deny) with input as fixture.plan([key_policy([root, root, via_s3])])
}

test_violation_other_account_role if {
	s := json.patch(via_s3, [{"op": "replace", "path": "/Principal/AWS", "value": "arn:aws:iam::210987654321:role/a"}])
	fixture.has("[PLAN-KMS-03]", p.deny) with input as fixture.plan([key_policy([root, s])])
}

test_violation_role_without_via_service if {
	s := json.patch(via_s3, [{"op": "remove", "path": "/Condition"}])
	fixture.has("[PLAN-KMS-04]", p.deny) with input as fixture.plan([key_policy([root, s])])
}

test_violation_service_without_source if {
	s := json.patch(logs, [{"op": "remove", "path": "/Condition"}])
	fixture.has("[PLAN-KMS-05]", p.deny) with input as fixture.plan([key_policy([root, s])])
}

test_pass_service_limited_by_source_account if {
	s := json.patch(logs, [{"op": "replace", "path": "/Condition", "value": {"StringEquals": {"aws:SourceAccount": "123456789012"}}}])
	count(p.deny) == 0 with input as fixture.plan([key_policy([root, s])])
}

test_violation_for_all_values_encryption_context if {
	s := json.patch(logs, [{"op": "replace", "path": "/Condition", "value": {"ForAllValues:StringEquals": {"kms:EncryptionContext:aws:logs:arn": ["x"]}}}])
	fixture.has("[PLAN-KMS-06]", p.deny) with input as fixture.plan([key_policy([root, s])])
}
