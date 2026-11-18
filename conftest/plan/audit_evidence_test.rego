package plan.audit_evidence_test

import data.plan.audit_evidence as p
import data.plan.fixture

bucket_arn := "arn:aws:s3:::tabp-log-archive-audit-123456789012-ap-northeast-1-an"

trail := "arn:aws:cloudtrail:ap-northeast-1:111111111111:trail/tabp-org"

decl := {
	"bucket": "module.a.module.archive.aws_s3_bucket.this",
	"bucket_policy": "module.a.module.archive.aws_s3_bucket_policy.this",
	"object_lock_configuration": "module.a.module.archive.aws_s3_bucket_object_lock_configuration.this",
	"lifecycle_configuration": "module.a.module.archive.aws_s3_bucket_lifecycle_configuration.this",
	"key_policy": "module.a.module.archive_key.aws_kms_key_policy.this",
	"metric_alarm": "module.a.module.archive.aws_cloudwatch_metric_alarm.this",
}

blocking(sid, actions, condition) := object.union(
	{"Sid": sid, "Effect": "Deny", "Principal": "*", "Action": actions, "Resource": sprintf("%s/*", [bucket_arn])},
	{"Condition": condition},
)

denies := [
	{"Sid": "DenyInsecureTransport", "Effect": "Deny", "Principal": "*", "Action": "s3:*", "Resource": [bucket_arn, sprintf("%s/*", [bucket_arn])], "Condition": {"Bool": {"aws:SecureTransport": "false"}}},
	blocking("DenyObjectWriteByNonService", "s3:PutObject", {"Bool": {"aws:PrincipalIsAWSService": "false"}}),
	{"Sid": "DenyObjectDeletion", "Effect": "Deny", "Principal": "*", "Action": ["s3:DeleteObject", "s3:DeleteObjectVersion"], "Resource": sprintf("%s/*", [bucket_arn])},
	blocking("DenyRetentionBeyondDefault", ["s3:PutObject", "s3:PutObjectRetention"], {"NumericGreaterThan": {"s3:object-lock-remaining-retention-days": "731"}}),
	blocking("DenyOtherLockMode", ["s3:PutObject", "s3:PutObjectRetention"], {"Null": {"s3:object-lock-mode": "false"}, "StringNotEquals": {"s3:object-lock-mode": "COMPLIANCE"}}),
	{"Sid": "DenyLegalHold", "Effect": "Deny", "Principal": "*", "Action": "s3:PutObjectLegalHold", "Resource": sprintf("%s/*", [bucket_arn])},
	blocking("DenyLegalHoldOnWrite", "s3:PutObject", {"Null": {"s3:object-lock-legal-hold": "false"}}),
	blocking("DenyEventHold", ["s3:PutObject", "s3:PutObjectRetention"], {"Null": {"s3:object-lock-event-hold": "false"}}),
]

trail_write := {
	"Sid": "AWSCloudTrailOrganizationWrite",
	"Effect": "Allow",
	"Principal": {"Service": "cloudtrail.amazonaws.com"},
	"Action": "s3:PutObject",
	"Resource": sprintf("%s/AWSLogs/o-abcdefghij/*", [bucket_arn]),
	"Condition": {"StringEquals": {"s3:x-amz-acl": "bucket-owner-full-control", "aws:SourceArn": trail}},
}

config_write := {
	"Sid": "AWSConfigBucketDelivery222222222222",
	"Effect": "Allow",
	"Principal": {"Service": "config.amazonaws.com"},
	"Action": "s3:PutObject",
	"Resource": sprintf("%s/AWSLogs/222222222222/Config/*", [bucket_arn]),
	"Condition": {"StringEquals": {"s3:x-amz-acl": "bucket-owner-full-control", "aws:SourceAccount": "222222222222"}},
}

key_trail := {
	"Sid": "CloudTrailEncrypt",
	"Effect": "Allow",
	"Principal": {"Service": "cloudtrail.amazonaws.com"},
	"Action": "kms:GenerateDataKey*",
	"Resource": "*",
	"Condition": {"StringEquals": {"aws:SourceArn": [trail], "kms:EncryptionContext:aws:cloudtrail:arn": [trail]}},
}

key_config := {
	"Sid": "ConfigService",
	"Effect": "Allow",
	"Principal": {"Service": "config.amazonaws.com"},
	"Action": ["kms:Decrypt", "kms:GenerateDataKey"],
	"Resource": "*",
	"Condition": {"StringEquals": {"aws:SourceAccount": ["222222222222"]}},
}

change(address, after) := {"address": address, "type": "t", "change": {"actions": ["create"], "after": after, "after_unknown": {}}}

lock(mode, days) := change(decl.object_lock_configuration, {"rule": [{"default_retention": [{"mode": mode, "days": days, "years": null}]}]})

lifecycle(days) := change(decl.lifecycle_configuration, {"rule": [
	{"id": "expire-after-retention", "status": "Enabled", "filter": [{"and": [], "tag": []}], "expiration": [{"days": days, "expired_object_delete_marker": false}]},
	{"id": "expire-delete-markers", "status": "Enabled", "expiration": [{"days": 0, "expired_object_delete_marker": true}]},
]})

bucket(enabled) := change(decl.bucket, {"object_lock_enabled": enabled, "arn": bucket_arn})

bucket_policy(statements) := change(decl.bucket_policy, {"policy": json.marshal({"Version": "2012-10-17", "Statement": statements})})

key_policy(statements) := change(decl.key_policy, {"policy": json.marshal({"Version": "2012-10-17", "Statement": statements})})

valid := [
	bucket(true),
	lock("COMPLIANCE", 731),
	lifecycle(731),
	bucket_policy(array.concat(denies, [trail_write, config_write])),
	key_policy([key_trail, key_config]),
	change(decl.metric_alarm, {"alarm_name": "a"}),
]

replace(rcs, rc) := array.concat([x | some x in rcs; x.address != rc.address], [rc])

msgs(rcs) := found if {
	found := p.deny with input as fixture.plan(rcs) with data.policy_plan.audit_evidence as decl
}

ids(rcs) := {substring(m, 0, 16) | some m in msgs(rcs)}

test_pass_valid if {
	count(msgs(valid)) == 0
}

test_violation_declaration_missing if {
	found := p.deny with input as fixture.plan(valid) with data.policy_plan.audit_evidence as object.remove(decl, ["key_policy"])
	"[PLAN-AUDIT-00] " in {substring(m, 0, 16) | some m in found}
}

test_violation_declared_address_absent if {
	"[PLAN-AUDIT-00] " in ids([x | some x in valid; x.address != decl.lifecycle_configuration])
}

test_violation_lock_without_default_retention if {
	every rc in [
		change(decl.object_lock_configuration, {"rule": []}),
		change(decl.object_lock_configuration, {"rule": [{"default_retention": []}]}),
		lock("NONE", 731),
		lock("COMPLIANCE", 0),
	] {
		"[PLAN-AUDIT-01] " in ids(replace(valid, rc))
	}
}

test_violation_expiration_differs_from_retention if {
	every rc in [lifecycle(730), lifecycle(732)] {
		"[PLAN-AUDIT-01] " in ids(replace(valid, rc))
	}
}

test_violation_object_lock_disabled if {
	"[PLAN-AUDIT-04] " in ids(replace(valid, bucket(false)))
}

test_violation_allow_not_service_only if {
	every s in [
		json.patch(trail_write, [{"op": "replace", "path": "/Principal", "value": {"AWS": "arn:aws:iam::123456789012:role/r"}}]),
		json.patch(trail_write, [{"op": "replace", "path": "/Principal", "value": "*"}]),
		json.patch(trail_write, [{"op": "replace", "path": "/Principal", "value": {"Service": "cloudtrail.amazonaws.com", "AWS": "arn:aws:iam::123456789012:role/r"}}]),
	] {
		"[PLAN-AUDIT-02] " in ids(replace(valid, bucket_policy(array.concat(denies, [s]))))
	}
}

test_violation_allow_source_not_limited if {
	every c in [
		{"StringEquals": {"s3:x-amz-acl": "bucket-owner-full-control"}},
		{"StringLike": {"aws:SourceArn": "arn:aws:cloudtrail:ap-northeast-1:111111111111:trail/*"}},
		{"StringEquals": {"aws:SourceAccount": "*"}},
		{"StringNotEquals": {"aws:SourceArn": trail}},
	] {
		s := json.patch(trail_write, [{"op": "replace", "path": "/Condition", "value": c}])
		"[PLAN-AUDIT-02] " in ids(replace(valid, bucket_policy(array.concat(denies, [s]))))
	}
}

test_violation_required_deny_missing if {
	every i in numbers.range(0, count(denies) - 1) {
		rest := [d | some j, d in denies; j != i]
		"[PLAN-AUDIT-03] " in ids(replace(valid, bucket_policy(array.concat(rest, [trail_write]))))
	}
}

test_violation_deny_with_principal_exception if {
	s := json.patch(denies[2], [{"op": "add", "path": "/Condition", "value": {"ArnNotEquals": {"aws:PrincipalArn": "arn:aws:iam::123456789012:role/r"}}}])
	rest := [d | some j, d in denies; j != 2]
	"[PLAN-AUDIT-03] " in ids(replace(valid, bucket_policy(array.concat(rest, [s, trail_write]))))
}

test_violation_deny_not_unconditional_shape if {
	every patch in [
		[{"op": "replace", "path": "/Principal", "value": {"AWS": "arn:aws:iam::123456789012:root"}}],
		[{"op": "replace", "path": "/Effect", "value": "Allow"}],
	] {
		s := json.patch(denies[2], patch)
		rest := [d | some j, d in denies; j != 2]
		"[PLAN-AUDIT-03] " in ids(replace(valid, bucket_policy(array.concat(rest, [s, trail_write]))))
	}
}

test_violation_deny_values_differ_from_retention if {
	every rc in [lock("GOVERNANCE", 731), lock("COMPLIANCE", 365)] {
		"[PLAN-AUDIT-03] " in ids(replace(replace(valid, rc), lifecycle(rc.change.after.rule[0].default_retention[0].days)))
	}
}

test_violation_trail_mismatch if {
	other := json.patch(key_trail, [{"op": "replace", "path": "/Condition/StringEquals/aws:SourceArn", "value": ["arn:aws:cloudtrail:ap-northeast-1:111111111111:trail/other"]}])
	"[PLAN-AUDIT-05] " in ids(replace(valid, key_policy([other, key_config])))
}

test_pass_trail_match_across_statements if {
	describe := {
		"Sid": "CloudTrailDescribeKey",
		"Effect": "Allow",
		"Principal": {"Service": "cloudtrail.amazonaws.com"},
		"Action": "kms:DescribeKey",
		"Resource": "*",
		"Condition": {"StringEquals": {"aws:SourceArn": [trail]}},
	}
	count(msgs(replace(valid, key_policy([key_trail, describe, key_config])))) == 0
}

test_violation_expiration_not_whole_bucket if {
	rc := change(decl.lifecycle_configuration, {"rule": [{"id": "x", "status": "Enabled", "filter": [{"and": [], "prefix": "AWSLogs/", "tag": []}], "expiration": [{"days": 731}]}]})
	"[PLAN-AUDIT-01] " in ids(replace(valid, rc))
}

test_violation_metric_alarm_absent if {
	"[PLAN-AUDIT-00] " in ids([x | some x in valid; x.address != decl.metric_alarm])
}

# 条件の演算子やキーが欠けた Deny、Action・Resource・Condition をすり替えた Deny は、Sid が在っても落ちる。
test_violation_deny_shape_altered if {
	every pair in [
		[0, [{"op": "replace", "path": "/Condition/Bool/aws:SecureTransport", "value": "true"}]],
		[0, [{"op": "replace", "path": "/Condition", "value": {}}]],
		[0, [{"op": "replace", "path": "/Resource", "value": sprintf("%s/*", [bucket_arn])}]],
		[2, [{"op": "replace", "path": "/Action", "value": "s3:GetObject"}]],
		[3, [{"op": "replace", "path": "/Condition", "value": {"NumericLessThan": {"s3:object-lock-remaining-retention-days": "731"}}}]],
		[3, [{"op": "remove", "path": "/Condition"}]],
		[4, [{"op": "replace", "path": "/Condition", "value": {"Null": {"s3:object-lock-mode": "false"}}}]],
		[6, [{"op": "replace", "path": "/Condition/Null/s3:object-lock-legal-hold", "value": "true"}]],
	] {
		i := pair[0]
		altered := json.patch(denies[i], pair[1])
		rest := [d | some j, d in denies; j != i]
		"[PLAN-AUDIT-03] " in ids(replace(valid, bucket_policy(array.concat(rest, [altered, trail_write, config_write]))))
	}
}

test_violation_policy_unreadable if {
	"[PLAN-AUDIT-03] " in ids(replace(valid, change(decl.bucket_policy, {"policy": "not json"})))
	"[PLAN-AUDIT-05] " in ids(replace(valid, change(decl.key_policy, {"policy": "not json"})))
}

test_violation_bucket_arn_unknown if {
	"[PLAN-AUDIT-03] " in ids(replace(valid, change(decl.bucket, {"object_lock_enabled": true})))
}

test_violation_no_trail_on_either_side if {
	"[PLAN-AUDIT-05] " in ids(replace(replace(valid, bucket_policy(array.concat(denies, [config_write]))), key_policy([key_config])))
}

test_violation_config_accounts_differ if {
	other := json.patch(key_config, [{"op": "replace", "path": "/Condition/StringEquals/aws:SourceAccount", "value": ["333333333333"]}])
	"[PLAN-AUDIT-05] " in ids(replace(valid, key_policy([key_trail, other])))
}
