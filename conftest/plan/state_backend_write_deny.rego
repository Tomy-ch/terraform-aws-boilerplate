# state Bucket の object の書き込みを、Bucket Policy で CI の role と key と操作の組に限ること（ADR-0602 決定11、
# ADR-0604 決定9、state-backend/ADR-0104）。role の ARN（deployment-identity）と Bucket Policy・Key Policy（state-backend）は
# 別の root の plan に在るので、結合した入力で突き合わせる。役割は宣言した address から引き、role の名前で判定しない（ADR-0402 決定13）。
package plan.state_backend_write_deny

import data.plan.kms_key_policy
import data.plan.lib

di := object.get(data.policy_plan, "deployment_identity", {})

cfg := object.get(data.policy_plan, "state_backend", {})

declared(name) := object.get(cfg, name, "")

role_address(name) := object.get(object.get(di, name, {}), "role", "")

after(address) := c.rc.change.after if {
	some c in lib.changes
	c.rc.address == address
}

role_arn(name) := after(role_address(name)).arn

bucket_arn := after(declared("bucket")).arn

state_key_arn := after(declared("state_key")).arn

object_arn(key) := sprintf("%s/%s", [bucket_arn, key])

lock_key := object.get(object.get(di, "unlock_role", {}), "lock_key", "")

root_keys(root) := {object_arn(k) | some k in [root.state, sprintf("%s.tflock", [root.state])]}

object_write_actions := {"s3:deleteobject", "s3:deleteobjectversion", "s3:putobject", "s3:replicatedelete", "s3:replicateobject"}

# writer ごとに、書ける object と、その object へ拒否する操作。
writers[arn] := {"keys": root_keys(root), "denied": {"s3:deleteobjectversion", "s3:replicatedelete", "s3:replicateobject"}} if {
	some root in object.get(cfg, "roots", [])
	arn := role_arn(root.writer)
}

writers[arn] := {"keys": {object_arn(lock_key)}, "denied": {"s3:deleteobjectversion", "s3:putobject", "s3:replicatedelete", "s3:replicateobject"}} if {
	arn := role_arn(object.get(cfg, "unlocker", ""))
}

bucket_policies contains r if {
	some r in lib.readable
	r.document.address == declared("bucket_policy")
}

key_policies contains r if {
	some r in lib.readable
	r.document.address == declared("key_policy")
}

statements contains {"path": r.document.path, "statement": s} if {
	some r in bucket_policies
	some s in lib.statements(r.body)
}

actions(s) := {a | some a in lib.lower_all(object.get(s, "Action", []))}

resources(s) := {x | some x in lib.as_array(object.get(s, "Resource", []))}

not_resources(s) := {x | some x in lib.as_array(object.get(s, "NotResource", []))}

# IAM は条件の演算子と条件キーの大文字小文字を区別しない。
conditions(s) := {lower(op): {lower(k): v | some k, v in kv} | some op, kv in object.get(s, "Condition", {})}

principal_arn_values(s, op) := {v | some v in lib.as_array(conditions(s)[op]["aws:principalarn"])}

tls(s) if conditions(s) == {"bool": {"aws:securetransport": "false"}}

denies contains a if {
	some a in statements
	a.statement.Effect == "Deny"
}

others(a) if {
	conditions(a.statement) == {"arnnotequals": object.get(conditions(a.statement), "arnnotequals", {})}
	object.keys(conditions(a.statement).arnnotequals) == {"aws:principalarn"}
	a.statement.Principal == "*"
	resources(a.statement) == {sprintf("%s/*", [bucket_arn])}
	actions(a.statement) == object_write_actions
}

confining(a, arn) if {
	a.statement.Principal == "*"
	object.keys(conditions(a.statement)) == {"arnequals"}
	object.keys(conditions(a.statement).arnequals) == {"aws:principalarn"}
	principal_arn_values(a.statement, "arnequals") == {arn}
}

sse := "s3:x-amz-server-side-encryption"

sse_key := "s3:x-amz-server-side-encryption-aws-kms-key-id"

encryption(a) if {
	a.statement.Principal == "*"
	actions(a.statement) == {"s3:putobject"}
	resources(a.statement) == {sprintf("%s/*", [bucket_arn])}
}

other_than_kms(a) if {
	encryption(a)
	c := conditions(a.statement)
	object.keys(c) == {"null", "stringnotequals"}
	c.null == {sse: "false"}
	c.stringnotequals == {sse: "aws:kms"}
}

other_key(a) if {
	encryption(a)
	c := conditions(a.statement)
	object.keys(c) == {"null", "stringnotequals"}
	c.null == {sse_key: "false"}
	object.keys(c.stringnotequals) == {sse_key}
}

managed_key(a) if {
	encryption(a)
	c := conditions(a.statement)
	object.keys(c) == {"null", "stringequals"}
	c.stringequals == {sse: "aws:kms"}
	c.null == {sse_key: "true"}
}

deny contains msg if {
	some name in ["bucket", "bucket_policy", "key_policy", "state_key"]
	declared(name) == ""
	msg := sprintf("[PLAN-SBW-00] data.policy_plan.state_backend.%s が宣言にありません", [name])
}

deny contains msg if {
	declared("bucket_policy") != ""
	count(bucket_policies) == 0
	msg := sprintf("[PLAN-SBW-00] state Bucket の Bucket Policy %q が入力にありません。address が変わったなら data.policy_plan.state_backend を直してください", [declared("bucket_policy")])
}

deny contains msg if {
	declared("key_policy") != ""
	count(key_policies) == 0
	msg := sprintf("[PLAN-SBW-00] state の鍵の Key Policy %q が入力にありません。address が変わったなら data.policy_plan.state_backend を直してください", [declared("key_policy")])
}

deny contains msg if {
	not is_string(bucket_arn)
	msg := sprintf("[PLAN-SBW-00] state Bucket %q の ARN を入力から読めません。tests/policy-input.tftest.hcl の override_resource で与えてください", [declared("bucket")])
}

deny contains msg if {
	not is_string(state_key_arn)
	msg := sprintf("[PLAN-SBW-00] state の鍵 %q の ARN を入力から読めません。tests/policy-input.tftest.hcl の override_resource で与えてください", [declared("state_key")])
}

role_names contains name if {
	some root in object.get(cfg, "roots", [])
	name := root.writer
}

role_names contains object.get(cfg, "unlocker", "")

role_names contains name if {
	some name in object.get(cfg, "key_user_roles", [])
}

deny contains msg if {
	some name in role_names
	not role_known(name)
	msg := sprintf("[PLAN-SBW-00] role %q（data.policy_plan.deployment_identity.%s.role）の ARN を入力から読めません。address を直すか、tests/policy-input.tftest.hcl の override_resource で与えてください", [role_address(name), name])
}

# 関数の呼び出しを not の中に直接書くと、未定義の結果が not の外へ出て規則ごと黙る。
role_known(name) if is_string(role_arn(name))

deny contains msg if {
	count(object.get(cfg, "roots", [])) == 0
	msg := "[PLAN-SBW-00] root ごとの writer と state の key（data.policy_plan.state_backend.roots）が宣言にありません"
}

deny contains msg if {
	lock_key == ""
	msg := "[PLAN-SBW-00] lock を外す role が消せる lock file の key（data.policy_plan.deployment_identity.unlock_role.lock_key）が宣言にありません"
}

deny contains msg if {
	is_string(bucket_arn)
	count(bucket_policies) > 0
	count({a | some a in denies; others(a)}) != 1
	msg := sprintf("[PLAN-SBW-01] state Bucket の Bucket Policy に、%s/* への object の書き込みを aws:PrincipalArn の ArnNotEquals だけを条件に Principal \"*\" に拒否する文が1つではありません", [bucket_arn])
}

deny contains msg if {
	some a in denies
	others(a)
	principal_arn_values(a.statement, "arnnotequals") != object.keys(writers)
	msg := sprintf("[PLAN-SBW-01] %s: writer 以外の書き込みを拒否する文の例外 %v が、apply の role と lock を外す role の ARN %v と一致しません", [a.path, sort(principal_arn_values(a.statement, "arnnotequals")), sort(object.keys(writers))])
}

exact_role_arn(v) if regex.match(`^arn:aws[a-z-]*:iam::[0-9]{12}:role/[A-Za-z0-9+=,.@_/-]+$`, v)

deny contains msg if {
	some a in statements
	some op, kv in conditions(a.statement)
	kv["aws:principalarn"]
	not op in {"arnequals", "arnnotequals"}
	msg := sprintf("[PLAN-SBW-02] %s: aws:PrincipalArn を %s で比べています。ArnEquals か ArnNotEquals で role の ARN と完全一致で比べてください", [a.path, op])
}

deny contains msg if {
	some a in statements
	some op, kv in conditions(a.statement)
	some v in lib.as_array(kv["aws:principalarn"])
	not exact_role_arn(v)
	msg := sprintf("[PLAN-SBW-02] %s: aws:PrincipalArn の値 %v が role の ARN の完全一致ではありません（user・assumed-role・root・ワイルドカードは受けません）", [a.path, v])
}

# 書き込みの拒否を object に限る理由は state-backend/ADR-0104 決定3。
deny contains msg if {
	some a in denies
	not tls(a.statement)
	xs := resources(a.statement) | not_resources(a.statement)
	some x in xs
	not object_resource(x)
	msg := sprintf("[PLAN-SBW-03] %s: TLS 以外の Deny の Resource・NotResource に、state Bucket の object でない %v があります", [a.path, x])
}

object_resource(x) if startswith(x, sprintf("%s/", [bucket_arn]))

deny contains msg if {
	some a in denies
	not tls(a.statement)
	count(actions(a.statement) - object_write_actions) > 0
	msg := sprintf("[PLAN-SBW-03] %s: TLS 以外の Deny の Action %v が、object の書き込みの5つ（s3:PutObject・s3:DeleteObject・s3:DeleteObjectVersion・s3:ReplicateObject・s3:ReplicateDelete）の部分集合ではありません", [a.path, sort(actions(a.statement))])
}

deny contains msg if {
	some a in denies
	not tls(a.statement)
	a.statement.NotAction
	msg := sprintf("[PLAN-SBW-03] %s: TLS 以外の Deny が NotAction を使っています。Action を列挙してください", [a.path])
}

deny contains msg if {
	some arn, w in writers
	is_string(bucket_arn)
	count(bucket_policies) > 0
	count({a |
		some a in denies
		confining(a, arn)
		not_resources(a.statement) == w.keys
		count(resources(a.statement)) == 0
		actions(a.statement) == object_write_actions
	}) != 1
	msg := sprintf("[PLAN-SBW-04] %s を、自分の key %v の外への書き込みを拒否する文で閉じ込めていません", [arn, sort(w.keys)])
}

deny contains msg if {
	some arn, w in writers
	is_string(bucket_arn)
	count(bucket_policies) > 0
	count({a |
		some a in denies
		confining(a, arn)
		resources(a.statement) == w.keys
		count(not_resources(a.statement)) == 0
		actions(a.statement) == w.denied
	}) != 1
	msg := sprintf("[PLAN-SBW-04] %s の key %v への操作を、%v の拒否で閉じ込めていません", [arn, sort(w.keys), sort(w.denied)])
}

deny contains msg if {
	some a in denies
	some arn in principal_arn_values(a.statement, "arnequals")
	not writers[arn]
	msg := sprintf("[PLAN-SBW-04] %s: 宣言した writer でない %s を閉じ込める文があります", [a.path, arn])
}

deny contains msg if {
	some i, r1 in object.get(cfg, "roots", [])
	some j, r2 in object.get(cfg, "roots", [])
	i < j
	count(root_keys(r1) & root_keys(r2)) > 0
	msg := sprintf("[PLAN-SBW-05] root %q と %q の key が重なっています", [r1.writer, r2.writer])
}

deny contains msg if {
	some root in object.get(cfg, "roots", [])
	some a in denies
	some arn in principal_arn_values(a.statement, "arnequals")
	arn != role_arn(root.writer)
	object_arn(root.state) in not_resources(a.statement)
	msg := sprintf("[PLAN-SBW-05] %s: %s が root の state の key %s に書けます", [a.path, arn, object_arn(root.state)])
}

crypto(a) if a in kms_key_policy.crypto_actions

crypto(a) if {
	contains(a, "*")
	some c in kms_key_policy.crypto_actions
	glob.match(a, [], c)
}

key_users contains p if {
	some r in key_policies
	some s in lib.statements(r.body)
	s.Effect == "Allow"
	some act in lib.lower_all(s.Action)
	crypto(act)
	is_object(s.Principal)
	some p in lib.as_array(object.get(s.Principal, "AWS", []))
}

declared_key_users := {role_arn(name) | some name in object.get(cfg, "key_user_roles", [])}

deny contains msg if {
	count(key_policies) > 0
	key_users != declared_key_users
	msg := sprintf("[PLAN-SBW-06] state の鍵の暗号操作を許す principal %v が、宣言（data.policy_plan.state_backend.key_user_roles）の role %v と一致しません", [sort(key_users), sort(declared_key_users)])
}

deny contains msg if {
	some r in key_policies
	some s in lib.statements(r.body)
	s.Effect == "Deny"
	msg := sprintf("[PLAN-SBW-06] %s: %s が Deny の文を持っています。拒否は Bucket Policy に置き、Key Policy には置きません", [r.document.path, r.document.address])
}

deny contains msg if {
	is_string(bucket_arn)
	count(bucket_policies) > 0
	count({a | some a in denies; other_than_kms(a)}) != 1
	msg := "[PLAN-SBW-07] state Bucket の Bucket Policy に、方式の header が在って aws:kms でない s3:PutObject を Principal \"*\" に拒否する文が1つではありません"
}

deny contains msg if {
	is_string(bucket_arn)
	count(bucket_policies) > 0
	count({a | some a in denies; managed_key(a)}) != 1
	msg := "[PLAN-SBW-07] state Bucket の Bucket Policy に、方式の header が aws:kms で鍵 ID の header が無い s3:PutObject を Principal \"*\" に拒否する文が1つではありません"
}

deny contains msg if {
	is_string(bucket_arn)
	count(bucket_policies) > 0
	count({a | some a in denies; other_key(a)}) != 1
	msg := "[PLAN-SBW-07] state Bucket の Bucket Policy に、別の鍵 ID の s3:PutObject を Principal \"*\" に拒否する文が1つではありません"
}

deny contains msg if {
	some a in denies
	other_key(a)
	{v | some v in lib.as_array(conditions(a.statement).stringnotequals[sse_key])} != {state_key_arn}
	msg := sprintf("[PLAN-SBW-07] %s: 別の鍵 ID を拒否する文の値が、state の鍵の ARN %v ではありません", [a.path, state_key_arn])
}

deny contains msg if {
	some a in statements
	some k, v in object.get(conditions(a.statement), "null", {})
	not v in {"true", "false"}
	msg := sprintf("[PLAN-SBW-07] %s: Null の %s の値 %v が文字列の \"true\" か \"false\" ではありません", [a.path, k, v])
}

tls_deny(a) if {
	tls(a.statement)
	a.statement.Principal == "*"
	actions(a.statement) == {"s3:*"}
	resources(a.statement) == {bucket_arn, sprintf("%s/*", [bucket_arn])}
}

deny contains msg if {
	is_string(bucket_arn)
	count(bucket_policies) > 0
	count({a | some a in denies; tls_deny(a)}) != 1
	msg := sprintf("[PLAN-SBW-08] state Bucket の Bucket Policy に、%s と %s/* への s3:* を aws:SecureTransport が false の要求について Principal \"*\" に拒否する文が1つではありません", [bucket_arn, bucket_arn])
}

# Allow は IAM の側に置く。KICS は Statement を concat で組む文書を読めないので、Allow の検査をここで持つ。
deny contains msg if {
	some a in statements
	a.statement.Effect != "Deny"
	msg := sprintf("[PLAN-SBW-09] %s: state Bucket の Bucket Policy が Deny でない文（Effect %v）を持っています", [a.path, a.statement.Effect])
}
