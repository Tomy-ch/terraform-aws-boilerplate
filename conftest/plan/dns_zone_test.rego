package plan.dns_zone_test

import data.plan.fixture
import data.plan.dns_zone as p

lg_arn := "arn:aws:logs:us-east-1:123456789012:log-group:/aws/route53/example.com"

query_key_arn := "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"

dnssec_key_arn := "arn:aws:kms:us-east-1:123456789012:key/abcd1234-12ab-34cd-56ef-1234567890ab"

dz := {
	"zone": "module.d.module.zone.aws_route53_zone.this",
	"query_log": "module.d.module.zone.aws_route53_query_log.this",
	"query_log_group": "module.d.module.query_log.module.log_group.aws_cloudwatch_log_group.this",
	"query_log_key": "module.d.module.query_log.module.kms.aws_kms_key.this",
	"query_log_resource_policy": "module.d.module.query_log.aws_cloudwatch_log_resource_policy.this",
	"dnssec_key": "module.d.module.dnssec_key.aws_kms_key.this",
	"dnssec_key_policy": "module.d.module.dnssec_key.aws_kms_key_policy.this",
	"key_signing_key": "module.d.module.zone.aws_route53_key_signing_key.this",
	"hosted_zone_dnssec": "module.d.module.zone.aws_route53_hosted_zone_dnssec.this",
	"alarms": {"address": "module.d.module.zone.aws_cloudwatch_metric_alarm.dnssec", "count": 2},
	"delegation_ns": {"address": "module.d.module.zone.aws_route53_record.delegation_ns", "count": 2},
	"delegation_ds": {"address": "module.d.module.zone.aws_route53_record.delegation_ds", "count": 1},
	"module_prefix": "module.d.",
	"allowed_types": [
		"aws_cloudwatch_log_group", "aws_cloudwatch_log_resource_policy", "aws_cloudwatch_metric_alarm", "aws_kms_alias", "aws_kms_key",
		"aws_kms_key_policy", "aws_route53_hosted_zone_dnssec", "aws_route53_key_signing_key", "aws_route53_query_log",
		"aws_route53_record", "aws_route53_zone",
	],
}

decl := {"dns_zone": dz}

rc(type, address, after) := {"address": address, "type": type, "change": {"actions": ["create"], "after": after, "after_unknown": {}}}

zone_after := {"name": "example.com", "force_destroy": false, "enable_accelerated_recovery": false}

log_statement := {
	"Sid": "Route53QueryLogs",
	"Effect": "Allow",
	"Principal": {"Service": "route53.amazonaws.com"},
	"Action": ["logs:CreateLogStream", "logs:PutLogEvents"],
	"Resource": concat("", [lg_arn, ":*"]),
	"Condition": {"StringEquals": {"aws:SourceAccount": "123456789012"}},
}

log_policy_with(statements) := rc("aws_cloudwatch_log_resource_policy", dz.query_log_resource_policy, {"region": "us-east-1", "policy_document": json.marshal({"Version": "2012-10-17", "Statement": statements})})

admin_statement := {
	"Sid": "AccountKeyAdministration",
	"Effect": "Allow",
	"Principal": {"AWS": "arn:aws:iam::123456789012:root"},
	"Action": ["kms:DescribeKey"],
	"Resource": "*",
}

sign_statement := {
	"Sid": "Route53DnssecService",
	"Effect": "Allow",
	"Principal": {"Service": "dnssec-route53.amazonaws.com"},
	"Action": ["kms:DescribeKey", "kms:GetPublicKey", "kms:Sign"],
	"Resource": "*",
	"Condition": {"StringEquals": {"aws:SourceAccount": "123456789012"}},
}

grant_statement := {
	"Sid": "Route53DnssecGrant",
	"Effect": "Allow",
	"Principal": {"Service": "dnssec-route53.amazonaws.com"},
	"Action": "kms:CreateGrant",
	"Resource": "*",
	"Condition": {
		"Bool": {"kms:GrantIsForAWSResource": "true"},
		"StringEquals": {"aws:SourceAccount": "123456789012"},
	},
}

key_policy_with(statements) := rc("aws_kms_key_policy", dz.dnssec_key_policy, {"policy": json.marshal({"Version": "2012-10-17", "Statement": statements})})

dnssec_key_after := {"arn": dnssec_key_arn, "key_usage": "SIGN_VERIFY", "customer_master_key_spec": "ECC_NIST_P256", "region": "us-east-1", "multi_region": false, "enable_key_rotation": false}

alarm(key, metric) := rc("aws_cloudwatch_metric_alarm", sprintf("module.d.module.zone.aws_cloudwatch_metric_alarm.dnssec[\"%s\"]", [key]), {"namespace": "AWS/Route53", "metric_name": metric, "region": "us-east-1", "dimensions": {}})

alarm_unknown_dimension(key, metric) := object.union(alarm(key, metric), {"change": {"after_unknown": {"dimensions": {"HostedZoneId": true}}}})

record(key, type, name) := rc("aws_route53_record", sprintf("module.d.module.zone.aws_route53_record.delegation_%s", [key]), {"type": type, "name": name})

delegation(kind, key, type) := rc("aws_route53_record", sprintf("module.d.module.zone.aws_route53_record.delegation_%s[\"%s\"]", [kind, key]), {"type": type, "name": sprintf("%s.example.com", [key])})

base := {
	"zone": rc("aws_route53_zone", dz.zone, zone_after),
	"ql": rc("aws_route53_query_log", dz.query_log, {"cloudwatch_log_group_arn": lg_arn}),
	"lg": rc("aws_cloudwatch_log_group", dz.query_log_group, {"arn": lg_arn, "region": "us-east-1", "kms_key_id": query_key_arn, "retention_in_days": 365}),
	"qkey": rc("aws_kms_key", dz.query_log_key, {"arn": query_key_arn, "region": "us-east-1"}),
	"lrp": log_policy_with([log_statement]),
	"dkey": rc("aws_kms_key", dz.dnssec_key, dnssec_key_after),
	"dkp": key_policy_with([admin_statement, sign_statement, grant_statement]),
	"ksk": rc("aws_route53_key_signing_key", dz.key_signing_key, {"status": "ACTIVE", "key_management_service_arn": dnssec_key_arn}),
	"dnssec": rc("aws_route53_hosted_zone_dnssec", dz.hosted_zone_dnssec, {"signing_status": "SIGNING"}),
	"a0": alarm_unknown_dimension("internal_failure", "DNSSECInternalFailure"),
	"a1": alarm_unknown_dimension("ksk_action", "DNSSECKeySigningKeysNeedingAction"),
	"ns": delegation("ns", "dev", "NS"),
	"ns2": delegation("ns", "stg", "NS"),
	"ds": delegation("ds", "dev", "DS"),
}

input_of(m) := fixture.plan([r | some r in m])

denies(m) := msgs if {
	msgs := p.deny with input as input_of(m) with data.policy_plan as decl
}

denies_with(m, d) := msgs if {
	msgs := p.deny with input as input_of(m) with data.policy_plan as {"dns_zone": d}
}

# object.union は入れ子の object を合わせるので、キーを除いてから置き換える。
put(m, k, v) := object.union(object.remove(m, [k]), {k: v})

with_after(r, attrs) := put(r, "change", put(r.change, "after", object.union(r.change.after, attrs)))

without_after(r, attr) := put(r, "change", put(r.change, "after", object.remove(r.change.after, [attr])))

test_pass_reference_shape if {
	count(denies(base)) == 0
}

test_pass_zone_id_dimension_when_known if {
	known := object.union(alarm("internal_failure", "DNSSECInternalFailure"), {"change": {"after": {"dimensions": {"HostedZoneId": "Z0123456789ABCDEFGHIJ"}}}})
	zone := with_after(base.zone, {"zone_id": "Z0123456789ABCDEFGHIJ"})
	m := put(put(base, "a0", known), "zone", zone)
	count(denies(m)) == 0
}

test_violation_delegation_record_count_differs if {
	fixture.has("[PLAN-DNS-00]", denies(object.remove(base, ["ns"])))
	fixture.has("[PLAN-DNS-00]", denies(object.remove(base, ["ds"])))
	fixture.has("[PLAN-DNS-00]", denies(object.remove(base, ["ns", "ns2", "ds"])))
	fixture.has("[PLAN-DNS-00]", denies(put(base, "ns3", delegation("ns", "prd", "NS"))))
	fixture.has("[PLAN-DNS-00]", denies(put(base, "ds2", delegation("ds", "stg", "DS"))))
}

test_violation_delegation_declaration_malformed if {
	every k in ["delegation_ns", "delegation_ds"] {
		fixture.has("[PLAN-DNS-00]", denies_with(base, object.remove(dz, [k])))
		fixture.has("[PLAN-DNS-00]", denies_with(base, put(dz, k, {"address": dz[k].address, "count": 0})))
		fixture.has("[PLAN-DNS-00]", denies_with(base, put(dz, k, {"address": "module.other.aws_route53_record.x", "count": 1})))
		every c in ["1", null, -1, 1.5] {
			fixture.has("[PLAN-DNS-00]", denies_with(base, put(dz, k, {"address": dz[k].address, "count": c})))
		}
		every a in [null, 1, {}] {
			fixture.has("[PLAN-DNS-00]", denies_with(base, put(dz, k, {"address": a, "count": 1})))
		}
	}
}

test_violation_declared_delegation_has_wrong_type if {
	every k, type in {"ns": "aws_route53_zone", "ds": "aws_route53_query_log"} {
		fixture.has("[PLAN-DNS-00]", denies(put(base, k, rc(type, base[k].address, base[k].change.after))))
	}
}

test_pass_record_deeper_than_one_label if {
	count(denies(put(base, "ns", record("ns", "NS", "a.dev.example.com")))) == 0
}

# PLAN-DNS-00
test_violation_missing_declaration if {
	msgs := p.deny with input as input_of(base) with data.policy_plan as {}
	fixture.has("[PLAN-DNS-00]", msgs)
}

test_violation_each_declaration_key_missing if {
	every k in ["zone", "query_log", "query_log_group", "query_log_key", "query_log_resource_policy", "dnssec_key", "dnssec_key_policy", "key_signing_key", "hosted_zone_dnssec", "alarms", "delegation_ns", "delegation_ds", "module_prefix", "allowed_types"] {
		fixture.has("[PLAN-DNS-00]", denies_with(base, object.remove(dz, [k])))
	}
}

test_violation_malformed_declaration_values if {
	fixture.has("[PLAN-DNS-00]", denies_with(base, put(dz, "module_prefix", "")))
	fixture.has("[PLAN-DNS-00]", denies_with(base, put(dz, "module_prefix", "module.d")))
	fixture.has("[PLAN-DNS-00]", denies_with(base, put(dz, "alarms", {"address": dz.alarms.address, "count": "2"})))
	fixture.has("[PLAN-DNS-00]", denies_with(base, put(dz, "alarms", {"address": dz.alarms.address, "count": 0})))
	fixture.has("[PLAN-DNS-00]", denies_with(base, put(dz, "allowed_types", [])))
	fixture.has("[PLAN-DNS-00]", denies_with(base, put(dz, "zone", "module.other.aws_route53_zone.this")))
}

test_violation_empty_input if {
	msgs := p.deny with input as fixture.plan([]) with data.policy_plan as decl
	fixture.has("[PLAN-DNS-00]", msgs)
}

test_violation_each_declared_resource_absent if {
	every k in ["zone", "ql", "lg", "qkey", "lrp", "dkey", "dkp", "ksk", "dnssec"] {
		fixture.has("[PLAN-DNS-00]", denies(object.remove(base, [k])))
	}
}

test_violation_declared_address_has_wrong_type if {
	every k, type in {"zone": "aws_route53_query_log", "ql": "aws_route53_zone", "lg": "aws_kms_key", "qkey": "aws_cloudwatch_log_group", "lrp": "aws_kms_key_policy", "dkey": "aws_cloudwatch_log_group", "dkp": "aws_cloudwatch_log_resource_policy", "ksk": "aws_route53_hosted_zone_dnssec", "dnssec": "aws_route53_key_signing_key"} {
		fixture.has("[PLAN-DNS-00]", denies(put(base, k, rc(type, base[k].address, base[k].change.after))))
	}
}

test_violation_declared_alarm_has_wrong_type if {
	a := rc("aws_cloudwatch_log_group", base.a0.address, base.a0.change.after)
	fixture.has("[PLAN-DNS-00]", denies(put(base, "a0", a)))
}

test_violation_declared_resource_only_deleted if {
	every k in ["zone", "ql", "lg", "qkey", "lrp", "dkey", "dkp", "ksk", "dnssec"] {
		fixture.has("[PLAN-DNS-00]", denies(put(base, k, put(base[k], "change", put(base[k].change, "actions", ["delete"])))))
	}
}

test_violation_alarm_only_deleted if {
	a := put(base.a0, "change", put(base.a0.change, "actions", ["delete"]))
	fixture.has("[PLAN-DNS-00]", denies(put(base, "a0", a)))
}

test_violation_zero_alarms if {
	fixture.has("[PLAN-DNS-00]", denies(object.remove(base, ["a0", "a1"])))
}

test_violation_second_declared_name_outside_the_prefix if {
	fixture.has("[PLAN-DNS-00]", denies_with(base, put(dz, "query_log_key", "module.other.aws_kms_key.this")))
}

test_violation_alarms_address_outside_the_prefix if {
	fixture.has("[PLAN-DNS-00]", denies_with(base, put(dz, "alarms", {"address": "module.other.aws_cloudwatch_metric_alarm.dnssec", "count": 2})))
}

test_violation_allowed_types_not_an_array if {
	fixture.has("[PLAN-DNS-00]", denies_with(base, put(dz, "allowed_types", "aws_route53_zone")))
}

test_violation_alarm_count_differs if {
	fixture.has("[PLAN-DNS-00]", denies(object.remove(base, ["a1"])))
	fixture.has("[PLAN-DNS-00]", denies(put(base, "a2", alarm_unknown_dimension("extra", "DNSSECInternalFailure"))))
}

# PLAN-DNS-01
test_violation_zone_force_destroy_true if {
	fixture.has("[PLAN-DNS-01]", denies(put(base, "zone", with_after(base.zone, {"force_destroy": true}))))
}

test_violation_zone_force_destroy_omitted if {
	fixture.has("[PLAN-DNS-01]", denies(put(base, "zone", without_after(base.zone, "force_destroy"))))
}

test_violation_zone_force_destroy_null if {
	fixture.has("[PLAN-DNS-01]", denies(put(base, "zone", with_after(base.zone, {"force_destroy": null}))))
}

test_violation_zone_accelerated_recovery_enabled if {
	fixture.has("[PLAN-DNS-01]", denies(put(base, "zone", with_after(base.zone, {"enable_accelerated_recovery": true}))))
}

test_violation_zone_accelerated_recovery_omitted if {
	fixture.has("[PLAN-DNS-01]", denies(put(base, "zone", without_after(base.zone, "enable_accelerated_recovery"))))
}

# PLAN-DNS-02
test_violation_query_log_points_to_other_group if {
	ql := rc("aws_route53_query_log", dz.query_log, {"cloudwatch_log_group_arn": "arn:aws:logs:us-east-1:123456789012:log-group:/aws/other"})
	fixture.has("[PLAN-DNS-02]", denies(put(base, "ql", ql)))
	fixture.has("[PLAN-DNS-02]", denies(put(base, "ql", rc("aws_route53_query_log", dz.query_log, {}))))
}

test_violation_log_group_region if {
	fixture.has("[PLAN-DNS-02]", denies(put(base, "lg", with_after(base.lg, {"region": "ap-northeast-1"}))))
	fixture.has("[PLAN-DNS-02]", denies(put(base, "lg", without_after(base.lg, "region"))))
}

test_violation_resource_policy_region if {
	fixture.has("[PLAN-DNS-02]", denies(put(base, "lrp", with_after(base.lrp, {"region": "ap-northeast-1"}))))
	fixture.has("[PLAN-DNS-02]", denies(put(base, "lrp", without_after(base.lrp, "region"))))
}

test_violation_log_group_other_key if {
	lg := with_after(base.lg, {"kms_key_id": "arn:aws:kms:us-east-1:123456789012:key/00000000-0000-0000-0000-000000000000"})
	fixture.has("[PLAN-DNS-02]", denies(put(base, "lg", lg)))
}

test_violation_query_log_and_group_arn_both_unknown if {
	m := put(put(base, "ql", rc("aws_route53_query_log", dz.query_log, {})), "lg", without_after(base.lg, "arn"))
	fixture.has("[PLAN-DNS-02]", denies(m))
}

test_violation_resource_policy_without_statement if {
	r := rc("aws_cloudwatch_log_resource_policy", dz.query_log_resource_policy, {"region": "us-east-1", "policy_document": json.marshal({"Version": "2012-10-17"})})
	fixture.has("[PLAN-DNS-02]", denies(put(base, "lrp", r)))
}

test_violation_resource_policy_source_account_boundaries if {
	every v in ["12345678901", "1234567890123", ["123456789012"], "123456789012\n", 123456789012] {
		sts := [put(log_statement, "Condition", {"StringEquals": {"aws:SourceAccount": v}})]
		fixture.has("[PLAN-DNS-02]", denies(put(base, "lrp", log_policy_with(sts))))
	}
}

test_violation_query_log_key_region if {
	fixture.has("[PLAN-DNS-02]", denies(put(base, "qkey", rc("aws_kms_key", dz.query_log_key, {"arn": query_key_arn, "region": "ap-northeast-1"}))))
	fixture.has("[PLAN-DNS-02]", denies(put(base, "qkey", rc("aws_kms_key", dz.query_log_key, {"arn": query_key_arn}))))
}

test_violation_log_group_without_key if {
	fixture.has("[PLAN-DNS-02]", denies(put(base, "lg", without_after(base.lg, "kms_key_id"))))
}

test_violation_log_group_and_key_both_unknown if {
	lg := with_after(base.lg, {"kms_key_id": null})
	fixture.has("[PLAN-DNS-02]", denies(put(put(base, "lg", lg), "qkey", rc("aws_kms_key", dz.query_log_key, {}))))
}

test_violation_retention_short if {
	fixture.has("[PLAN-DNS-02]", denies(put(base, "lg", with_after(base.lg, {"retention_in_days": 364}))))
	fixture.has("[PLAN-DNS-02]", denies(put(base, "lg", with_after(base.lg, {"retention_in_days": 0}))))
	fixture.has("[PLAN-DNS-02]", denies(put(base, "lg", without_after(base.lg, "retention_in_days"))))
}

test_pass_retention_longer if {
	count(denies(put(base, "lg", with_after(base.lg, {"retention_in_days": 400})))) == 0
}

test_violation_resource_policy_variants if {
	extra := {"Effect": "Allow", "Principal": {"Service": "route53.amazonaws.com"}, "Action": "logs:PutLogEvents", "Resource": concat("", [lg_arn, ":*"]), "Condition": {"StringEquals": {"aws:SourceAccount": "123456789012"}}}
	every sts in [
		[object.union(log_statement, {"Resource": "*"})],
		[object.union(log_statement, {"Resource": lg_arn})],
		[object.union(log_statement, {"Resource": "arn:aws:logs:us-east-1:123456789012:log-group:/aws/other:*"})],
		[object.union(log_statement, {"Principal": {"Service": "delivery.logs.amazonaws.com"}})],
		[object.union(log_statement, {"Principal": {"Service": ["route53.amazonaws.com", "logs.amazonaws.com"]}})],
		[object.union(log_statement, {"Principal": {"AWS": "*"}})],
		[object.union(log_statement, {"Principal": "*"})],
		[object.union(log_statement, {"Action": ["logs:CreateLogStream", "logs:PutLogEvents", "logs:DeleteLogGroup"]})],
		[object.union(log_statement, {"Action": ["logs:PutLogEvents"]})],
		[object.union(log_statement, {"Action": "logs:*"})],
		[object.union(log_statement, {"Effect": "Deny"})],
		[put(log_statement, "Condition", {})],
		[put(log_statement, "Condition", {"StringEquals": {"aws:SourceAccount": "*"}})],
		[put(log_statement, "Condition", {"StringEquals": {"aws:SourceAccount": "123456789012", "aws:SourceOrgID": "o-x"}})],
		[put(log_statement, "Condition", {"StringEquals": {"aws:SourceAccount": "123456789012"}, "Bool": {"aws:SecureTransport": "true"}})],
		[log_statement, extra],
		[],
	] {
		fixture.has("[PLAN-DNS-02]", denies(put(base, "lrp", log_policy_with(sts))))
	}
}

test_violation_resource_policy_unreadable if {
	fixture.has("[PLAN-DNS-02]", denies(put(base, "lrp", rc("aws_cloudwatch_log_resource_policy", dz.query_log_resource_policy, {"region": "us-east-1"}))))
	fixture.has("[PLAN-DNS-02]", denies(put(base, "lrp", rc("aws_cloudwatch_log_resource_policy", dz.query_log_resource_policy, {"region": "us-east-1", "policy_document": "{"}))))
}

# PLAN-DNS-03
test_violation_dnssec_key_attributes if {
	every attr, bad in {"key_usage": "ENCRYPT_DECRYPT", "customer_master_key_spec": "SYMMETRIC_DEFAULT", "region": "ap-northeast-1", "multi_region": true} {
		fixture.has("[PLAN-DNS-03]", denies(put(base, "dkey", with_after(base.dkey, {attr: bad}))))
		fixture.has("[PLAN-DNS-03]", denies(put(base, "dkey", without_after(base.dkey, attr))))
	}
}

test_violation_dnssec_key_rotation_enabled if {
	fixture.has("[PLAN-DNS-03]", denies(put(base, "dkey", with_after(base.dkey, {"enable_key_rotation": true}))))
}

test_pass_dnssec_key_rotation_omitted if {
	count(denies(put(base, "dkey", without_after(base.dkey, "enable_key_rotation")))) == 0
}

test_violation_key_policy_service_statements if {
	every sts in [
		[admin_statement, sign_statement],
		[admin_statement, grant_statement],
		[admin_statement],
		[admin_statement, sign_statement, grant_statement, sign_statement],
		[admin_statement, sign_statement, grant_statement, object.union(sign_statement, {"Principal": {"Service": "route53.amazonaws.com"}})],
		[admin_statement, object.union(sign_statement, {"Action": ["kms:DescribeKey", "kms:GetPublicKey", "kms:Sign", "kms:Decrypt"]}), grant_statement],
		[admin_statement, object.union(sign_statement, {"Action": ["kms:DescribeKey", "kms:Sign"]}), grant_statement],
		[admin_statement, object.union(sign_statement, {"Effect": "Deny"}), grant_statement],
		[admin_statement, put(sign_statement, "Condition", {}), grant_statement],
		[admin_statement, put(sign_statement, "Condition", {"StringEquals": {"aws:SourceAccount": "*"}}), grant_statement],
		[admin_statement, put(sign_statement, "Condition", {"StringEquals": {"aws:SourceArn": "arn:aws:route53:::hostedzone/*"}}), grant_statement],
		[admin_statement, sign_statement, put(grant_statement, "Condition", {"StringEquals": {"aws:SourceAccount": "123456789012"}})],
		[admin_statement, sign_statement, put(grant_statement, "Condition", {"Bool": {"kms:GrantIsForAWSResource": "true"}})],
		[admin_statement, sign_statement, put(grant_statement, "Condition", {"Bool": {"kms:GrantIsForAWSResource": "false"}, "StringEquals": {"aws:SourceAccount": "123456789012"}})],
		[admin_statement, sign_statement, object.union(grant_statement, {"Action": ["kms:CreateGrant", "kms:Decrypt"]})],
		[admin_statement, sign_statement, grant_statement, object.union(grant_statement, {"Principal": {"Service": "logs.us-east-1.amazonaws.com"}})],
	] {
		fixture.has("[PLAN-DNS-03]", denies(put(base, "dkp", key_policy_with(sts))))
	}
}

test_violation_key_policy_without_statement if {
	r := rc("aws_kms_key_policy", dz.dnssec_key_policy, {"policy": json.marshal({"Version": "2012-10-17"})})
	fixture.has("[PLAN-DNS-03]", denies(put(base, "dkp", r)))
}

test_violation_key_policy_source_account_boundaries if {
	every v in ["12345678901", "1234567890123", ["123456789012"], 123456789012] {
		sign := put(sign_statement, "Condition", {"StringEquals": {"aws:SourceAccount": v}})
		grant := put(grant_statement, "Condition", {"Bool": {"kms:GrantIsForAWSResource": "true"}, "StringEquals": {"aws:SourceAccount": v}})
		fixture.has("[PLAN-DNS-03]", denies(put(base, "dkp", key_policy_with([admin_statement, sign, grant_statement]))))
		fixture.has("[PLAN-DNS-03]", denies(put(base, "dkp", key_policy_with([admin_statement, sign_statement, grant]))))
	}
}

test_violation_key_policy_extra_statement_mixing_service_and_aws_principal if {
	extra := {"Effect": "Allow", "Principal": {"AWS": "*", "Service": "dnssec-route53.amazonaws.com"}, "Action": "kms:Sign", "Resource": "*"}
	fixture.has("[PLAN-DNS-03]", denies(put(base, "dkp", key_policy_with([admin_statement, sign_statement, grant_statement, extra]))))
}

test_violation_key_policy_unreadable if {
	fixture.has("[PLAN-DNS-03]", denies(put(base, "dkp", rc("aws_kms_key_policy", dz.dnssec_key_policy, {}))))
	fixture.has("[PLAN-DNS-03]", denies(put(base, "dkp", rc("aws_kms_key_policy", dz.dnssec_key_policy, {"policy": "{"}))))
}

test_violation_ksk_uses_other_key if {
	ksk := with_after(base.ksk, {"key_management_service_arn": query_key_arn})
	fixture.has("[PLAN-DNS-03]", denies(put(base, "ksk", ksk)))
	fixture.has("[PLAN-DNS-03]", denies(put(base, "ksk", without_after(base.ksk, "key_management_service_arn"))))
}

test_violation_ksk_and_key_arn_both_unknown if {
	m := put(put(base, "ksk", with_after(base.ksk, {"key_management_service_arn": null})), "dkey", without_after(base.dkey, "arn"))
	fixture.has("[PLAN-DNS-03]", denies(m))
}

# PLAN-DNS-04
test_violation_ksk_status if {
	fixture.has("[PLAN-DNS-04]", denies(put(base, "ksk", with_after(base.ksk, {"status": "INACTIVE"}))))
	fixture.has("[PLAN-DNS-04]", denies(put(base, "ksk", without_after(base.ksk, "status"))))
}

test_violation_signing_status if {
	fixture.has("[PLAN-DNS-04]", denies(put(base, "dnssec", with_after(base.dnssec, {"signing_status": "NOT_SIGNING"}))))
	fixture.has("[PLAN-DNS-04]", denies(put(base, "dnssec", without_after(base.dnssec, "signing_status"))))
}

test_violation_alarm_metric_set if {
	fixture.has("[PLAN-DNS-04]", denies(put(base, "a1", alarm_unknown_dimension("ksk_action", "DNSSECInternalFailure"))))
	fixture.has("[PLAN-DNS-04]", denies(put(base, "a1", alarm_unknown_dimension("ksk_action", "DNSSECKeySigningKeysNeedingActionX"))))
}

test_violation_alarm_namespace_and_region if {
	every attr, bad in {"namespace": "AWS/EC2", "region": "ap-northeast-1"} {
		fixture.has("[PLAN-DNS-04]", denies(put(base, "a0", with_after(base.a0, {attr: bad}))))
		fixture.has("[PLAN-DNS-04]", denies(put(base, "a0", without_after(base.a0, attr))))
	}
}

test_violation_alarm_known_dimension_while_zone_id_unknown if {
	known := object.union(alarm("internal_failure", "DNSSECInternalFailure"), {"change": {"after": {"dimensions": {"HostedZoneId": "Z0123456789ABCDEFGHIJ"}}}})
	fixture.has("[PLAN-DNS-04]", denies(put(base, "a0", known)))
}

test_violation_alarm_dimension if {
	fixture.has("[PLAN-DNS-04]", denies(put(base, "a0", alarm("internal_failure", "DNSSECInternalFailure"))))
	other := object.union(alarm("internal_failure", "DNSSECInternalFailure"), {"change": {"after": {"dimensions": {"HostedZoneId": "Z-OTHER"}}}})
	zone := with_after(base.zone, {"zone_id": "Z0123456789ABCDEFGHIJ"})
	fixture.has("[PLAN-DNS-04]", denies(put(put(base, "a0", other), "zone", zone)))
}

# PLAN-DNS-05
repeat_a(n) := concat("", [x | numbers.range(1, n)[_]; x := "a"])

label63 := sprintf("%s.example.com", [repeat_a(63)])

label64 := sprintf("%s.example.com", [repeat_a(64)])

name253 := sprintf("%s.%s.%s.%s.example.com", [repeat_a(63), repeat_a(63), repeat_a(63), repeat_a(49)])

name254 := sprintf("%s.%s.%s.%s.example.com", [repeat_a(63), repeat_a(63), repeat_a(63), repeat_a(50)])

test_violation_record_type if {
	every t in ["A", "AAAA", "CNAME", "TXT", "MX", "SOA", "CAA"] {
		fixture.has("[PLAN-DNS-05]", denies(put(base, "x", record("x", t, "dev.example.com"))))
	}
}

test_violation_record_name if {
	every n in ["example.com", "EXAMPLE.com", "dev.other.com", "devexample.com", "*.example.com", "*.dev.example.com", "dev.example.com.evil.net", "example.com.", "", ".example.com", "a..example.com", "-a.example.com", "a-.example.com", "a b.example.com", "\\052.dev.example.com", label64, name254] {
		fixture.has("[PLAN-DNS-05]", denies(put(base, "x", record("x", "NS", n))))
	}
}

test_pass_record_name_at_dns_length_limits if {
	every n in [label63, name253, "a.example.com", "1.example.com", "_a_.example.com"] {
		count(denies(put(base, "x", record("x", "NS", n)))) == 0
	}
}

test_violation_record_name_omitted if {
	x := without_after(record("x", "NS", "dev.example.com"), "name")
	fixture.has("[PLAN-DNS-05]", denies(put(base, "x", x)))
}

test_violation_record_when_zone_name_unknown if {
	fixture.has("[PLAN-DNS-05]", denies(put(base, "zone", without_after(base.zone, "name"))))
}

test_violation_record_type_omitted if {
	fixture.has("[PLAN-DNS-05]", denies(put(base, "x", without_after(record("x", "NS", "dev.example.com"), "type"))))
}

test_pass_zone_name_mixed_case_with_lower_record if {
	m := put(base, "zone", with_after(base.zone, {"name": "Example.COM"}))
	count(denies(put(m, "x", record("x", "NS", "dev.example.com")))) == 0
}

test_pass_record_name_mixed_case if {
	count(denies(put(base, "x", record("x", "NS", "DEV.Example.com")))) == 0
}

test_pass_record_name_with_underscore_and_hyphen_labels if {
	count(denies(put(base, "x", record("x", "DS", "a-b_c.dev.example.com")))) == 0
}

test_pass_record_outside_the_module_is_not_ours if {
	count(denies(put(base, "x", rc("aws_route53_record", "module.other.aws_route53_record.a", {"type": "A", "name": "example.com"})))) == 0
}

# PLAN-DNS-06
test_violation_type_outside_the_declaration if {
	every t in ["aws_route53_zone_association", "aws_route53_records_exclusive", "aws_route53_delegation_set", "aws_route53_health_check", "aws_iam_role", "aws_route53_vpc_association_authorization"] {
		fixture.has("[PLAN-DNS-06]", denies(put(base, "x", rc(t, sprintf("module.d.module.zone.%s.this", [t]), {}))))
	}
}

test_violation_type_outside_the_declaration_under_indexed_module if {
	fixture.has("[PLAN-DNS-06]", denies(put(base, "x", rc("aws_route53_zone_association", "module.d[0].module.zone.aws_route53_zone_association.this", {}))))
}

test_pass_other_module_may_hold_other_types if {
	count(denies(put(base, "x", rc("aws_route53_zone_association", "module.e.aws_route53_zone_association.this", {})))) == 0
}
