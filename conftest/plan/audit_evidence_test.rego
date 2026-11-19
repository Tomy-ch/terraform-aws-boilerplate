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
	found := p.deny with input as fixture.plan(rcs) with data.policy_plan as {"audit_evidence": decl}
}

ids(rcs) := {substring(m, 0, 16) | some m in msgs(rcs)}

# 同じ ID を持つ別の規則で通らないよう、規則に固有の文言で見る。
said(rcs, fragment) if {
	some m in msgs(rcs)
	contains(m, fragment)
}

test_pass_valid if {
	count(msgs(valid)) == 0
}

test_violation_declaration_missing if {
	found := p.deny with input as fixture.plan(valid) with data.policy_plan as {"audit_evidence": object.remove(decl, ["key_policy"])}
	some m in found
	contains(m, "宣言 audit_evidence.key_policy が無いか")
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
	every k in ["aws:PrincipalArn", "aws:userid", "AWS:PrincipalAccount", "aws:PrincipalOrgID"] {
		extra := {"Sid": "DenyExtra", "Effect": "Deny", "Principal": "*", "Action": "s3:GetObject", "Resource": sprintf("%s/*", [bucket_arn]), "Condition": {"StringNotEquals": {k: "x"}}}
		said(replace(valid, bucket_policy(array.concat(denies, [extra, trail_write, config_write]))), "主体による例外")
	}
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

test_violation_each_declaration_missing_or_not_string if {
	every key, _ in decl {
		every bad in [null, 1, "", ["x"]] {
			found := p.deny with input as fixture.plan(valid) with data.policy_plan as {"audit_evidence": object.union(decl, {key: bad})}
			some m in found
			contains(m, sprintf("宣言 audit_evidence.%s が無いか", [key]))
		}
		found_missing := p.deny with input as fixture.plan(valid) with data.policy_plan as {"audit_evidence": object.remove(decl, [key])}
		some m2 in found_missing
		contains(m2, sprintf("宣言 audit_evidence.%s が無いか", [key]))
	}
}

test_violation_whole_declaration_absent if {
	found := p.deny with input as fixture.plan(valid) with data.policy_plan as {}
	count({m | some m in found; startswith(m, "[PLAN-AUDIT-00] 宣言 audit_evidence.")}) == count(decl)
}

test_violation_each_declared_address_absent if {
	every _, address in decl {
		said([x | some x in valid; x.address != address], sprintf("宣言した %s が入力にありません", [address]))
	}
}

test_violation_declared_address_without_after if {
	gone := {"address": decl.bucket_policy, "type": "t", "change": {"actions": ["create"], "after": null, "after_unknown": {}}}
	said(replace(valid, gone), sprintf("宣言した %s が入力にありません", [decl.bucket_policy]))
}

test_violation_retention_degenerate_shapes if {
	every rule in [
		[{"default_retention": [{"mode": "COMPLIANCE", "days": 731}]}, {"default_retention": [{"mode": "COMPLIANCE", "days": 731}]}],
		[{"default_retention": [{"mode": "COMPLIANCE", "days": 731}, {"mode": "COMPLIANCE", "days": 731}]}],
		[{"default_retention": [{"mode": "COMPLIANCE", "days": null, "years": 2}]}],
		[{"default_retention": [{"mode": "COMPLIANCE", "days": "731"}]}],
	] {
		said(replace(valid, change(decl.object_lock_configuration, {"rule": rule})), "既定の保持を1つ持ちません")
	}
}

test_pass_governance_one_day if {
	rcs := replace(replace(valid, lock("GOVERNANCE", 1)), lifecycle(1))
	altered := [d | some d in denies; not d.Sid in {"DenyRetentionBeyondDefault", "DenyOtherLockMode"}]
	retention := json.patch(denies[3], [{"op": "replace", "path": "/Condition/NumericGreaterThan/s3:object-lock-remaining-retention-days", "value": "1"}])
	mode := json.patch(denies[4], [{"op": "replace", "path": "/Condition/StringNotEquals/s3:object-lock-mode", "value": "GOVERNANCE"}])
	count(msgs(replace(rcs, bucket_policy(array.concat(altered, [retention, mode, trail_write, config_write]))))) == 0
}

test_violation_no_effective_expiration if {
	disabled := change(decl.lifecycle_configuration, {"rule": [{"id": "x", "status": "Disabled", "filter": [{"and": [], "tag": []}], "expiration": [{"days": 731}]}]})
	two := change(decl.lifecycle_configuration, {"rule": [
		{"id": "a", "status": "Enabled", "filter": [{"and": [], "tag": []}], "expiration": [{"days": 731}]},
		{"id": "b", "status": "Enabled", "filter": [{"and": [], "tag": []}], "expiration": [{"days": 800}]},
	]})
	none := change(decl.lifecycle_configuration, {"rule": []})
	every rc in [disabled, two, none] {
		said(replace(valid, rc), "現行版の失効の日数")
	}
}

test_violation_expiration_filter_shapes if {
	every f in [
		[{"and": [], "prefix": "AWSLogs/", "tag": []}],
		[{"and": [], "tag": [{"key": "k", "value": "v"}]}],
		[{"and": [{"prefix": "x"}], "tag": []}],
		[],
		[{"and": [], "tag": []}, {"and": [], "tag": []}],
	] {
		rc := change(decl.lifecycle_configuration, {"rule": [{"id": "x", "status": "Enabled", "filter": f, "expiration": [{"days": 731}]}]})
		said(replace(valid, rc), "Bucket 全体に掛かりません")
	}
}

test_pass_expiration_filter_empty_values if {
	rc := change(decl.lifecycle_configuration, {"rule": [{"id": "x", "status": "Enabled", "filter": [{"and": [], "prefix": "", "object_size_greater_than": null, "tag": []}], "expiration": [{"days": 731}]}]})
	count(msgs(replace(valid, rc))) == 0
}

test_violation_object_lock_flag_missing if {
	every after in [{"arn": bucket_arn}, {"arn": bucket_arn, "object_lock_enabled": null}] {
		said(replace(valid, change(decl.bucket, after)), "Object Lock を有効にしていません")
	}
}

test_violation_source_partly_wildcard if {
	c := {"StringEquals": {"s3:x-amz-acl": "bucket-owner-full-control", "aws:SourceArn": [trail, "*"]}}
	s := json.patch(trail_write, [{"op": "replace", "path": "/Condition", "value": c}])
	said(replace(valid, bucket_policy(array.concat(denies, [s, config_write]))), "wildcard と policy 変数の無い値に限っていません")
}

test_violation_allow_with_not_principal if {
	s := json.patch(trail_write, [{"op": "add", "path": "/NotPrincipal", "value": {"AWS": "arn:aws:iam::123456789012:root"}}])
	said(replace(valid, bucket_policy(array.concat(denies, [s, config_write]))), "サービス principal だけを名指していません")
}

test_violation_each_deny_altered if {
	every i, d in denies {
		every patch in [
			[{"op": "replace", "path": "/Action", "value": "s3:GetObject"}],
			[{"op": "replace", "path": "/Resource", "value": bucket_arn}],
			[{"op": "add", "path": "/Condition", "value": {"Bool": {"aws:SecureTransport": "true"}}}],
			[{"op": "add", "path": "/NotAction", "value": "s3:GetObject"}],
			[{"op": "add", "path": "/NotResource", "value": bucket_arn}],
		] {
			altered := json.patch(d, patch)
			rest := [x | some j, x in denies; j != i]
			said(replace(valid, bucket_policy(array.concat(rest, [altered, trail_write, config_write]))), sprintf("の Deny %s が、Principal", [d.Sid]))
		}
	}
}

test_violation_policy_missing if {
	said(replace(valid, change(decl.bucket_policy, {})), "policy が plan で決まらないか")
	said(replace(valid, change(decl.bucket_policy, {"policy": 1})), "policy が plan で決まらないか")
	said(replace(valid, change(decl.key_policy, {})), "policy が plan で決まらないか")
}

test_violation_trail_only_on_bucket_side if {
	said(replace(valid, key_policy([key_config])), "trail")
	said(replace(valid, key_policy([key_config])), "一致しません")
}

test_violation_config_only_on_one_side if {
	said(replace(valid, key_policy([key_trail])), "Config のアカウント")
	said(replace(valid, bucket_policy(array.concat(denies, [trail_write]))), "Config のアカウント")
}

test_pass_no_config_on_either_side if {
	count(msgs(replace(replace(valid, key_policy([key_trail])), bucket_policy(array.concat(denies, [trail_write]))))) == 0
}

# 形を見る Deny の一覧と、欠落を見る Sid の一覧が食い違うと、片方にだけある Deny は黙って検査の外に出る。
test_pass_expected_denies_match_required_sids if {
	sids := {sid | some sid, _ in p.expected_denies} with input as fixture.plan(valid) with data.policy_plan as {"audit_evidence": decl}
	sids == p.required_denies
	count(sids) == 8
}

# 同じ変数を Bucket Policy と Key Policy の両方に置くと 05 は一致と読むので、02 だけが落とす。
test_violation_source_policy_variable if {
	v := "arn:aws:cloudtrail:ap-northeast-1:${aws:SourceAccount}:trail/tabp-org"
	s := json.patch(trail_write, [{"op": "replace", "path": "/Condition", "value": {"StringEquals": {"s3:x-amz-acl": "bucket-owner-full-control", "aws:SourceArn": v}}}])
	k := json.patch(key_trail, [{"op": "replace", "path": "/Condition", "value": {"StringEquals": {"aws:SourceArn": [v], "kms:EncryptionContext:aws:cloudtrail:arn": [v]}}}])
	both := replace(replace(valid, bucket_policy(array.concat(denies, [s, config_write]))), key_policy([k, key_config]))
	ids(both) == {"[PLAN-AUDIT-02] "}
	said(both, "wildcard と policy 変数の無い値に限っていません")

	c := json.patch(config_write, [{"op": "replace", "path": "/Condition", "value": {"StringEquals": {"s3:x-amz-acl": "bucket-owner-full-control", "aws:SourceAccount": ["222222222222", "${aws:PrincipalAccount}"]}}}])
	mixed := replace(valid, bucket_policy(array.concat(denies, [trail_write, c])))
	"[PLAN-AUDIT-02] " in ids(mixed)
	said(mixed, "wildcard と policy 変数の無い値に限っていません")
}

test_violation_source_not_string if {
	c := json.patch(config_write, [{"op": "replace", "path": "/Condition", "value": {"StringEquals": {"s3:x-amz-acl": "bucket-owner-full-control", "aws:SourceAccount": 222222222222}}}])
	said(replace(valid, bucket_policy(array.concat(denies, [trail_write, c]))), "wildcard と policy 変数の無い値に限っていません")
}
