# 証跡を保管する Bucket の不変条件（audit-evidence/ADR-0101・0102）。保持の設定と Bucket Policy の形は
# 本リポジトリの設計で、Security の道具の既定のルールは持たない（ADR-0402 決定2・3）。
package plan.audit_evidence

import data.plan.lib

decl := object.get(data.policy_plan, "audit_evidence", {})

declared_keys := ["bucket", "bucket_policy", "object_lock_configuration", "lifecycle_configuration", "key_policy", "metric_alarm"]

address(key) := decl[key] if is_string(object.get(decl, key, null))

deny contains msg if {
	some key in declared_keys
	not address(key)
	msg := sprintf("[PLAN-AUDIT-00] 宣言 audit_evidence.%s が無いか、文字列ではありません", [key])
}

after(key) := c.rc.change.after if {
	some c in lib.changes
	c.rc.address == address(key)
	is_object(c.rc.change.after)
}

deny contains msg if {
	some key in declared_keys
	address(key)
	not after(key)
	msg := sprintf("[PLAN-AUDIT-00] 宣言した %s が入力にありません", [address(key)])
}

# 既定の保持が無いと、保持を指定しない書き手（CloudTrail・Config）の object は保護されない。
retention := r if {
	rule := object.get(after("object_lock_configuration"), "rule", [])
	count(rule) == 1
	dr := object.get(rule[0], "default_retention", [])
	count(dr) == 1
	r := dr[0]
	r.mode in {"COMPLIANCE", "GOVERNANCE"}
	is_number(r.days)
	r.days >= 1
}

deny contains msg if {
	after("object_lock_configuration")
	not retention
	msg := sprintf("[PLAN-AUDIT-01] %s が、mode（COMPLIANCE か GOVERNANCE）と日数を持つ既定の保持を1つ持ちません", [address("object_lock_configuration")])
}

expiration_days contains e.days if {
	some rule in object.get(after("lifecycle_configuration"), "rule", [])
	rule.status == "Enabled"
	some e in object.get(rule, "expiration", [])
	is_number(object.get(e, "days", null))
	e.days > 0
}

# 一部の prefix や tag にだけ掛かる失効は、Bucket の残りを保持の後も残す。
whole_bucket(rule) if {
	filters := object.get(rule, "filter", [])
	count(filters) == 1
	every _, v in filters[0] { empty_value(v) }
}

empty_value(v) if v == null

empty_value(v) if v == []

empty_value(v) if v == ""

deny contains msg if {
	retention
	some rule in object.get(after("lifecycle_configuration"), "rule", [])
	rule.status == "Enabled"
	some e in object.get(rule, "expiration", [])
	is_number(object.get(e, "days", null))
	e.days > 0
	not whole_bucket(rule)
	msg := sprintf("[PLAN-AUDIT-01] %s の規則 %v の失効が、Bucket 全体に掛かりません（filter が空でない）", [address("lifecycle_configuration"), object.get(rule, "id", "")])
}

# 失効が保持より早いと、保持の終わる前に現行版が delete marker で隠れる。遅いと保管の費用が際限なく残る。
deny contains msg if {
	retention
	expiration_days != {retention.days}
	msg := sprintf("[PLAN-AUDIT-01] %s の現行版の失効の日数 %v が、既定の保持の日数 %d と一致しません", [address("lifecycle_configuration"), expiration_days, retention.days])
}

deny contains msg if {
	after("bucket")
	object.get(after("bucket"), "object_lock_enabled", false) != true
	msg := sprintf("[PLAN-AUDIT-04] %s が Object Lock を有効にしていません", [address("bucket")])
}

bucket_policy := json.unmarshal(after("bucket_policy").policy) if {
	is_string(object.get(after("bucket_policy"), "policy", null))
	json.is_valid(after("bucket_policy").policy)
}

key_policy := json.unmarshal(after("key_policy").policy) if {
	is_string(object.get(after("key_policy"), "policy", null))
	json.is_valid(after("key_policy").policy)
}

deny contains msg if {
	after("bucket_policy")
	not bucket_policy
	msg := sprintf("[PLAN-AUDIT-03] %s の policy が plan で決まらないか、JSON として読めません", [address("bucket_policy")])
}

deny contains msg if {
	after("key_policy")
	not key_policy
	msg := sprintf("[PLAN-AUDIT-05] %s の policy が plan で決まらないか、JSON として読めません", [address("key_policy")])
}

statements := lib.statements(bucket_policy)

source_values(s) := [v |
	some k in ["aws:sourcearn", "aws:sourceaccount"]
	some v in lib.limiting_values(s, k)
]

service_only(s) if {
	not s.NotPrincipal
	is_object(s.Principal)
	object.keys(s.Principal) == {"Service"}
}

source_limited(s) if {
	count(source_values(s)) > 0
	every v in source_values(s) { lib.no_wildcard(v) }
}

# 書き手はサービスだけで、各 Allow は要求元を wildcard の無い値で限る（confused deputy。ADR-0301 決定18）。
deny contains msg if {
	some s in statements
	s.Effect == "Allow"
	not service_only(s)
	msg := sprintf("[PLAN-AUDIT-02] %s の Allow %v が、サービス principal だけを名指していません", [address("bucket_policy"), object.get(s, "Sid", "")])
}

deny contains msg if {
	some s in statements
	s.Effect == "Allow"
	not source_limited(s)
	msg := sprintf("[PLAN-AUDIT-02] %s の Allow %v が、aws:SourceArn か aws:SourceAccount で要求元を wildcard の無い値に限っていません", [address("bucket_policy"), object.get(s, "Sid", "")])
}

# Deny は principal の例外を持たない。8文の形は Bucket の ARN と既定の保持から決まる（audit-evidence/ADR-0102 決定1）。
bucket_arn := after("bucket").arn if is_string(object.get(after("bucket"), "arn", null))

objects := sprintf("%s/*", [bucket_arn])

expected_denies := {
	"DenyInsecureTransport": {"actions": {"s3:*"}, "resources": {bucket_arn, objects}, "condition": {"Bool": {"aws:SecureTransport": "false"}}},
	"DenyObjectWriteByNonService": {"actions": {"s3:PutObject"}, "resources": {objects}, "condition": {"Bool": {"aws:PrincipalIsAWSService": "false"}}},
	"DenyObjectDeletion": {"actions": {"s3:DeleteObject", "s3:DeleteObjectVersion"}, "resources": {objects}, "condition": {}},
	"DenyRetentionBeyondDefault": {"actions": {"s3:PutObject", "s3:PutObjectRetention"}, "resources": {objects}, "condition": {"NumericGreaterThan": {"s3:object-lock-remaining-retention-days": sprintf("%d", [retention.days])}}},
	"DenyOtherLockMode": {"actions": {"s3:PutObject", "s3:PutObjectRetention"}, "resources": {objects}, "condition": {"Null": {"s3:object-lock-mode": "false"}, "StringNotEquals": {"s3:object-lock-mode": retention.mode}}},
	"DenyLegalHold": {"actions": {"s3:PutObjectLegalHold"}, "resources": {objects}, "condition": {}},
	"DenyLegalHoldOnWrite": {"actions": {"s3:PutObject"}, "resources": {objects}, "condition": {"Null": {"s3:object-lock-legal-hold": "false"}}},
	"DenyEventHold": {"actions": {"s3:PutObject", "s3:PutObjectRetention"}, "resources": {objects}, "condition": {"Null": {"s3:object-lock-event-hold": "false"}}},
}

required_denies := {"DenyInsecureTransport", "DenyObjectWriteByNonService", "DenyObjectDeletion", "DenyRetentionBeyondDefault", "DenyOtherLockMode", "DenyLegalHold", "DenyLegalHoldOnWrite", "DenyEventHold"}

as_set(x) := {v | some v in lib.as_array(x)}

matches_expected(sid) if {
	e := expected_denies[sid]
	some s in statements
	s.Sid == sid
	s.Effect == "Deny"
	s.Principal == "*"
	not s.NotPrincipal
	not s.NotAction
	not s.NotResource
	as_set(s.Action) == e.actions
	as_set(s.Resource) == e.resources
	object.get(s, "Condition", {}) == e.condition
}

deny contains msg if {
	bucket_policy
	not bucket_arn
	msg := sprintf("[PLAN-AUDIT-03] %s の ARN が plan で決まらないので、Deny の Resource を確かめられません", [address("bucket")])
}

deny contains msg if {
	bucket_policy
	bucket_arn
	retention
	some sid in required_denies
	not matches_expected(sid)
	msg := sprintf("[PLAN-AUDIT-03] %s の Deny %s が、Principal \"*\" と期待の Action・Resource・Condition を持ちません", [address("bucket_policy"), sid])
}

deny contains msg if {
	bucket_policy
	some s in statements
	s.Effect == "Deny"
	some _, kv in object.get(s, "Condition", {})
	some k, _ in kv
	lower(k) in {"aws:principalarn", "aws:userid", "aws:principalaccount", "aws:principalorgid"}
	msg := sprintf("[PLAN-AUDIT-03] %s の Deny %v が、主体による例外（%s）を持ちます", [address("bucket_policy"), object.get(s, "Sid", ""), k])
}

# 書き込みを許す要求元と、暗号化を許す要求元が食い違うと、配信が落ちるか、別の書き手に鍵を開く。文は principal のサービスで拾う。
service_sources(body, service, key) := {v |
	some s in lib.statements(body)
	s.Effect == "Allow"
	is_object(s.Principal)
	service in lib.as_array(object.get(s.Principal, "Service", []))
	some v in lib.limiting_values(s, key)
}

bucket_trails := service_sources(bucket_policy, "cloudtrail.amazonaws.com", "aws:sourcearn")

key_trails := service_sources(key_policy, "cloudtrail.amazonaws.com", "aws:sourcearn")

deny contains msg if {
	bucket_policy
	key_policy
	count(bucket_trails) == 0
	msg := sprintf("[PLAN-AUDIT-05] %s が、組織の trail に書き込みを許していません", [address("bucket_policy")])
}

deny contains msg if {
	bucket_policy
	key_policy
	bucket_trails != key_trails
	msg := sprintf("[PLAN-AUDIT-05] %s が書き込みを許す trail %v と、%s が暗号化を許す trail %v が一致しません", [address("bucket_policy"), bucket_trails, address("key_policy"), key_trails])
}

deny contains msg if {
	bucket_policy
	key_policy
	bucket_config := service_sources(bucket_policy, "config.amazonaws.com", "aws:sourceaccount")
	key_config := service_sources(key_policy, "config.amazonaws.com", "aws:sourceaccount")
	bucket_config != key_config
	msg := sprintf("[PLAN-AUDIT-05] %s が書き込みを許す Config のアカウント %v と、%s が暗号化を許すアカウント %v が一致しません", [address("bucket_policy"), bucket_config, address("key_policy"), key_config])
}
