# Key Policy が鍵の利用者を名指し、外へ開かないこと（ADR-0301 決定1・17）。Security の道具は aws_kms_key_policy の
# 文書を見ないので、Policy が持つ（ADR-0402 決定4）。規則はすべて文書ごとで、Key Policy が0件なら何も言わない。
package plan.kms_key_policy

import data.plan.lib

key_policies contains r if {
	some r in lib.readable
	r.document.type == "aws_kms_key_policy"
}

allows(body) := [s | some s in lib.statements(body); s.Effect == "Allow"]

aws_principals(s) := lib.as_array(object.get(s.Principal, "AWS", [])) if is_object(s.Principal)

aws_principals(s) := [] if not is_object(s.Principal)

service_principals(s) := lib.as_array(object.get(s.Principal, "Service", [])) if is_object(s.Principal)

service_principals(s) := [] if not is_object(s.Principal)

account_root(arn) if regex.match(`^arn:[a-z-]+:iam::[0-9]{12}:root$`, arn)

account_of(arn) := split(arn, ":")[4]

# IAM は条件キーの大文字小文字を区別しない。
condition_keys(s) := {lower(k) | some _, kv in object.get(s, "Condition", {}); some k, _ in kv}

roots(body) := [a | some s in allows(body); some a in aws_principals(s); account_root(a)]

deny contains msg if {
	some r in key_policies
	some s in allows(r.body)
	wildcard_principal(s)
	msg := sprintf("[PLAN-KMS-01] %s: %s が Principal \"*\" に許可しています", [r.document.path, r.document.address])
}

wildcard_principal(s) if s.Principal == "*"

wildcard_principal(s) if "*" in aws_principals(s)

deny contains msg if {
	some r in key_policies
	count(roots(r.body)) != 1
	msg := sprintf("[PLAN-KMS-02] %s: %s のアカウントへの委任の Allow が %d 件です（1件を期待）", [r.document.path, r.document.address, count(roots(r.body))])
}

deny contains msg if {
	some r in key_policies
	count(roots(r.body)) == 1
	account := account_of(roots(r.body)[0])
	some s in allows(r.body)
	some a in aws_principals(s)
	a != "*"
	account_of(a) != account
	msg := sprintf("[PLAN-KMS-03] %s: %s が別のアカウントの %s に許可しています", [r.document.path, r.document.address, a])
}

deny contains msg if {
	some r in key_policies
	some s in allows(r.body)
	some a in aws_principals(s)
	not account_root(a)
	not "kms:viaservice" in condition_keys(s)
	msg := sprintf("[PLAN-KMS-04] %s: %s が %s に、経由するサービス（kms:ViaService）を限らずに許可しています", [r.document.path, r.document.address, a])
}

deny contains msg if {
	some r in key_policies
	some s in allows(r.body)
	some svc in service_principals(s)
	not source_limited(s)
	msg := sprintf("[PLAN-KMS-05] %s: %s が %s に、要求元（aws:SourceArn・aws:SourceAccount・kms:EncryptionContext）を限らずに許可しています", [r.document.path, r.document.address, svc])
}

source_limited(s) if "aws:sourcearn" in condition_keys(s)

source_limited(s) if "aws:sourceaccount" in condition_keys(s)

source_limited(s) if {
	some k in condition_keys(s)
	startswith(k, "kms:encryptioncontext:")
}

# ForAllValues と kms:EncryptionContext の組は、暗号化コンテキストの無い要求も通す。
deny contains msg if {
	some r in key_policies
	some s in lib.statements(r.body)
	some op, kv in object.get(s, "Condition", {})
	startswith(lower(op), "forallvalues:")
	some k, _ in kv
	startswith(lower(k), "kms:encryptioncontext")
	msg := sprintf("[PLAN-KMS-06] %s: %s が %s を ForAllValues で条件にしています", [r.document.path, r.document.address, k])
}
