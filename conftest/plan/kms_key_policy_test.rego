package plan.kms_key_policy_test

import data.plan.fixture
import data.plan.kms_key_policy as p

# modules/kms/tests/policy.tftest.hcl の期待値と同じ文。module の実際の出力が通ることは、state-backend の reference の plan への適用が見る。
root := {
	"Sid": "AccountKeyAdministration",
	"Effect": "Allow",
	"Principal": {"AWS": "arn:aws:iam::123456789012:root"},
	"Action": [
		"kms:CancelKeyDeletion", "kms:CreateAlias", "kms:DeleteAlias", "kms:DescribeKey", "kms:DisableKey",
		"kms:DisableKeyRotation", "kms:EnableKey", "kms:EnableKeyRotation", "kms:GetKeyPolicy",
		"kms:GetKeyRotationStatus", "kms:ListGrants", "kms:ListKeyPolicies", "kms:ListResourceTags",
		"kms:PutKeyPolicy", "kms:RevokeGrant", "kms:RotateKeyOnDemand", "kms:ScheduleKeyDeletion",
		"kms:TagResource", "kms:UntagResource", "kms:UpdateAlias", "kms:UpdateKeyDescription",
	],
	"Resource": "*",
}

via_s3 := {
	"Sid": "RolesViaS3",
	"Effect": "Allow",
	"Principal": {"AWS": [
		"arn:aws:iam::123456789012:role/tabp-admin-ci-apply",
		"arn:aws:iam::123456789012:role/tabp-admin-ci-plan",
	]},
	"Action": ["kms:Decrypt", "kms:GenerateDataKey"],
	"Resource": "*",
	"Condition": {"StringEquals": {"kms:ViaService": "s3.ap-northeast-1.amazonaws.com"}},
}

logs := {
	"Sid": "LogsService",
	"Effect": "Allow",
	"Principal": {"Service": "logs.ap-northeast-1.amazonaws.com"},
	"Action": ["kms:Decrypt", "kms:Describe*", "kms:Encrypt", "kms:GenerateDataKey*", "kms:ReEncrypt*"],
	"Resource": "*",
	"Condition": {"ArnEquals": {"kms:EncryptionContext:aws:logs:arn": [
		"arn:aws:logs:ap-northeast-1:123456789012:log-group:/tabp/admin/s3-access/plan",
		"arn:aws:logs:ap-northeast-1:123456789012:log-group:/tabp/admin/s3-access/state",
	]}},
}

key_policy(statements) := fixture.doc("aws_kms_key_policy", "module.kms.aws_kms_key_policy.this", "policy", {"Version": "2012-10-17", "Statement": statements})

ids(statements) := found if {
	found := {substring(m, 0, 13) | some m in p.deny} with input as fixture.plan([key_policy(statements)])
}

with_condition(s, c) := json.patch(s, [{"op": "replace", "path": "/Condition", "value": c}])

without(s, path) := json.patch(s, [{"op": "remove", "path": path}])

test_pass_module_shape if {
	count(p.deny) == 0 with input as fixture.plan([key_policy([root, via_s3, logs])])
}

test_pass_no_key_policy if {
	count(p.deny) == 0 with input as fixture.plan([fixture.role_policy([fixture.allow("s3:GetObject")])])
}

test_pass_two_key_policies if {
	other := fixture.doc("aws_kms_key_policy", "module.other.aws_kms_key_policy.this", "policy", {"Statement": [root, logs]})
	count(p.deny) == 0 with input as fixture.plan([key_policy([root, via_s3]), other])
}

test_pass_condition_case_insensitive if {
	s := with_condition(via_s3, {"stringequals": {"KMS:VIASERVICE": "s3.ap-northeast-1.amazonaws.com"}})
	count(ids([root, s])) == 0
}

test_pass_single_role_string if {
	s := json.patch(via_s3, [{"op": "replace", "path": "/Principal/AWS", "value": "arn:aws:iam::123456789012:role/a"}])
	count(ids([root, s])) == 0
}

test_pass_other_partition if {
	r := json.patch(root, [{"op": "replace", "path": "/Principal/AWS", "value": "arn:aws-cn:iam::123456789012:root"}])
	s := json.patch(via_s3, [{"op": "replace", "path": "/Principal/AWS", "value": "arn:aws-cn:iam::123456789012:role/a"}])
	count(ids([r, s])) == 0
}

test_pass_deny_star_principal if {
	s := {"Effect": "Deny", "Principal": "*", "Action": "kms:*", "Resource": "*", "Condition": {"Bool": {"aws:SecureTransport": "false"}}}
	count(ids([root, s])) == 0
}

test_violation_star_principal if {
	ids([root, {"Effect": "Allow", "Principal": "*", "Action": "kms:Decrypt", "Resource": "*"}]) == {"[PLAN-KMS-01]"}
}

test_violation_star_in_aws_principals if {
	every v in ["*", ["arn:aws:iam::123456789012:role/a", "*"]] {
		ids([root, json.patch(via_s3, [{"op": "replace", "path": "/Principal/AWS", "value": v}])]) == {"[PLAN-KMS-01]"}
	}
}

test_violation_no_account_delegation if {
	every statements in [[via_s3], []] {
		"[PLAN-KMS-02]" in ids(statements)
	}
}

# 委任のアカウントが決まらない文書では、値を委任のアカウントと比べる規則は落とす側（05）か黙る側（03）に倒れる。どちらでも 02 が文書を落とす。
test_violation_account_undefined if {
	other := json.patch(via_s3, [{"op": "replace", "path": "/Principal/AWS", "value": "arn:aws:iam::210987654321:role/a"}])
	ids([logs]) == {"[PLAN-KMS-02]", "[PLAN-KMS-05]"}
	ids([other]) == {"[PLAN-KMS-02]"}
}

test_violation_two_account_delegations if {
	"[PLAN-KMS-02]" in ids([root, root, via_s3])
}

test_violation_root_arn_malformed if {
	every a in ["arn:aws:iam::123456789012:rootx", "xarn:aws:iam::123456789012:root", "arn:aws:iam::12345678901:root"] {
		"[PLAN-KMS-02]" in ids([json.patch(root, [{"op": "replace", "path": "/Principal/AWS", "value": a}]), via_s3])
	}
}

test_pass_own_assumed_role if {
	s := json.patch(via_s3, [{"op": "replace", "path": "/Principal/AWS", "value": "arn:aws:sts::123456789012:assumed-role/a/b"}])
	count(ids([root, s])) == 0
}

# AWS は12桁のアカウント ID をそのアカウントへの委任として受ける。ARN でない principal は同じアカウントと読まない。
test_violation_principal_not_in_account if {
	every a in [
		"arn:aws:iam::210987654321:role/a",
		"210987654321",
		"arn:aws:sts::210987654321:assumed-role/a/b",
		"AROAEXAMPLEUNIQUEID",
		"xarn:aws:iam::123456789012:role/a",
	] {
		"[PLAN-KMS-03]" in ids([root, json.patch(via_s3, [{"op": "replace", "path": "/Principal/AWS", "value": a}])])
	}
}

test_violation_role_not_limited_by_via_service if {
	every s in [
		without(via_s3, "/Condition"),
		with_condition(via_s3, {"StringEquals": {"aws:SourceAccount": "123456789012"}}),
		with_condition(via_s3, {"StringNotEquals": {"kms:ViaService": "s3.ap-northeast-1.amazonaws.com"}}),
		with_condition(via_s3, {"StringEqualsIfExists": {"kms:ViaService": "s3.ap-northeast-1.amazonaws.com"}}),
		with_condition(via_s3, {"StringLike": {"kms:ViaService": "*"}}),
		with_condition(via_s3, {"StringLike": {"kms:ViaService": ["s3.ap-northeast-1.amazonaws.com", "*"]}}),
		with_condition(via_s3, {"StringEquals": {"kms:ViaService": "s3.amazonaws.com"}}),
		with_condition(via_s3, {"StringEquals": {"kms:ViaService": 1}}),
	] {
		"[PLAN-KMS-04]" in ids([root, s])
	}
}

test_pass_role_limited_by_via_service if {
	every c in [
		{"StringLike": {"kms:ViaService": "s3.ap-northeast-1.amazonaws.com"}},
		{"StringEquals": {"kms:ViaService": ["s3.ap-northeast-1.amazonaws.com", "sqs.ap-northeast-1.amazonaws.com"]}},
	] {
		count(ids([root, with_condition(via_s3, c)])) == 0
	}
}

test_violation_service_not_limited_by_source if {
	every s in [
		without(logs, "/Condition"),
		with_condition(logs, {"Bool": {"aws:SecureTransport": "true"}}),
		with_condition(logs, {"StringEquals": {"kms:ViaService": "logs.ap-northeast-1.amazonaws.com"}}),
		with_condition(logs, {"StringEquals": {"kms:EncryptionContextKeys": "aws:logs:arn"}}),
		with_condition(logs, {"StringEquals": {"aws:SourceAccount": "210987654321"}}),
		with_condition(logs, {"ArnLike": {"aws:SourceArn": "*"}}),
		with_condition(logs, {"ArnLike": {"aws:SourceArn": "arn:aws:cloudtrail:ap-northeast-1:123456789012:trail/*"}}),
		with_condition(logs, {"ArnLike": {"aws:SourceArn": "arn:aws:cloudtrail:ap-northeast-1:123456789012:trail/?"}}),
		with_condition(logs, {"ArnEquals": {"aws:SourceArn": "foo:bar:baz:qux:123456789012:x"}}),
		with_condition(logs, {"ArnEquals": {"aws:SourceArn": "arn:aws-cn:cloudtrail:cn-north-1:123456789012:trail/x"}}),
		with_condition(logs, {"ArnEquals": {"aws:SourceArn": "arn:aws:s3:::bucket"}}),
		with_condition(logs, {"StringLike": {"kms:EncryptionContext:aws:secretsmanager:secret": "*"}}),
		with_condition(logs, {"StringEquals": {"kms:EncryptionContext:aws:dynamodb:tableName": "x"}}),
		with_condition(logs, {"ArnEquals": {"aws:SourceArn": "arn:aws:cloudtrail:ap-northeast-1:210987654321:trail/x"}}),
		with_condition(logs, {"ArnNotEquals": {"kms:EncryptionContext:aws:logs:arn": "arn:aws:logs:ap-northeast-1:123456789012:log-group:/x"}}),
		with_condition(logs, {"Null": {"kms:EncryptionContext:aws:logs:arn": "true"}}),
		with_condition(logs, {"ArnLike": {"kms:EncryptionContext:aws:logs:arn": "arn:aws:logs:ap-northeast-1:123456789012:log-group:*"}}),
	] {
		"[PLAN-KMS-05]" in ids([root, s])
	}
}

# 限る条件の値が1つでも外を指せば落とす。ほかの値やキーが委任のアカウントに限っていても通さない。
test_violation_service_source_partly_foreign if {
	every c in [
		{"StringEquals": {"aws:SourceAccount": ["123456789012", "210987654321"]}},
		{"ArnEquals": {"aws:SourceArn": ["arn:aws:cloudtrail:ap-northeast-1:123456789012:trail/x", "arn:aws:cloudtrail:ap-northeast-1:210987654321:trail/x"]}},
		{"ArnEquals": {"kms:EncryptionContext:aws:logs:arn": ["arn:aws:logs:ap-northeast-1:123456789012:log-group:/x", "arn:aws:logs:ap-northeast-1:210987654321:log-group:/x"]}},
		{"StringEquals": {"aws:SourceAccount": "123456789012"}, "ArnEquals": {"aws:SourceArn": "arn:aws:sns:ap-northeast-1:210987654321:t"}},
		{"StringEquals": {"aws:SourceAccount": "210987654321", "kms:EncryptionContext:foo": "bar"}},
	] {
		"[PLAN-KMS-05]" in ids([root, with_condition(logs, c)])
	}
}

test_violation_service_array_without_source if {
	s := json.patch(without(logs, "/Condition"), [{"op": "replace", "path": "/Principal/Service", "value": ["logs.ap-northeast-1.amazonaws.com", "cloudtrail.amazonaws.com"]}])
	"[PLAN-KMS-05]" in ids([root, s])
}

test_pass_service_limited_by_source if {
	every c in [
		{"StringEquals": {"aws:SourceAccount": "123456789012"}},
		{"ArnEquals": {"aws:SourceArn": "arn:aws:cloudtrail:ap-northeast-1:123456789012:trail/x"}},
		{"ArnLike": {"aws:SourceArn": "arn:aws:cloudtrail:ap-northeast-1:123456789012:trail/x"}},
		{"StringEquals": {"kms:EncryptionContext:aws:sns:topicArn": "arn:aws:sns:ap-northeast-1:123456789012:t"}},
		{"StringEquals": {"aws:SourceAccount": "123456789012"}, "ArnLike": {"aws:SourceArn": "arn:aws:s3:::bucket"}},
	] {
		count(ids([root, with_condition(logs, c)])) == 0
	}
}

test_violation_for_all_values_encryption_context if {
	every c in [
		{"ForAllValues:StringEquals": {"kms:EncryptionContext:aws:logs:arn": ["x"]}},
		{"ForAllValues:ArnEquals": {"kms:EncryptionContext:aws:logs:arn": ["x"]}},
		{"forallvalues:arnequals": {"KMS:EncryptionContext:aws:logs:arn": ["x"]}},
		{"ForAllValues:StringEquals": {"kms:EncryptionContextKeys": ["aws:logs:arn"]}},
	] {
		"[PLAN-KMS-06]" in ids([root, with_condition(logs, c)])
	}
}

test_pass_for_all_values_on_deny_and_other_keys if {
	deny_no_context := {"Effect": "Deny", "Principal": "*", "Action": "kms:Decrypt", "Resource": "*", "Condition": {"ForAllValues:StringNotEquals": {"kms:EncryptionContextKeys": ["aws:logs:arn"]}}}
	other_key := json.patch(logs, [{"op": "add", "path": "/Condition/ForAllValues:StringEquals", "value": {"aws:TagKeys": ["x"]}}])
	count(ids([root, logs, deny_no_context])) == 0
	count(ids([root, other_key])) == 0
}

test_violation_unreadable_principal if {
	every s in [
		{"Effect": "Allow", "NotPrincipal": {"AWS": "arn:aws:iam::123456789012:role/a"}, "Action": "kms:Decrypt", "Resource": "*"},
		{"Effect": "Allow", "Principal": {"AWS": "arn:aws:iam::123456789012:role/a"}, "NotPrincipal": {"AWS": "arn:aws:iam::123456789012:role/b"}, "Action": "kms:Decrypt", "Resource": "*"},
		{"Effect": "Allow", "Action": "kms:Decrypt", "Resource": "*"},
		{"Effect": "Allow", "Principal": "arn:aws:iam::123456789012:role/a", "Action": "kms:Decrypt", "Resource": "*"},
		{"Effect": "Allow", "Principal": {"Federated": "arn:aws:iam::210987654321:saml-provider/x"}, "Action": "kms:Decrypt", "Resource": "*"},
		{"Effect": "Allow", "Principal": {"CanonicalUser": "x"}, "Action": "kms:Decrypt", "Resource": "*"},
		{"Effect": "Allow", "Principal": {}, "Action": "kms:Decrypt", "Resource": "*"},
		{"Effect": "Allow", "Principal": {"AWS": []}, "Action": "kms:Decrypt", "Resource": "*"},
		{"Effect": "Allow", "Principal": {"Service": []}, "Action": "kms:Decrypt", "Resource": "*"},
		{"Effect": "Allow", "Principal": {"AWS": {"x": 1}}, "Action": "kms:Decrypt", "Resource": "*"},
	] {
		"[PLAN-KMS-07]" in ids([root, s])
	}
}

test_pass_readable_principal_shapes if {
	both := json.patch(via_s3, [{"op": "add", "path": "/Principal/Service", "value": ["logs.ap-northeast-1.amazonaws.com"]}, {"op": "add", "path": "/Condition/ArnEquals", "value": logs.Condition.ArnEquals}])
	not "[PLAN-KMS-07]" in ids([root, both])
	not "[PLAN-KMS-07]" in ids([root, {"Effect": "Allow", "Principal": "*", "Action": "kms:Decrypt", "Resource": "*"}])
}

test_violation_delegation_with_crypto_or_grant if {
	every a in [
		"kms:CreateGrant", "kms:Decapsulate", "kms:Decrypt", "kms:DeriveSharedSecret", "kms:Encapsulate", "kms:Encrypt",
		"kms:GenerateDataKey", "kms:GenerateDataKeyPair", "kms:GenerateDataKeyPairWithoutPlaintext",
		"kms:GenerateDataKeyWithoutPlaintext", "kms:GenerateMac", "kms:GetPublicKey", "kms:ReEncryptFrom", "kms:ReEncryptTo",
		"kms:Sign", "kms:Verify", "kms:VerifyMac",
		"KMS:CreateGrant", "kms:GenerateDataKey*", "kms:Create*", "kms:*", "kms:Decrypt?",
	] {
		"[PLAN-KMS-08]" in ids([json.patch(root, [{"op": "replace", "path": "/Action", "value": array.concat(root.Action, [a])}]), via_s3])
	}
}

test_pass_delegation_single_action if {
	count(ids([json.patch(root, [{"op": "replace", "path": "/Action", "value": "kms:DescribeKey"}]), via_s3])) == 0
}

test_violation_delegation_with_not_action if {
	r := json.patch(without(root, "/Action"), [{"op": "add", "path": "/NotAction", "value": "kms:Decrypt"}])
	"[PLAN-KMS-08]" in ids([r, via_s3])
}
