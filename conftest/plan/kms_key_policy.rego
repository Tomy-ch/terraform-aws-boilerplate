# Key Policy が鍵の利用者を名指し、外へ開かないこと（ADR-0301 決定1・18）。Security の道具は aws_kms_key_policy の
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

account_root(arn) if regex.match(`^arn:aws[a-z-]*:iam::[0-9]{12}:root$`, arn)

roots(body) := [a | some s in allows(body); some a in aws_principals(s); account_root(a)]

# 文書のアカウント。委任の Allow がちょうど1つのときだけ決まる。決まらない文書は 02 が落とし、05 は限っていないと読み、03 は見ない。
account(body) := split(roots(body)[0], ":")[4] if count(roots(body)) == 1

same_account(arn, acct) if regex.match(sprintf(`^arn:aws[a-z-]*:(iam|sts)::%s:`, [acct]), arn)

# 限る条件として認める演算子。IfExists はキーの無い要求を通し、Not 系と Null は限らない。
limiting_operators := {"stringequals", "stringlike", "arnequals", "arnlike"}

# IAM は条件キーの大文字小文字を区別しない。
limiting_values(s, key) := [v |
	some op, kv in object.get(s, "Condition", {})
	lower(op) in limiting_operators
	some k, vs in kv
	lower(k) == key
	some v in lib.as_array(vs)
]

limiting_keys(s) := {lower(k) |
	some op, kv in object.get(s, "Condition", {})
	lower(op) in limiting_operators
	some k, _ in kv
}

no_wildcard(v) if {
	is_string(v)
	not contains(v, "*")
	not contains(v, "?")
}

# 鍵の暗号操作と grant。アカウントへの委任に置くと、IAM の許可だけで鍵を使えるようになる。
crypto_actions := {
	"kms:creategrant", "kms:decapsulate", "kms:decrypt", "kms:derivesharedsecret", "kms:encapsulate", "kms:encrypt", "kms:generatedatakey",
	"kms:generatedatakeypair", "kms:generatedatakeypairwithoutplaintext", "kms:generatedatakeywithoutplaintext",
	"kms:generatemac", "kms:getpublickey", "kms:reencryptfrom", "kms:reencryptto", "kms:sign", "kms:verify",
	"kms:verifymac",
}

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
	acct := account(r.body)
	some s in allows(r.body)
	some a in aws_principals(s)
	a != "*"
	not same_account(a, acct)
	msg := sprintf("[PLAN-KMS-03] %s: %s が、委任のアカウントの ARN でない %s に許可しています", [r.document.path, r.document.address, a])
}

deny contains msg if {
	some r in key_policies
	some s in allows(r.body)
	some a in aws_principals(s)
	not account_root(a)
	not via_service_limited(s)
	msg := sprintf("[PLAN-KMS-04] %s: %s が %s に、経由するサービス（kms:ViaService）を限らずに許可しています", [r.document.path, r.document.address, a])
}

via_service_limited(s) if {
	vs := limiting_values(s, "kms:viaservice")
	count(vs) > 0
	every v in vs {
		is_string(v)
		regex.match(`^[a-z0-9-]+\.[a-z0-9-]+\.amazonaws\.com$`, v)
	}
}

deny contains msg if {
	some r in key_policies
	some s in allows(r.body)
	some svc in service_principals(s)
	not source_limited(s, r.body)
	msg := sprintf("[PLAN-KMS-05] %s: %s が %s に、要求元（委任のアカウントの aws:SourceArn・aws:SourceAccount・kms:EncryptionContext の ARN）を限らずに許可しています", [r.document.path, r.document.address, svc])
}

# 限る条件の値は、1つでも委任のアカウントの外を指せば、ほかの条件が限っていても開く。
deny contains msg if {
	some r in key_policies
	some s in allows(r.body)
	some svc in service_principals(s)
	some v in source_values(s)
	not own_source(v, s, r.body)
	msg := sprintf("[PLAN-KMS-05] %s: %s が %s の要求元に、委任のアカウントの外の値 %v を置いています", [r.document.path, r.document.address, svc, v])
}

source_keys(s) := {k |
	some k in limiting_keys(s)
	source_key(k)
}

source_key("aws:sourceaccount")

source_key("aws:sourcearn")

source_key(k) if startswith(k, "kms:encryptioncontext:")

source_values(s) := [v | some k in source_keys(s); some v in limiting_values(s, k)]

source_limited(s, body) if {
	some v in source_values(s)
	own_source(v, s, body)
}

# アカウント ID か、委任と同じ partition とアカウントの ARN。ARN でない暗号化コンテキストの値は要求元を限らない。
# アカウントの欄が空の ARN（S3 の Bucket）は、aws:SourceAccount が同じ文で委任のアカウントに限るときだけ認める。
own_source(v, _, body) if {
	no_wildcard(v)
	v == account(body)
}

own_source(v, _, body) if {
	own_partition_arn(v, body)
	split(v, ":")[4] == account(body)
}

own_source(v, s, body) if {
	own_partition_arn(v, body)
	split(v, ":")[4] == ""
	some a in limiting_values(s, "aws:sourceaccount")
	a == account(body)
}

own_partition_arn(v, body) if {
	no_wildcard(v)
	parts := split(v, ":")
	count(parts) >= 6
	parts[0] == "arn"
	parts[1] == split(roots(body)[0], ":")[1]
}

# ForAllValues と kms:EncryptionContext の組は、暗号化コンテキストの無い要求も通す。Deny では拒む側に働くので見ない。
deny contains msg if {
	some r in key_policies
	some s in allows(r.body)
	some op, kv in object.get(s, "Condition", {})
	startswith(lower(op), "forallvalues:")
	some k, _ in kv
	startswith(lower(k), "kms:encryptioncontext")
	msg := sprintf("[PLAN-KMS-06] %s: %s が %s を ForAllValues で条件にしています", [r.document.path, r.document.address, k])
}

deny contains msg if {
	some r in key_policies
	some s in allows(r.body)
	not readable_principal(s)
	msg := sprintf("[PLAN-KMS-07] %s: %s の Allow の Principal を読めません（NotPrincipal、Principal の欠落、AWS・Service 以外の種類）", [r.document.path, r.document.address])
}

readable_principal(s) if {
	not s.NotPrincipal
	s.Principal == "*"
}

readable_principal(s) if {
	not s.NotPrincipal
	is_object(s.Principal)
	count(s.Principal) > 0
	every k, v in s.Principal {
		k in {"AWS", "Service"}
		count(lib.as_array(v)) > 0
		every x in lib.as_array(v) { is_string(x) }
	}
}

deny contains msg if {
	some r in key_policies
	some s in allows(r.body)
	some a in aws_principals(s)
	account_root(a)
	some act in delegated_too_much(s)
	msg := sprintf("[PLAN-KMS-08] %s: %s がアカウントへの委任に %s を置いています（暗号操作・grant・wildcard は名指した相手にだけ許す）", [r.document.path, r.document.address, act])
}

delegated_too_much(s) := {a |
	some a in lib.lower_all(s.Action)
	a in crypto_actions
} | {a |
	some a in lib.lower_all(s.Action)
	regex.match(`[*?]`, a)
} | {"NotAction" | s.NotAction}
