# dns-zone の public Hosted Zone の不変条件（dns-zone/ADR-0201 の置換と削除の拒否、dns-zone/ADR-0301 の
# query logging、dns-zone/ADR-0302 の DNSSEC、dns-zone/ADR-0102 の record の範囲、ADR-0301 決定12・18 の Key Policy）。Security の道具は zone の
# force_destroy・query log の宛先の resource policy・DNSSEC の鍵の Key Policy の service principal・委任の record の
# 名前を見ない（ADR-0402 決定3・4）。役割は宣言した address から引く（同 決定13）。
package plan.dns_zone

import data.plan.lib

cfg := object.get(data.policy_plan, "dns_zone", {})

single_names := ["zone", "query_log", "query_log_group", "query_log_key", "query_log_resource_policy", "dnssec_key", "dnssec_key_policy", "key_signing_key", "hosted_zone_dnssec"]

# Route 53 は公開 Hosted Zone の query log と DNSSEC の鍵を us-east-1 にしか受けない。
region := "us-east-1"

dnssec_service := "dnssec-route53.amazonaws.com"

dnssec_metrics := {"DNSSECInternalFailure", "DNSSECKeySigningKeysNeedingAction"}

after(address) := c.rc.change.after if {
	some c in lib.changes
	c.rc.address == address
}

present(address) if {
	some c in lib.changes
	c.rc.address == address
}

alarm_instances contains c.rc if {
	some c in lib.changes
	lib.strip_index(c.rc.address) == cfg.alarms.address
}

delegations := ["delegation_ns", "delegation_ds"]

delegation_instances(name) := {c.rc |
	some c in lib.changes
	lib.strip_index(c.rc.address) == cfg[name].address
}

# module の下の変更。宣言の外の型（zone の association・records の排他・delegation set など）を、型を列挙せずに拾う。
use_case_changes contains c.rc if {
	some c in lib.changes
	startswith(lib.strip_index(c.rc.address), cfg.module_prefix)
}

prefix_ok if {
	is_string(object.get(cfg, "module_prefix", null))
	startswith(cfg.module_prefix, "module.")
	endswith(cfg.module_prefix, ".")
}

well_formed if {
	prefix_ok
	every name in single_names {
		is_string(object.get(cfg, name, null))
		startswith(cfg[name], cfg.module_prefix)
	}
	is_string(object.get(object.get(cfg, "alarms", {}), "address", null))
	startswith(cfg.alarms.address, cfg.module_prefix)
	is_number(object.get(object.get(cfg, "alarms", {}), "count", null))
	round(cfg.alarms.count) == cfg.alarms.count
	cfg.alarms.count >= 1
	every name in delegations {
		is_string(object.get(object.get(cfg, name, {}), "address", null))
		startswith(cfg[name].address, cfg.module_prefix)
		is_number(object.get(object.get(cfg, name, {}), "count", null))
		round(cfg[name].count) == cfg[name].count
		cfg[name].count >= 1
	}
	is_array(object.get(cfg, "allowed_types", null))
	count(cfg.allowed_types) >= 1
}

deny contains msg if {
	not well_formed
	msg := "[PLAN-DNS-00] data.policy_plan.dns_zone の宣言が欠けているか形が違います"
}

deny contains msg if {
	well_formed
	some name in single_names
	not present(cfg[name])
	msg := sprintf("[PLAN-DNS-00] 宣言した %s（%s）が入力にありません", [name, cfg[name]])
}

declared_types := {
	"zone": "aws_route53_zone",
	"query_log": "aws_route53_query_log",
	"query_log_group": "aws_cloudwatch_log_group",
	"query_log_key": "aws_kms_key",
	"query_log_resource_policy": "aws_cloudwatch_log_resource_policy",
	"dnssec_key": "aws_kms_key",
	"dnssec_key_policy": "aws_kms_key_policy",
	"key_signing_key": "aws_route53_key_signing_key",
	"hosted_zone_dnssec": "aws_route53_hosted_zone_dnssec",
}

deny contains msg if {
	well_formed
	some name, type in declared_types
	some c in lib.changes
	c.rc.address == cfg[name]
	c.rc.type != type
	msg := sprintf("[PLAN-DNS-00] 宣言した %s（%s）の型が %s ではなく %s です", [name, cfg[name], type, c.rc.type])
}

deny contains msg if {
	well_formed
	some rc in alarm_instances
	rc.type != "aws_cloudwatch_metric_alarm"
	msg := sprintf("[PLAN-DNS-00] 宣言した alarm（%s）の型が aws_cloudwatch_metric_alarm ではなく %s です", [rc.address, rc.type])
}

deny contains msg if {
	well_formed
	some name in delegations
	some rc in delegation_instances(name)
	rc.type != "aws_route53_record"
	msg := sprintf("[PLAN-DNS-00] 宣言した委任の record（%s）の型が aws_route53_record ではなく %s です", [rc.address, rc.type])
}

deny contains msg if {
	well_formed
	some name in delegations
	count(delegation_instances(name)) != cfg[name].count
	msg := sprintf("[PLAN-DNS-00] 宣言した委任の record（%s）が %d 件ではなく %d 件です", [cfg[name].address, cfg[name].count, count(delegation_instances(name))])
}

deny contains msg if {
	well_formed
	count(alarm_instances) != cfg.alarms.count
	msg := sprintf("[PLAN-DNS-00] 宣言した alarm（%s）が %d 件ではなく %d 件です", [cfg.alarms.address, cfg.alarms.count, count(alarm_instances)])
}

# 管理外の record を含めて zone を消すと、委任の先を含む名前がすべて消える。
deny contains msg if {
	well_formed
	not object.get(after(cfg.zone), "force_destroy", null) == false
	msg := sprintf("[PLAN-DNS-01] zone（%s）の force_destroy が false ではありません", [cfg.zone])
}

deny contains msg if {
	well_formed
	not object.get(after(cfg.zone), "enable_accelerated_recovery", null) == false
	msg := sprintf("[PLAN-DNS-01] zone（%s）の enable_accelerated_recovery が false ではありません", [cfg.zone])
}

# query log の宛先は、アカウント単位の resource policy が許す Log Group に限られる。
deny contains msg if {
	well_formed
	not query_log_targets_group
	msg := sprintf("[PLAN-DNS-02] query log（%s）が、宣言した Log Group（%s）の ARN を指していません", [cfg.query_log, cfg.query_log_group])
}

query_log_targets_group if {
	arn := object.get(after(cfg.query_log_group), "arn", null)
	is_string(arn)
	object.get(after(cfg.query_log), "cloudwatch_log_group_arn", null) == arn
}

deny contains msg if {
	well_formed
	not object.get(after(cfg.query_log_group), "region", null) == region
	msg := sprintf("[PLAN-DNS-02] query log の Log Group（%s）の region が %s ではありません", [cfg.query_log_group, region])
}

deny contains msg if {
	well_formed
	not object.get(after(cfg.query_log_resource_policy), "region", null) == region
	msg := sprintf("[PLAN-DNS-02] query log の resource policy（%s）の region が %s ではありません", [cfg.query_log_resource_policy, region])
}

deny contains msg if {
	well_formed
	not object.get(after(cfg.query_log_key), "region", null) == region
	msg := sprintf("[PLAN-DNS-02] query log の鍵（%s）の region が %s ではありません", [cfg.query_log_key, region])
}

encrypted_with_declared_key if {
	k := object.get(after(cfg.query_log_group), "kms_key_id", null)
	arn := object.get(after(cfg.query_log_key), "arn", null)
	is_string(k)
	is_string(arn)
	k == arn
}

deny contains msg if {
	well_formed
	not encrypted_with_declared_key
	msg := sprintf("[PLAN-DNS-02] query log の Log Group（%s）が、宣言した鍵（%s）で暗号化されていません", [cfg.query_log_group, cfg.query_log_key])
}

retention_ok if {
	n := object.get(after(cfg.query_log_group), "retention_in_days", null)
	is_number(n)
	n >= 365
}

deny contains msg if {
	well_formed
	not retention_ok
	msg := sprintf("[PLAN-DNS-02] query log の Log Group（%s）の保持が365日以上ではありません", [cfg.query_log_group])
}

log_policy_document := json.unmarshal(object.get(after(cfg.query_log_resource_policy), "policy_document", "")) if {
	is_string(object.get(after(cfg.query_log_resource_policy), "policy_document", null))
	json.is_valid(after(cfg.query_log_resource_policy).policy_document)
}

log_policy_ok(body) if {
	statements := lib.statements(body)
	count(statements) == 1
	st := statements[0]
	st.Effect == "Allow"
	st.Principal == {"Service": "route53.amazonaws.com"}
	{a | some a in lib.as_array(st.Action)} == {"logs:CreateLogStream", "logs:PutLogEvents"}
	arn := object.get(after(cfg.query_log_group), "arn", null)
	is_string(arn)
	st.Resource == concat("", [arn, ":*"])
	regex.match(`^[0-9]{12}$`, st.Condition.StringEquals["aws:SourceAccount"])
	count(object.keys(st.Condition.StringEquals)) == 1
	count(object.keys(st.Condition)) == 1
}

log_policy_valid if log_policy_ok(log_policy_document)

# 文書が plan で決まらないか読めないときも落とす。CI の実 plan の経路は plan.input を掛けない（conftest/plan/README.md の「入力」）。
deny contains msg if {
	well_formed
	not log_policy_valid
	msg := sprintf("[PLAN-DNS-02] query log の resource policy（%s）が、route53.amazonaws.com に Log Group の ARN の :* への logs:CreateLogStream と logs:PutLogEvents だけを aws:SourceAccount の条件つきで許す1文ではありません", [cfg.query_log_resource_policy])
}

deny contains msg if {
	well_formed
	k := after(cfg.dnssec_key)
	some attr, want in {"key_usage": "SIGN_VERIFY", "customer_master_key_spec": "ECC_NIST_P256", "region": region, "multi_region": false}
	not object.get(k, attr, null) == want
	msg := sprintf("[PLAN-DNS-03] DNSSEC の鍵（%s）の %s が %v ではありません", [cfg.dnssec_key, attr, want])
}

# 非対称鍵は自動 rotation を持てない。true を書くと apply が失敗する。
deny contains msg if {
	well_formed
	object.get(after(cfg.dnssec_key), "enable_key_rotation", null) == true
	msg := sprintf("[PLAN-DNS-03] DNSSEC の鍵（%s）の enable_key_rotation が true です", [cfg.dnssec_key])
}

dnssec_key_policy_document := json.unmarshal(object.get(after(cfg.dnssec_key_policy), "policy", "")) if {
	is_string(object.get(after(cfg.dnssec_key_policy), "policy", null))
	json.is_valid(after(cfg.dnssec_key_policy).policy)
}

service_statements(body) := [st |
	some st in lib.statements(body)
	object.get(st, "Principal", null) != null
	is_object(st.Principal)
	"Service" in object.keys(st.Principal)
]

source_account_ok(st) if regex.match(`^[0-9]{12}$`, st.Condition.StringEquals["aws:SourceAccount"])

sign_statement(st) if {
	st.Effect == "Allow"
	st.Principal == {"Service": dnssec_service}
	{a | some a in lib.as_array(st.Action)} == {"kms:DescribeKey", "kms:GetPublicKey", "kms:Sign"}
	source_account_ok(st)
	count(object.keys(st.Condition)) == 1
	count(object.keys(st.Condition.StringEquals)) == 1
}

grant_statement(st) if {
	st.Effect == "Allow"
	st.Principal == {"Service": dnssec_service}
	{a | some a in lib.as_array(st.Action)} == {"kms:CreateGrant"}
	source_account_ok(st)
	st.Condition.Bool == {"kms:GrantIsForAWSResource": "true"}
	count(object.keys(st.Condition)) == 2
	count(object.keys(st.Condition.StringEquals)) == 1
}

key_policy_services_ok(body) if {
	sts := service_statements(body)
	count(sts) == 2
	count([st | some st in sts; sign_statement(st)]) == 1
	count([st | some st in sts; grant_statement(st)]) == 1
}

key_policy_valid if key_policy_services_ok(dnssec_key_policy_document)

deny contains msg if {
	well_formed
	not key_policy_valid
	msg := sprintf("[PLAN-DNS-03] DNSSEC の鍵の Key Policy（%s）の service principal の文が、%s の2文（kms:DescribeKey・kms:GetPublicKey・kms:Sign と、kms:GrantIsForAWSResource を条件にした kms:CreateGrant。どちらも aws:SourceAccount つき）だけではありません", [cfg.dnssec_key_policy, dnssec_service])
}

ksk_uses_declared_key if {
	arn := object.get(after(cfg.dnssec_key), "arn", null)
	is_string(arn)
	object.get(after(cfg.key_signing_key), "key_management_service_arn", null) == arn
}

deny contains msg if {
	well_formed
	not ksk_uses_declared_key
	msg := sprintf("[PLAN-DNS-03] KSK（%s）が、宣言した DNSSEC の鍵（%s）を使っていません", [cfg.key_signing_key, cfg.dnssec_key])
}

deny contains msg if {
	well_formed
	not object.get(after(cfg.key_signing_key), "status", null) == "ACTIVE"
	msg := sprintf("[PLAN-DNS-04] KSK（%s）の status が ACTIVE ではありません", [cfg.key_signing_key])
}

deny contains msg if {
	well_formed
	not object.get(after(cfg.hosted_zone_dnssec), "signing_status", null) == "SIGNING"
	msg := sprintf("[PLAN-DNS-04] DNSSEC（%s）の signing_status が SIGNING ではありません", [cfg.hosted_zone_dnssec])
}

alarm_metrics := {m |
	some rc in alarm_instances
	m := object.get(rc.change.after, "metric_name", null)
}

deny contains msg if {
	well_formed
	count(alarm_instances) == cfg.alarms.count
	alarm_metrics != dnssec_metrics
	msg := sprintf("[PLAN-DNS-04] alarm（%s）の metric 名の集合が、DNSSECInternalFailure と DNSSECKeySigningKeysNeedingAction ではありません", [cfg.alarms.address])
}

deny contains msg if {
	well_formed
	some rc in alarm_instances
	some attr, want in {"namespace": "AWS/Route53", "region": region}
	not object.get(rc.change.after, attr, null) == want
	msg := sprintf("[PLAN-DNS-04] alarm（%s）の %s が %s ではありません", [rc.address, attr, want])
}

# 作る zone の ID は apply まで決まらないので、plan で決まらない値か、zone の zone_id と一致する値を認める。
dimension_ok(rc) if object.get(object.get(rc.change, "after_unknown", {}), "dimensions", {}).HostedZoneId == true

dimension_ok(rc) if {
	id := object.get(after(cfg.zone), "zone_id", null)
	is_string(id)
	object.get(object.get(rc.change.after, "dimensions", {}), "HostedZoneId", null) == id
}

deny contains msg if {
	well_formed
	some rc in alarm_instances
	not dimension_ok(rc)
	msg := sprintf("[PLAN-DNS-04] alarm（%s）の dimension HostedZoneId が zone を指していません", [rc.address])
}

zone_name := lower(object.get(after(cfg.zone), "name", null)) if is_string(object.get(after(cfg.zone), "name", null))

records contains rc if {
	some rc in use_case_changes
	rc.type == "aws_route53_record"
}

# 委任の record だけを置く。apex 自身の record（SOA・NS を含む）と zone の外の名前、wildcard は置かない。
deny contains msg if {
	well_formed
	some rc in records
	not object.get(rc.change.after, "type", null) in {"NS", "DS"}
	msg := sprintf("[PLAN-DNS-05] record（%s）の type が NS でも DS でもありません", [rc.address])
}

deny contains msg if {
	well_formed
	some rc in records
	not is_string(zone_name)
	msg := sprintf("[PLAN-DNS-05] record（%s）を zone の名前と突き合わせられません。zone（%s）の name が plan で決まりません", [rc.address, cfg.zone])
}

dns_label := `[a-z0-9_]([a-z0-9_-]*[a-z0-9_])?`

record_name_ok(name) if {
	is_string(name)
	is_string(zone_name)
	suffix := concat("", [".", zone_name])
	endswith(lower(name), suffix)
	count(lower(name)) <= 253
	regex.match(sprintf(`^%s(\.%s)*$`, [dns_label, dns_label]), trim_suffix(lower(name), suffix))
	every label in split(lower(name), ".") {
		count(label) <= 63
	}
}

deny contains msg if {
	well_formed
	some rc in records
	not record_name_ok(object.get(rc.change.after, "name", null))
	msg := sprintf("[PLAN-DNS-05] record（%s）の name が、zone の apex でなく zone の名前の下で、wildcard を含まない名前ではありません", [rc.address])
}

deny contains msg if {
	well_formed
	some rc in use_case_changes
	not rc.type in {t | some t in cfg.allowed_types}
	msg := sprintf("[PLAN-DNS-06] dns-zone の下に宣言の外の型の resource（%s）があります", [rc.address])
}
