package plan.kms_key_policy_test

import data.plan.fixture
import data.plan.kms_key_policy as p

# modules/kms/tests/policy.tftest.hcl の期待値と同じ文。module の実際の出力が通ることは、state-backend と audit-evidence の reference の plan への適用が見る。
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
	"Sid": "RolesViaS3ReadWrite",
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

# modules/kms/tests/policy.tftest.hcl の sns_role の期待値と同じ文。
via_sns := {
	"Sid": "RolesViaSns0",
	"Effect": "Allow",
	"Principal": {"AWS": ["arn:aws:iam::123456789012:role/tabp-dev-heartbeat"]},
	"Action": ["kms:Decrypt", "kms:GenerateDataKey*"],
	"Resource": "*",
	"Condition": {"StringEquals": {
		"kms:ViaService": "sns.ap-northeast-1.amazonaws.com",
		"kms:EncryptionContext:aws:sns:topicArn": ["arn:aws:sns:ap-northeast-1:123456789012:tabp-dev-ops-notification"],
	}},
}

# modules/kms/tests/policy.tftest.hcl の RolesViaS3Read の期待値と同じ形の文。
via_s3_read := {
	"Sid": "RolesViaS3Read0",
	"Effect": "Allow",
	"Principal": {"AWS": ["arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"]},
	"Action": "kms:Decrypt",
	"Resource": "*",
	"Condition": {"StringEquals": {
		"kms:ViaService": "s3.ap-northeast-1.amazonaws.com",
		"kms:EncryptionContext:aws:s3:arn": ["arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/identity/terraform.tfstate.tflock"],
	}},
}

own_object := "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/identity/terraform.tfstate.tflock"

s3_read_with(op, values) := with_condition(via_s3_read, object.union(
	{"StringEquals": {"kms:ViaService": "s3.ap-northeast-1.amazonaws.com"}},
	{op: {"kms:EncryptionContext:aws:s3:arn": values}},
))

sns_publisher(sid, svc) := {
	"Sid": sid,
	"Effect": "Allow",
	"Principal": {"Service": svc},
	"Action": ["kms:Decrypt", "kms:GenerateDataKey*"],
	"Resource": "*",
	"Condition": {"StringEquals": {"aws:SourceAccount": "123456789012"}},
}

sns_events := {
	"Sid": "SnsPublisherEvents",
	"Effect": "Allow",
	"Principal": {"Service": "events.amazonaws.com"},
	"Action": ["kms:Decrypt", "kms:GenerateDataKey*"],
	"Resource": "*",
	"Condition": {"StringEquals": {"kms:EncryptionContext:aws:sns:topicArn": ["arn:aws:sns:ap-northeast-1:123456789012:tabp-dev-ops-notification"]}},
}

declared := "module.state_backend.module.kms.aws_kms_key_policy.this"

key_policy(statements) := fixture.doc("aws_kms_key_policy", declared, "policy", {"Version": "2012-10-17", "Statement": statements})

ids(statements) := found if {
	found := {substring(m, 0, 13) | some m in p.deny} with input as fixture.plan([key_policy(statements)])
}

with_condition(s, c) := json.patch(s, [{"op": "replace", "path": "/Condition", "value": c}])

without(s, path) := json.patch(s, [{"op": "remove", "path": path}])

test_pass_module_shape if {
	count(p.deny) == 0 with input as fixture.plan([key_policy([root, via_s3, via_s3_read, logs])])
}

test_violation_declared_key_policy_absent if {
	msgs := p.deny with input as fixture.plan([fixture.role_policy([fixture.allow("s3:GetObject")])])
	msgs == {sprintf("[PLAN-KMS-00] 宣言した state の鍵の Key Policy %s が入力にありません", [declared])}
}

test_violation_only_other_key_policy if {
	other := fixture.doc("aws_kms_key_policy", "module.other.aws_kms_key_policy.this", "policy", {"Statement": [root, logs]})
	fixture.has("[PLAN-KMS-00]", p.deny) with input as fixture.plan([other])
}

test_violation_declaration_not_string if {
	every v in [null, 1, ["x"]] {
		fixture.has("[PLAN-KMS-00] 宣言 state_backend.key_policy", p.deny) with input as fixture.plan([key_policy([root, via_s3])])
			with data.policy_plan.state_backend.key_policy as v
	}
}

test_violation_declaration_absent if {
	decl := object.union(object.remove(data.policy_plan, ["state_backend"]), {"state_backend": {}})
	fixture.has("[PLAN-KMS-00] 宣言 state_backend.key_policy", p.deny) with input as fixture.plan([key_policy([root, via_s3])])
		with data.policy_plan as decl
}

test_violation_key_policy_unknown if {
	unknown := {"address": declared, "type": "aws_kms_key_policy", "change": {"actions": ["update"], "after": {}, "after_unknown": {"policy": true}}}
	ids := {substring(m, 0, 13) | some m in p.deny} with input as fixture.plan([unknown])
	ids == {"[PLAN-KMS-09]"}
}

test_violation_key_policy_not_json if {
	every v in ["{", 1] {
		broken := {"address": declared, "type": "aws_kms_key_policy", "change": {"actions": ["update"], "after": {"policy": v}, "after_unknown": {}}}
		ids := {substring(m, 0, 13) | some m in p.deny} with input as fixture.plan([broken])
		ids == {"[PLAN-KMS-09]"}
	}
}

test_violation_other_key_policy_unknown if {
	unknown := {"address": "module.other.aws_kms_key_policy.this", "type": "aws_kms_key_policy", "change": {"actions": ["create"], "after": {}, "after_unknown": {"policy": true}}}
	ids := {substring(m, 0, 13) | some m in p.deny} with input as fixture.plan([key_policy([root, via_s3]), unknown])
	ids == {"[PLAN-KMS-09]"}
}

test_pass_declared_key_policy_unchanged if {
	noop := json.patch(key_policy([root, via_s3, logs]), [{"op": "replace", "path": "/change/actions", "value": ["no-op"]}])
	count(p.deny) == 0 with input as fixture.plan([noop])
}

test_pass_deleted_other_key_policy if {
	gone := {"address": "module.other.aws_kms_key_policy.this", "type": "aws_kms_key_policy", "change": {"actions": ["delete"], "after": null, "after_unknown": {}}}
	count(p.deny) == 0 with input as fixture.plan([key_policy([root, via_s3]), gone])
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

test_violation_declared_key_policy_deleted if {
	gone := {"address": declared, "type": "aws_kms_key_policy", "change": {"actions": ["delete"], "after": null, "after_unknown": {}}}
	msgs := p.deny with input as fixture.plan([gone])
	msgs == {sprintf("[PLAN-KMS-00] 宣言した state の鍵の Key Policy %s が入力にありません", [declared])}
}

test_pass_declared_key_policy_replaced if {
	replaced := json.patch(key_policy([root, via_s3, logs]), [{"op": "replace", "path": "/change/actions", "value": ["delete", "create"]}])
	count(p.deny) == 0 with input as fixture.plan([replaced])
}

test_violation_declared_address_other_type if {
	other := fixture.doc("aws_iam_role_policy", declared, "policy", {"Statement": [fixture.allow("s3:GetObject")]})
	fixture.has("[PLAN-KMS-00] 宣言した state の鍵の Key Policy", p.deny) with input as fixture.plan([other])
}

# JSON として読めれば 09 は出ない。文書の形でないものは委任が0件として 02 が落とす。
test_violation_key_policy_json_not_document if {
	every v in ["{}", "[]", "\"x\""] {
		odd := {"address": declared, "type": "aws_kms_key_policy", "change": {"actions": ["update"], "after": {"policy": v}, "after_unknown": {}}}
		ids := {substring(m, 0, 13) | some m in p.deny} with input as fixture.plan([odd])
		"[PLAN-KMS-02]" in ids
		not "[PLAN-KMS-09]" in ids
	}
}

# 管理から外す変更は変更後の文書を持たない。その理由は plan.stateful_protection が持つ。
test_pass_forgotten_other_key_policy if {
	forgotten := {"address": "module.other.aws_kms_key_policy.this", "type": "aws_kms_key_policy", "change": {"actions": ["forget"], "after": null, "after_unknown": {}}}
	count(p.deny) == 0 with input as fixture.plan([key_policy([root, via_s3]), forgotten])
}

role_change(name, actions) := {"address": sprintf("module.x.aws_iam_role.this[%q]", [name]), "type": "aws_iam_role", "change": {"actions": actions, "after": {"name": name}, "after_unknown": {}}}

kms10(name) := sprintf("[PLAN-KMS-10] p.json: role module.x.aws_iam_role.this[%q] を作るのに、それを名指す %s を書き直しません。KMS は作る前の role を名指し続けます", [name, declared])

unchanged(doc) := json.patch(doc, [
	{"op": "replace", "path": "/change/actions", "value": ["no-op"]},
	{"op": "add", "path": "/change/before", "value": doc.change.after},
])

noop_key_policy := unchanged(key_policy([root, via_s3, logs]))

test_violation_named_role_replaced_key_policy_unchanged if {
	every actions in [["delete", "create"], ["create", "delete"]] {
		msgs := p.deny with input as fixture.plan([noop_key_policy, role_change("tabp-admin-ci-plan", actions)])
		msgs == {kms10("tabp-admin-ci-plan")}
	}
}

test_pass_named_role_replaced_key_policy_rewritten if {
	updated := json.patch(key_policy([root, via_s3, logs]), [{"op": "replace", "path": "/change/actions", "value": ["update"]}])
	count(p.deny) == 0 with input as fixture.plan([updated, role_change("tabp-admin-ci-plan", ["delete", "create"])])
}

test_pass_unnamed_role_replaced if {
	count(p.deny) == 0 with input as fixture.plan([noop_key_policy, role_change("tabp-admin-ci-other", ["delete", "create"])])
}

test_pass_named_role_updated_in_place if {
	count(p.deny) == 0 with input as fixture.plan([noop_key_policy, role_change("tabp-admin-ci-plan", ["update"])])
}

test_pass_role_name_suffix_only if {
	count(p.deny) == 0 with input as fixture.plan([noop_key_policy, role_change("ci-plan", ["delete", "create"])])
}

test_violation_declared_key_policy_forgotten if {
	forgotten := {"address": declared, "type": "aws_kms_key_policy", "change": {"actions": ["forget"], "after": null, "after_unknown": {}}}
	msgs := p.deny with input as fixture.plan([forgotten])
	msgs == {sprintf("[PLAN-KMS-00] 宣言した state の鍵の Key Policy %s が入力にありません", [declared])}
}

test_violation_named_role_moved_without_moved_block if {
	gone := {"address": "module.x.aws_iam_role.old", "type": "aws_iam_role", "change": {"actions": ["delete"], "after": null, "after_unknown": {}}}
	msgs := p.deny with input as fixture.plan([noop_key_policy, gone, role_change("tabp-admin-ci-plan", ["create"])])
	msgs == {kms10("tabp-admin-ci-plan")}
}

test_violation_key_policy_updated_without_policy_change if {
	same := json.patch(noop_key_policy, [{"op": "replace", "path": "/change/actions", "value": ["update"]}])
	kms10("tabp-admin-ci-plan") in p.deny with input as fixture.plan([same, role_change("tabp-admin-ci-plan", ["delete", "create"])])
}

test_pass_key_policy_replaced if {
	replaced := json.patch(key_policy([root, via_s3, logs]), [{"op": "replace", "path": "/change/actions", "value": ["delete", "create"]}])
	count(p.deny) == 0 with input as fixture.plan([replaced, role_change("tabp-admin-ci-plan", ["delete", "create"])])
}

test_pass_other_key_policy_unchanged if {
	other := unchanged(fixture.doc("aws_kms_key_policy", "module.other.aws_kms_key_policy.this", "policy", {"Statement": [root, via_s3]}))
	updated := json.patch(key_policy([root, via_s3, logs]), [{"op": "replace", "path": "/change/actions", "value": ["update"]}])
	count(p.deny) == 0 with input as fixture.plan([updated, other, role_change("tabp-admin-ci-plan", ["delete", "create"])])
}

test_violation_created_role_name_unknown if {
	unknown := {"address": "module.x.aws_iam_role.this[\"x\"]", "type": "aws_iam_role", "change": {"actions": ["create"], "after": {}, "after_unknown": {"name": true}}}
	fixture.has("[PLAN-KMS-10] p.json: 作る role", p.deny) with input as fixture.plan([noop_key_policy, unknown])
}

test_role_path_is_part_of_the_arn if {
	pathed := json.patch(role_change("tabp-admin-ci-plan", ["delete", "create"]), [{"op": "add", "path": "/change/after/path", "value": "/ci/"}])
	count(p.deny) == 0 with input as fixture.plan([noop_key_policy, pathed])
	s := json.patch(via_s3, [{"op": "replace", "path": "/Principal/AWS", "value": "arn:aws:iam::123456789012:role/ci/tabp-admin-ci-plan"}])
	kms10("tabp-admin-ci-plan") in p.deny with input as fixture.plan([unchanged(key_policy([root, s])), pathed])
}

test_pass_sns_shape if {
	count(ids([
		root, via_sns,
		sns_publisher("SnsPublisherBudgets", "budgets.amazonaws.com"),
		sns_publisher("SnsPublisherCloudWatch", "cloudwatch.amazonaws.com"),
		sns_publisher("SnsPublisherCostAlerts", "costalerts.amazonaws.com"),
		sns_events,
	])) == 0
}

test_violation_sns_role_without_via_service if {
	"[PLAN-KMS-04]" in ids([root, with_condition(via_sns, {"StringEquals": {"kms:EncryptionContext:aws:sns:topicArn": ["arn:aws:sns:ap-northeast-1:123456789012:tabp-dev-ops-notification"]}})])
}

test_violation_sns_publisher_without_source if {
	every s in [
		without(sns_events, "/Condition"),
		without(sns_publisher("SnsPublisherCloudWatch", "cloudwatch.amazonaws.com"), "/Condition"),
		without(sns_publisher("SnsPublisherBudgets", "budgets.amazonaws.com"), "/Condition"),
		without(sns_publisher("SnsPublisherCostAlerts", "costalerts.amazonaws.com"), "/Condition"),
		with_condition(sns_publisher("SnsPublisherBudgets", "budgets.amazonaws.com"), {"StringEquals": {"aws:SourceAccount": "210987654321"}}),
		with_condition(sns_events, {"StringEquals": {"kms:EncryptionContext:aws:sns:topicArn": ["arn:aws:sns:ap-northeast-1:210987654321:tabp-dev-ops-notification"]}}),
		with_condition(sns_events, {"StringEquals": {"kms:EncryptionContext:aws:sns:topicArn": ["arn:aws:sns:*:123456789012:*"]}}),
		with_condition(sns_events, {"StringEquals": {"kms:EncryptionContext:aws:sns:topicArn": ["arn:aws-cn:sns:ap-northeast-1:123456789012:tabp-dev-ops-notification"]}}),
		with_condition(sns_events, {"StringEquals": {"kms:ViaService": "sns.ap-northeast-1.amazonaws.com"}}),
	] {
		ids([root, s]) == {"[PLAN-KMS-05]"}
	}
}

test_pass_sns_parts if {
	every statements in [[root, via_sns], [root, sns_events]] {
		count(ids(statements)) == 0
	}
}

test_violation_sns_role_without_topic if {
	every s in [
		with_condition(via_sns, {"StringEquals": {"kms:ViaService": "sns.ap-northeast-1.amazonaws.com"}}),
		with_condition(via_sns, {"StringEquals": {
			"kms:ViaService": "sns.ap-northeast-1.amazonaws.com",
			"kms:EncryptionContext:aws:sns:topicArn": ["arn:aws:sns:ap-northeast-1:210987654321:tabp-dev-ops-notification"],
		}}),
		with_condition(via_sns, {"StringEquals": {
			"kms:ViaService": "sns.ap-northeast-1.amazonaws.com",
			"kms:EncryptionContext:aws:sns:topicArn": ["arn:aws:sns:ap-northeast-1:123456789012:*"],
		}}),
		with_condition(via_sns, {"StringEquals": {
			"kms:ViaService": "sns.ap-northeast-1.amazonaws.com",
			"kms:EncryptionContext:aws:sns:topicArn": ["arn:aws:logs:ap-northeast-1:123456789012:log-group:x"],
		}}),
		with_condition(via_sns, {"StringEquals": {
			"kms:ViaService": "sns.ap-northeast-1.amazonaws.com",
			"kms:EncryptionContext:aws:sns:topicArn": ["arn:aws:sns:ap-northeast-1:123456789012:"],
		}}),
	] {
		ids([root, s]) == {"[PLAN-KMS-11]"}
	}
}

test_pass_s3_role_is_not_sns if {
	count(ids([root, via_s3])) == 0
}

test_pass_s3_read_shapes if {
	every s in [
		s3_read_with("StringEquals", [own_object, "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/workload/terraform.tfstate.tflock"]),
		s3_read_with("ArnEquals", [own_object]),
		s3_read_with("StringLike", [own_object]),
		s3_read_with("ArnLike", own_object),
		with_condition(via_s3_read, {"STRINGEQUALS": {
			"KMS:VIASERVICE": "s3.ap-northeast-1.amazonaws.com",
			"KMS:ENCRYPTIONCONTEXT:AWS:S3:ARN": own_object,
		}}),
		json.patch(via_s3_read, [{"op": "replace", "path": "/Action", "value": ["kms:Decrypt", "kms:DescribeKey"]}]),
		s3_read_with("StringEquals", ["arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/a/b:c"]),
		with_condition(via_s3_read, {"StringEquals": {
			"kms:ViaService": ["s3.ap-northeast-1.amazonaws.com", "s3.us-east-1.amazonaws.com"],
			"kms:EncryptionContext:aws:s3:arn": ["arn:aws:s3:::a.b-c-123456789012-us-east-1-an/k"],
		}}),
	] {
		count(ids([root, s])) == 0
	}
}

# ViaService と region は実在の形ではない。見るのは partition（aws-cn）の置換だけ。
test_pass_s3_read_other_partition if {
	cn_root := json.patch(root, [{"op": "replace", "path": "/Principal/AWS", "value": "arn:aws-cn:iam::123456789012:root"}])
	s := json.patch(with_condition(via_s3_read, {"StringEquals": {
		"kms:ViaService": "s3.cn-north-1.amazonaws.com",
		"kms:EncryptionContext:aws:s3:arn": ["arn:aws-cn:s3:::x-123456789012-cn-north-1-an/k"],
	}}), [{"op": "replace", "path": "/Principal/AWS", "value": ["arn:aws-cn:iam::123456789012:role/r"]}])
	count(ids([cn_root, s])) == 0
}

test_pass_s3_read_write if {
	every s in [
		json.patch(with_condition(via_s3, {"StringEquals": {"kms:ViaService": "s3.ap-northeast-1.amazonaws.com"}}), [{"op": "replace", "path": "/Action", "value": "kms:*"}]),
		with_condition(via_s3, {"StringEquals": {
			"kms:ViaService": "s3.ap-northeast-1.amazonaws.com",
			"kms:EncryptionContext:aws:s3:arn": [own_object],
		}}),
		json.patch(via_s3, [{"op": "replace", "path": "/Action", "value": ["kms:Decrypt", "kms:GenerateDataKey*"]}]),
		json.patch(via_s3, [{"op": "remove", "path": "/Action"}, {"op": "add", "path": "/NotAction", "value": ["kms:Encrypt"]}]),
	] {
		count({i | some i in ids([root, s]); i == "[PLAN-KMS-12]"}) == 0
	}
}

test_violation_s3_read_without_object if {
	every s in [
		with_condition(via_s3_read, {"StringEquals": {"kms:ViaService": "s3.ap-northeast-1.amazonaws.com"}}),
		s3_read_with("StringEqualsIfExists", [own_object]),
		s3_read_with("StringEqualsIgnoreCase", [own_object]),
		s3_read_with("ForAnyValue:StringEquals", [own_object]),
		s3_read_with("StringNotEquals", [own_object]),
		with_condition(via_s3_read, {"StringEquals": {
			"kms:ViaService": "s3.ap-northeast-1.amazonaws.com",
			"kms:EncryptionContext:aws:s3:bucket": "tabp-admin-tfstate-123456789012-ap-northeast-1-an",
		}}),
		json.patch(with_condition(via_s3_read, {"StringEquals": {"kms:ViaService": "s3.ap-northeast-1.amazonaws.com"}}), [{"op": "remove", "path": "/Action"}, {"op": "add", "path": "/NotAction", "value": ["kms:GenerateDataKey*", "kms:Encrypt"]}]),
		json.patch(with_condition(via_s3_read, {"StringEquals": {"kms:ViaService": "s3.ap-northeast-1.amazonaws.com"}}), [{"op": "replace", "path": "/Action", "value": [1]}]),
		json.patch(with_condition(via_s3_read, {"StringEquals": {"kms:ViaService": "s3.ap-northeast-1.amazonaws.com"}}), [{"op": "remove", "path": "/Action"}]),
		json.patch(with_condition(via_s3_read, {"StringEquals": {"kms:ViaService": "s3.ap-northeast-1.amazonaws.com"}}), [{"op": "replace", "path": "/Action", "value": []}]),
		json.patch(with_condition(via_s3_read, {"StringEquals": {"kms:ViaService": "s3.ap-northeast-1.amazonaws.com"}}), [{"op": "remove", "path": "/Action"}, {"op": "add", "path": "/NotAction", "value": [1]}]),
		json.patch(with_condition(via_s3_read, {"StringEquals": {"kms:ViaService": "s3.ap-northeast-1.amazonaws.com"}}), [{"op": "replace", "path": "/Action", "value": "kms:Dec*"}]),
		s3_read_with("StringEquals", []),
	] {
		ids([root, s]) == {"[PLAN-KMS-12]"}
	}
}

test_violation_s3_read_foreign_object if {
	every s in [
		s3_read_with("StringEquals", ["arn:aws:s3:::tabp-admin-tfstate-210987654321-ap-northeast-1-an/k"]),
		s3_read_with("StringEquals", ["arn:aws:s3:::bkt/k"]),
		s3_read_with("StringEquals", ["arn:aws:s3:::tabp-admin-tfstate-123456789012-us-east-1-an/k"]),
		s3_read_with("StringEquals", ["arn:aws-cn:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/k"]),
		s3_read_with("StringLike", ["arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/identity/*"]),
		s3_read_with("StringEquals", ["arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/${aws:PrincipalTag/k}"]),
		s3_read_with("StringEquals", ["arn:aws:s3:::tabp123456789012-ap-northeast-1-an/k"]),
		s3_read_with("StringEquals", ["*"]),
		s3_read_with("StringEquals", ["arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/k?"]),
		s3_read_with("StringEquals", ["arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/"]),
		s3_read_with("StringEquals", ["arn:aws:sns:ap-northeast-1:123456789012:t"]),
		s3_read_with("StringEquals", [1]),
		s3_read_with("StringEquals", [own_object, "arn:aws:s3:::bkt/k"]),
		with_condition(via_s3, {"StringEquals": {
			"kms:ViaService": "s3.ap-northeast-1.amazonaws.com",
			"kms:EncryptionContext:aws:s3:arn": ["arn:aws:s3:::bkt/k"],
		}}),
	] {
		ids([root, s]) == {"[PLAN-KMS-12]"}
	}
}

test_violation_s3_read_bucket_arn if {
	msgs := p.deny with input as fixture.plan([key_policy([root, s3_read_with("StringEquals", ["arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an"])])])
	fixture.has("[PLAN-KMS-12] p.json: module.state_backend.module.kms.aws_kms_key_policy.this が S3 経由の role の文の object に Bucket の ARN", msgs)
	count({m | some m in msgs; contains(m, "account regional namespace の Bucket の object の ARN でない")}) == 0
}

test_violation_s3_read_account_undefined if {
	r2 := json.patch(root, [{"op": "replace", "path": "/Sid", "value": "Root2"}])
	every statements in [[via_s3_read], [root, r2, via_s3_read]] {
		some m in p.deny with input as fixture.plan([key_policy(statements)])
		contains(m, "account regional namespace の Bucket の object の ARN でない")
	}
}

# 規則ごとの文言で、限定の欠落と object の誤りを取り違えないことを見る。
test_violation_s3_read_rule_messages if {
	missing := p.deny with input as fixture.plan([key_policy([root, with_condition(via_s3_read, {"StringEquals": {"kms:ViaService": "s3.ap-northeast-1.amazonaws.com"}})])])
	some m1 in missing
	contains(m1, "復号を object の ARN（kms:EncryptionContext:aws:s3:arn）で限らずに許可しています")
	foreign := p.deny with input as fixture.plan([key_policy([root, s3_read_with("StringEquals", ["arn:aws:s3:::bkt/k"])])])
	some m2 in foreign
	contains(m2, "account regional namespace の Bucket の object の ARN でない")
	count({m | some m in foreign; contains(m, "で限らずに許可しています")}) == 0
}

test_violation_s3_read_region_unreadable if {
	every v in ["s3.amazonaws.com", "s3.ap-northeast-1.amazonaws.com.evil"] {
		s := with_condition(via_s3_read, {"StringEquals": {"kms:ViaService": v, "kms:EncryptionContext:aws:s3:arn": own_object}})
		some m in p.deny with input as fixture.plan([key_policy([root, s])])
		contains(m, "account regional namespace の Bucket の object の ARN でない")
	}
}

test_pass_s3_read_out_of_scope if {
	every s in [
		json.patch(with_condition(via_s3_read, {"StringEquals": {"kms:ViaService": "s3.ap-northeast-1.amazonaws.com"}}), [{"op": "replace", "path": "/Principal/AWS", "value": "arn:aws:iam::123456789012:root"}]),
		json.patch(with_condition(via_s3_read, {"StringEquals": {"kms:ViaService": "s3.ap-northeast-1.amazonaws.com"}}), [{"op": "replace", "path": "/Action", "value": ["kms:DescribeKey"]}]),
		json.patch(with_condition(via_s3_read, {"StringEquals": {"kms:ViaService": "s3.ap-northeast-1.amazonaws.com", "kms:EncryptionContext:aws:s3:arn": ["arn:aws:s3:::bkt/k"]}}), [{"op": "replace", "path": "/Principal/AWS", "value": "arn:aws:iam::123456789012:root"}, {"op": "replace", "path": "/Action", "value": ["kms:DescribeKey"]}]),
		json.patch(via_sns, [{"op": "replace", "path": "/Action", "value": [1]}]),
		with_condition(via_sns, {"StringEquals": {
			"kms:ViaService": "sns.ap-northeast-1.amazonaws.com",
			"kms:EncryptionContext:aws:sns:topicArn": ["arn:aws:sns:ap-northeast-1:123456789012:t"],
			"kms:EncryptionContext:aws:s3:arn": ["arn:aws:s3:::bkt/k"],
		}}),
	] {
		count({i | some i in ids([root, s]); i == "[PLAN-KMS-12]"}) == 0
	}
}

test_violation_s3_read_mixed_via_service if {
	s := with_condition(via_s3_read, {"StringEquals": {"kms:ViaService": ["sns.ap-northeast-1.amazonaws.com", "s3.ap-northeast-1.amazonaws.com"]}})
	{"[PLAN-KMS-11]", "[PLAN-KMS-12]"} == {i | some i in ids([root, s]); i in {"[PLAN-KMS-11]", "[PLAN-KMS-12]"}}
}

trail_encrypt := {
	"Sid": "CloudTrailEncrypt",
	"Effect": "Allow",
	"Principal": {"Service": "cloudtrail.amazonaws.com"},
	"Action": "kms:GenerateDataKey*",
	"Resource": "*",
	"Condition": {"StringEquals": {
		"aws:SourceArn": ["arn:aws:cloudtrail:ap-northeast-1:111111111111:trail/tabp-org"],
		"kms:EncryptionContext:aws:cloudtrail:arn": ["arn:aws:cloudtrail:ap-northeast-1:111111111111:trail/tabp-org"],
	}},
}

config_service := {
	"Sid": "ConfigService",
	"Effect": "Allow",
	"Principal": {"Service": "config.amazonaws.com"},
	"Action": ["kms:Decrypt", "kms:GenerateDataKey"],
	"Resource": "*",
	"Condition": {"StringEquals": {"aws:SourceAccount": ["111111111111", "222222222222"]}},
}

cross := {"cross_account_sources": {declared: ["cloudtrail.amazonaws.com", "config.amazonaws.com"]}}

cross_ids(statements, decl) := found if {
	found := {substring(m, 0, 13) | some m in p.deny} with input as fixture.plan([key_policy(statements)])
		with data.policy_plan as object.union(object.remove(data.policy_plan, ["kms_key_policy"]), {"kms_key_policy": decl})
}

test_pass_declared_cross_account_sources if {
	count(cross_ids([root, trail_encrypt, config_service], cross)) == 0
}

test_violation_cross_account_sources_not_declared if {
	every decl in [
		{},
		{"cross_account_sources": {}},
		{"cross_account_sources": {"module.other.aws_kms_key_policy.this": ["cloudtrail.amazonaws.com", "config.amazonaws.com"]}},
		{"cross_account_sources": {declared: ["config.amazonaws.com"]}},
		{"cross_account_sources": {declared: "cloudtrail.amazonaws.com"}},
		{"cross_account_sources": []},
	] {
		"[PLAN-KMS-05]" in cross_ids([root, trail_encrypt], decl)
	}
}

test_violation_declared_cross_account_sources_not_typed if {
	every c in [
		{"StringEquals": {"aws:SourceArn": "arn:aws:cloudtrail:ap-northeast-1:*:trail/tabp-org"}},
		{"StringLike": {"aws:SourceArn": "arn:aws:cloudtrail:ap-northeast-1:111111111111:trail/*"}},
		{"StringEquals": {"aws:SourceArn": "arn:aws-cn:cloudtrail:cn-north-1:111111111111:trail/tabp-org"}},
		{"StringEquals": {"aws:SourceArn": "arn:aws:s3:::bucket"}},
		{"StringEquals": {"aws:SourceAccount": "*"}},
		{"StringEquals": {"aws:SourceAccount": "1111"}},
	] {
		"[PLAN-KMS-05]" in cross_ids([root, with_condition(trail_encrypt, c)], cross)
	}
}

# 宣言したサービスと宣言の外のサービスを1つの文に並べても、外のアカウントは受けない。
test_violation_declared_cross_account_mixed_principal if {
	s := json.patch(trail_encrypt, [{"op": "replace", "path": "/Principal/Service", "value": ["cloudtrail.amazonaws.com", "logs.ap-northeast-1.amazonaws.com"]}])
	"[PLAN-KMS-05]" in cross_ids([root, s], cross)
}

# 宣言した文でも、許す値と外の値が混ざれば外の値の規則だけが落とす。
test_violation_declared_cross_account_mixed_values if {
	every v in ["arn:aws:s3:::bucket", "arn:aws:cloudtrail:ap-northeast-1::trail/x", "1111"] {
		c := {"StringEquals": {
			"aws:SourceArn": ["arn:aws:cloudtrail:ap-northeast-1:111111111111:trail/tabp-org", v],
			"kms:EncryptionContext:aws:cloudtrail:arn": ["arn:aws:cloudtrail:ap-northeast-1:111111111111:trail/tabp-org"],
		}}
		found := p.deny with input as fixture.plan([key_policy([root, with_condition(trail_encrypt, c)])])
			with data.policy_plan as object.union(object.remove(data.policy_plan, ["kms_key_policy"]), {"kms_key_policy": cross})
		some m in found
		contains(m, "委任のアカウントの外の値")
	}
}

test_violation_cross_account_sources_empty_service_list if {
	"[PLAN-KMS-05]" in cross_ids([root, trail_encrypt], {"cross_account_sources": {declared: []}})
}

# エスケープと閉じない ${ も、policy 変数と同じく落とす側に倒す。
test_violation_source_policy_variable if {
	every c in [
		{"ArnEquals": {"kms:EncryptionContext:aws:logs:arn": ["arn:aws:logs:ap-northeast-1:123456789012:log-group:${aws:PrincipalTag/t}"]}},
		{"ArnEquals": {"kms:EncryptionContext:aws:logs:arn": [
			"arn:aws:logs:ap-northeast-1:123456789012:log-group:/tabp/admin/s3-access/plan",
			"arn:aws:logs:ap-northeast-1:123456789012:log-group:${aws:PrincipalTag/t}",
		]}},
		{"ArnEquals": {"aws:SourceArn": ["arn:aws:sns:ap-northeast-1:123456789012:${aws:PrincipalTag/t}"]}},
		{"ArnEquals": {"aws:SourceArn": ["arn:aws:sns:ap-northeast-1:123456789012:${aws:PrincipalTag/t, 'x'}"]}},
		{"ArnEquals": {"aws:SourceArn": ["arn:aws:sns:ap-northeast-1:123456789012:t${$}"]}},
		{"ArnEquals": {"aws:SourceArn": ["arn:aws:sns:ap-northeast-1:123456789012:t${"]}},
		{"StringEquals": {"aws:SourceArn": ["arn:aws:s3:::${aws:PrincipalTag/b}"], "aws:SourceAccount": "123456789012"}},
	] {
		ids([root, with_condition(logs, c)]) == {"[PLAN-KMS-05]"}
		some m in p.deny with input as fixture.plan([key_policy([root, with_condition(logs, c)])])
		contains(m, "wildcard か policy 変数を含む値")
	}
}

test_violation_source_not_string if {
	"[PLAN-KMS-05]" in ids([root, with_condition(logs, {"StringEquals": {"aws:SourceAccount": [123456789012]}})])
}

test_violation_cross_account_source_policy_variable if {
	c := {"StringEquals": {
		"aws:SourceArn": ["arn:aws:cloudtrail:ap-northeast-1:111111111111:trail/${aws:PrincipalTag/t}"],
		"kms:EncryptionContext:aws:cloudtrail:arn": ["arn:aws:cloudtrail:ap-northeast-1:111111111111:trail/tabp-org"],
	}}
	cross_ids([root, with_condition(trail_encrypt, c)], cross) == {"[PLAN-KMS-05]"}
}

# 名前に : を含む変数は ARN の区切りの数で落ち、: を含まない形は変数の判定で落ちる。
test_violation_topic_policy_variable if {
	every t in ["${aws:PrincipalTag/t}", "t${$}", "${x}"] {
		c := {"StringEquals": {"kms:ViaService": "sns.ap-northeast-1.amazonaws.com", "kms:EncryptionContext:aws:sns:topicArn": [concat("", ["arn:aws:sns:ap-northeast-1:123456789012:", t])]}}
		ids([root, with_condition(via_sns, c)]) == {"[PLAN-KMS-11]"}
	}
}
