# Key Policy が鍵の利用者を名指し、外へ開かないこと（ADR-0301 決定1・12・18）。Security の道具は aws_kms_key_policy の
# 文書を見ないので、Policy が持つ（ADR-0402 決定4）。00 のほかの規則は文書ごとに見る。
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

# Key Policy が無いことと読めないことは、ほかの規則が何も見ずに黙る形なのでここで落とす。
declared_key_policy := data.policy_plan.state_backend.key_policy if is_string(data.policy_plan.state_backend.key_policy)

deny contains msg if {
	not declared_key_policy
	msg := "[PLAN-KMS-00] 宣言 state_backend.key_policy が無いか、文字列ではありません"
}

deny contains msg if {
	address := declared_key_policy
	count({d | some d in lib.documents; d.type == "aws_kms_key_policy"; d.address == address; d.change.actions != ["forget"]}) == 0
	msg := sprintf("[PLAN-KMS-00] 宣言した state の鍵の Key Policy %s が入力にありません", [address])
}

deny contains msg if {
	some d in lib.documents
	d.type == "aws_kms_key_policy"
	d.change.actions != ["forget"]
	not lib.parsed(d)
	msg := sprintf("[PLAN-KMS-09] %s: %s の %s が plan で決まらないか、JSON の文書として読めません", [d.path, d.address, d.attr])
}

# KMS は Principal の role を一意 ID で持つので、作った role は、それを名指す Key Policy が書き直されるまで鍵を使えない。
# 同じ名前で作り直すと ARN の文字列も同じで、Key Policy は書き直されない。別の address での作り直しも含めて、作成を見る。
created_roles contains c.rc if {
	some c in lib.changes
	c.rc.type == "aws_iam_role"
	"create" in c.rc.change.actions
}

role_suffix(rc) := sprintf(":role%s%s", [role_path(rc), rc.change.after.name]) if is_string(rc.change.after.name)

role_path(rc) := object.get(rc.change.after, "path", "/") if is_string(object.get(rc.change.after, "path", "/"))

role_path(rc) := "/" if not is_string(object.get(rc.change.after, "path", "/"))

# PutKeyPolicy は、Key Policy の作成と置換と、文書の変わる update でだけ呼ばれる。
rewritten(d) if "create" in d.change.actions

rewritten(d) if not d.change.before.policy == d.change.after.policy

deny contains msg if {
	some r in key_policies
	r.document.address == declared_key_policy
	not rewritten(r.document)
	some rc in created_roles
	suffix := role_suffix(rc)
	some s in allows(r.body)
	some a in aws_principals(s)
	endswith(a, suffix)
	msg := sprintf("[PLAN-KMS-10] %s: role %s を作るのに、それを名指す %s を書き直しません。KMS は作る前の role を名指し続けます", [r.document.path, rc.address, r.document.address])
}

deny contains msg if {
	some r in key_policies
	r.document.address == declared_key_policy
	not rewritten(r.document)
	some rc in created_roles
	not role_suffix(rc)
	msg := sprintf("[PLAN-KMS-10] %s: 作る role %s の名前が plan で決まらないので、%s が名指すかを確かめられません", [r.document.path, rc.address, r.document.address])
}

# 文書のアカウント。委任の Allow がちょうど1つのときだけ決まる。決まらない文書は 02 が落とし、05 は限っていないと読み、03 は見ない。
account(body) := split(roots(body)[0], ":")[4] if count(roots(body)) == 1

same_account(arn, acct) if regex.match(sprintf(`^arn:aws[a-z-]*:(iam|sts)::%s:`, [acct]), arn)

limiting_keys(s) := {lower(k) |
	some op, kv in object.get(s, "Condition", {})
	lower(op) in lib.limiting_operators
	some k, _ in kv
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
	vs := lib.limiting_values(s, "kms:viaservice")
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

source_values(s) := [v | some k in source_keys(s); some v in lib.limiting_values(s, k)]

source_limited(s, body) if {
	some v in source_values(s)
	own_source(v, s, body)
}

own_source(v, _, body) if {
	lib.no_wildcard(v)
	v == account(body)
}

own_source(v, _, body) if {
	own_partition_arn(v, body)
	split(v, ":")[4] == account(body)
}

own_source(v, s, body) if {
	own_partition_arn(v, body)
	split(v, ":")[4] == ""
	some a in lib.limiting_values(s, "aws:sourceaccount")
	a == account(body)
}

own_partition_arn(v, body) if {
	lib.no_wildcard(v)
	parts := split(v, ":")
	count(parts) >= 6
	parts[0] == "arn"
	parts[1] == split(roots(body)[0], ":")[1]
}

# kms:ViaService は SNS 経由であることしか限らないので、SNS 経由の role は、SNS が渡す暗号化コンテキストで Topic を名指す。
deny contains msg if {
	some r in key_policies
	some s in allows(r.body)
	some a in aws_principals(s)
	not account_root(a)
	via_sns(s)
	not topic_limited(s, r.body)
	msg := sprintf("[PLAN-KMS-11] %s: %s が SNS 経由の %s に、委任のアカウントの Topic の ARN（kms:EncryptionContext:aws:sns:topicArn）を限らずに許可しています", [r.document.path, r.document.address, a])
}

via_sns(s) if {
	some v in lib.limiting_values(s, "kms:viaservice")
	startswith(v, "sns.")
}

topic_limited(s, body) if {
	vs := lib.limiting_values(s, "kms:encryptioncontext:aws:sns:topicarn")
	count(vs) > 0
	every v in vs { own_topic_arn(v, body) }
}

own_topic_arn(v, body) if {
	own_partition_arn(v, body)
	parts := split(v, ":")
	count(parts) == 6
	parts[2] == "sns"
	parts[4] == account(body)
	parts[5] != ""
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
	a in lib.crypto_actions
} | {a |
	some a in lib.lower_all(s.Action)
	regex.match(`[*?]`, a)
} | {"NotAction" | s.NotAction}
