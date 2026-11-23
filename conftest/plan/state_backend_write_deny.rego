# state Bucket の object の書き込みを、Bucket Policy で CI の role と key と操作の組に限ること（ADR-0602 決定11、
# ADR-0604 決定9、state-backend/ADR-0104）。role の ARN（deployment-identity）と Bucket Policy・Key Policy（state-backend）は
# 別の root の plan に在るので、結合した入力で突き合わせる。役割は宣言した address から引き、role の名前で判定しない（ADR-0402 決定13）。
package plan.state_backend_write_deny

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

lock_key(name) := object.get(object.get(di, name, {}), "lock_key", "")

root_keys(root) := {object_arn(k) | some k in [root.state, sprintf("%s.tflock", [root.state])]}

object_write_actions := {"s3:deleteobject", "s3:deleteobjectversion", "s3:putobject", "s3:replicatedelete", "s3:replicateobject"}

writer_names := array.concat([root.writer | some root in object.get(cfg, "roots", [])], [object.get(root, "unlocker", "") | some root in object.get(cfg, "roots", [])])

# 同じ role を2つの役割に宣言すると writers の key が重なり、評価そのものが落ちる。先に SBW-00 で止める。
distinct_writers if count({n | some n in writer_names}) == count(writer_names)

writers[arn] := {"keys": root_keys(root), "denied": {"s3:deleteobjectversion", "s3:replicatedelete", "s3:replicateobject"}} if {
	distinct_writers
	some root in object.get(cfg, "roots", [])
	arn := role_arn(root.writer)
}

writers[arn] := {"keys": {object_arn(lock_key(root.unlocker))}, "denied": {"s3:deleteobjectversion", "s3:putobject", "s3:replicatedelete", "s3:replicateobject"}} if {
	distinct_writers
	some root in object.get(cfg, "roots", [])
	arn := role_arn(root.unlocker)
}

well_formed_root(root) if {
	is_string(root.writer)
	is_string(root.state)
	is_string(root.unlocker)
	root.unlocker != ""
}

deny contains msg if {
	not is_array(object.get(cfg, "roots", []))
	msg := "[PLAN-SBW-00] data.policy_plan.state_backend.roots が配列ではありません"
}

deny contains msg if {
	some root in object.get(cfg, "roots", [])
	not well_formed_root(root)
	msg := sprintf("[PLAN-SBW-00] data.policy_plan.state_backend.roots の要素 %v が writer と state と unlocker の文字列を持っていません", [root])
}

deny contains msg if {
	not is_array(object.get(cfg, "key_user_roles", []))
	msg := "[PLAN-SBW-00] data.policy_plan.state_backend.key_user_roles が配列ではありません"
}

# lock file の復号を Key Policy が許さなければ、lock を外す role は lock file を読めない。
deny contains msg if {
	some root in object.get(cfg, "roots", [])
	well_formed_root(root)
	is_array(object.get(cfg, "key_user_roles", []))
	not root.unlocker in object.get(cfg, "key_user_roles", [])
	msg := sprintf("[PLAN-SBW-00] root の lock を外す role %s が data.policy_plan.state_backend.key_user_roles にありません", [root.unlocker])
}

deny contains msg if {
	not distinct_writers
	msg := sprintf("[PLAN-SBW-00] root の writer と lock を外す role（data.policy_plan.state_backend.roots の writer・unlocker）に同じ role %v が重なっています", [writer_names])
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
	not bucket_known
	msg := sprintf("[PLAN-SBW-00] state Bucket %q の ARN を入力から読めません。tests/policy-input.tftest.hcl の override_resource で与えてください", [declared("bucket")])
}

deny contains msg if {
	not state_key_known
	msg := sprintf("[PLAN-SBW-00] state の鍵 %q の ARN を入力から読めません。tests/policy-input.tftest.hcl の override_resource で与えてください", [declared("state_key")])
}

role_names contains name if {
	some root in object.get(cfg, "roots", [])
	name := root.writer
}

role_names contains name if {
	some root in object.get(cfg, "roots", [])
	name := object.get(root, "unlocker", "")
	is_string(name)
	name != ""
}

role_names contains name if {
	some name in object.get(cfg, "key_user_roles", [])
}

deny contains msg if {
	some name in role_names
	not role_known(name)
	msg := sprintf("[PLAN-SBW-00] role %q（data.policy_plan.deployment_identity.%s.role）の ARN を入力から読めません。address を直すか、tests/policy-input.tftest.hcl の override_resource で与えてください", [role_address(name), name])
}

# 未定義になりうる値を not の中に直接書くと、未定義が not の外へ出て規則ごと黙る。
role_known(name) if is_string(role_arn(name))

bucket_known if is_string(bucket_arn)

state_key_known if is_string(state_key_arn)

deny contains msg if {
	count(object.get(cfg, "key_user_roles", [])) == 0
	msg := "[PLAN-SBW-00] state の鍵の暗号操作を許す role（data.policy_plan.state_backend.key_user_roles）が宣言にありません"
}

deny contains msg if {
	count(object.get(cfg, "roots", [])) == 0
	msg := "[PLAN-SBW-00] root ごとの writer と state の key（data.policy_plan.state_backend.roots）が宣言にありません"
}

deny contains msg if {
	some root in object.get(cfg, "roots", [])
	well_formed_root(root)
	lock_key(root.unlocker) == ""
	msg := sprintf("[PLAN-SBW-00] lock を外す role が消せる lock file の key（data.policy_plan.deployment_identity.%s.lock_key）が宣言にありません", [root.unlocker])
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
	msg := sprintf("[PLAN-SBW-04] %s を、自分の key %v の外への書き込みを Principal \"*\" と aws:PrincipalArn の ArnEquals だけの条件で拒否する文で閉じ込めていません", [arn, sort(w.keys)])
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
	msg := sprintf("[PLAN-SBW-04] %s の key %v への操作を、Principal \"*\" と aws:PrincipalArn の ArnEquals だけの条件の %v の拒否で閉じ込めていません", [arn, sort(w.keys), sort(w.denied)])
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

deny contains msg if {
	some root in object.get(cfg, "roots", [])
	well_formed_root(root)
	lock_key(root.unlocker) != ""
	lock_key(root.unlocker) != sprintf("%s.tflock", [root.state])
	msg := sprintf("[PLAN-SBW-05] lock を外す role %s が消せる key %q が、その root の lock file %q ではありません", [root.unlocker, lock_key(root.unlocker), sprintf("%s.tflock", [root.state])])
}

crypto(a) if a in lib.crypto_actions

crypto(a) if {
	contains(a, "*")
	some c in lib.crypto_actions
	glob.match(a, [], c)
}

# NotAction の Allow は列挙の外をすべて許すので、暗号操作を許すと読む。
grants_crypto(s) if {
	some act in lib.lower_all(object.get(s, "Action", []))
	crypto(act)
}

grants_crypto(s) if s.NotAction

key_grants contains {"document": r.document, "statement": s} if {
	some r in key_policies
	some s in lib.statements(r.body)
	s.Effect == "Allow"
	grants_crypto(s)
}

key_users contains p if {
	some g in key_grants
	is_object(g.statement.Principal)
	some p in lib.as_array(object.get(g.statement.Principal, "AWS", []))
}

deny contains msg if {
	some g in key_grants
	not aws_only(g.statement.Principal)
	msg := sprintf("[PLAN-SBW-06] %s: %s が AWS の role でない principal %v に暗号操作を許しています", [g.document.path, g.document.address, g.statement.Principal])
}

aws_only(p) if {
	is_object(p)
	object.keys(p) == {"AWS"}
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

# Allow は IAM の側に置く。ここで持つ理由は conftest/plan/README.md の plan.state_backend_write_deny の行にある。
deny contains msg if {
	some a in statements
	a.statement.Effect != "Deny"
	msg := sprintf("[PLAN-SBW-09] %s: state Bucket の Bucket Policy が Deny でない文（Effect %v）を持っています", [a.path, a.statement.Effect])
}

# CI の role の Key Policy の object を、その role の IAM の S3 の object と突き合わせる（state-backend/ADR-0201 決定9）。
key_object_roles := object.get(cfg, "key_object_roles", null)

key_object_names := key_object_roles if is_array(key_object_roles)

default key_object_names := []

iam_policy(name) := object.get(object.get(di, name, {}), "policy", "")

deny contains msg if {
	not is_array(key_object_roles)
	msg := "[PLAN-SBW-00] data.policy_plan.state_backend.key_object_roles が配列でないか、宣言にありません"
}

deny contains msg if {
	is_array(key_object_roles)
	count(key_object_roles) == 0
	msg := "[PLAN-SBW-00] data.policy_plan.state_backend.key_object_roles が空です"
}

# 鍵の暗号操作を許す role は、どれも S3 経由で object を限るので、すべてを突き合わせる。
deny contains msg if {
	is_array(key_object_roles)
	some name in object.get(cfg, "key_user_roles", [])
	not name in key_object_names
	msg := sprintf("[PLAN-SBW-00] data.policy_plan.state_backend.key_user_roles の %s が key_object_roles にありません", [name])
}

deny contains msg if {
	some name in key_object_names
	not name in object.get(cfg, "key_user_roles", [])
	msg := sprintf("[PLAN-SBW-00] data.policy_plan.state_backend.key_object_roles の %s が key_user_roles にありません", [name])
}

deny contains msg if {
	some name in key_object_names
	iam_policy(name) == ""
	msg := sprintf("[PLAN-SBW-00] data.policy_plan.deployment_identity.%s.policy が宣言にありません", [name])
}

deny contains msg if {
	some name in key_object_names
	iam_policy(name) != ""
	count({d | some d in lib.documents; d.type == "aws_iam_role_policy"; d.address == iam_policy(name)}) == 0
	msg := sprintf("[PLAN-SBW-00] %s の IAM の policy %q が入力にありません。address が変わったなら data.policy_plan.deployment_identity.%s.policy を直してください", [name, iam_policy(name), name])
}

deny contains msg if {
	some name in key_object_names
	some d in lib.documents
	d.type == "aws_iam_role_policy"
	d.address == iam_policy(name)
	not lib.parsed(d)
	msg := sprintf("[PLAN-SBW-10] %s: %s の IAM の policy %s が plan で決まらないか、JSON の文書として読めません", [d.path, name, d.address])
}

iam_allows(name) := {{"path": r.document.path, "statement": s} |
	some r in lib.readable
	r.document.type == "aws_iam_role_policy"
	r.document.address == iam_policy(name)
	some s in lib.statements(r.body)
	s.Effect == "Allow"
}

# Action と NotAction の wildcard は IAM の照合で読む。NotAction の Allow は列挙の外をすべて許す。
allows_action(s, action) if {
	some x in lib.lower_all(object.get(s, "Action", []))
	lib.iam_match(x, action)
}

allows_action(s, action) if {
	s.NotAction
	not excludes_action(s, action)
}

excludes_action(s, action) if {
	some x in lib.lower_all(s.NotAction)
	lib.iam_match(x, action)
}

iam_objects(name, actions) := {x |
	some a in iam_allows(name)
	some action in actions
	allows_action(a.statement, action)
	some x in lib.as_array(object.get(a.statement, "Resource", []))
}

deny contains msg if {
	some name in key_object_names
	some a in iam_allows(name)
	a.statement.NotResource
	some action in ["s3:getobject", "s3:putobject"]
	allows_action(a.statement, action)
	msg := sprintf("[PLAN-SBW-10] %s: %s の IAM の policy %s の S3 の object の Allow が NotResource を使っています。Resource を列挙してください", [a.path, name, iam_policy(name)])
}

key_role_grants(arn) := {{"document": r.document, "statement": s} |
	some r in key_policies
	some s in lib.statements(r.body)
	s.Effect == "Allow"
	is_object(s.Principal)
	arn in lib.as_array(object.get(s.Principal, "AWS", []))
}

key_objects(arn, action) := {v |
	some g in key_role_grants(arn)
	allows_action(g.statement, action)
	some v in lib.limiting_values(g.statement, "kms:encryptioncontext:aws:s3:arn")
}

deny contains msg if {
	some name in key_object_names
	role_known(name)
	some g in key_role_grants(role_arn(name))
	some action in ["kms:decrypt", "kms:generatedatakey"]
	allows_action(g.statement, action)
	count(lib.limiting_values(g.statement, "kms:encryptioncontext:aws:s3:arn")) == 0
	msg := sprintf("[PLAN-SBW-10] %s: %s が %s の role に、暗号操作を object（kms:EncryptionContext:aws:s3:arn）で限らずに許しています", [g.document.path, g.document.address, name])
}

# IAM は同じ条件キーの2つの演算子を AND で読むので、値の和で比べると、どちらも満たせない文を一致と見誤る。
deny contains msg if {
	some name in key_object_names
	role_known(name)
	some g in key_role_grants(role_arn(name))
	count([k |
		some _, kv in object.get(g.statement, "Condition", {})
		some k, _ in kv
		lower(k) == "kms:encryptioncontext:aws:s3:arn"
	]) > 1
	msg := sprintf("[PLAN-SBW-10] %s: %s の %s の role への文が kms:EncryptionContext:aws:s3:arn を2つ以上の演算子（かキー）に置いています", [g.document.path, g.document.address, name])
}

# 突き合わせる object が両側とも無いときは、一致ではなく何も見ていない。
deny contains msg if {
	some name in key_object_names
	role_known(name)
	count(key_objects(role_arn(name), "kms:decrypt")) == 0
	count(iam_objects(name, ["s3:getobject", "s3:putobject"])) == 0
	msg := sprintf("[PLAN-SBW-10] %s の role %s に、Key Policy も IAM も S3 の object を1つも許していません。宣言 key_object_roles か policy の address を確かめてください", [name, role_arn(name)])
}

# 文字列の完全一致で比べる。pattern 同士も文字列として比べ、当たる範囲の包含は読まない。
deny contains msg if {
	some name in key_object_names
	role_known(name)
	count(key_policies) > 0
	keys := key_objects(role_arn(name), "kms:decrypt")
	iam := iam_objects(name, ["s3:getobject", "s3:putobject"])
	keys != iam
	msg := sprintf("[PLAN-SBW-10] %s の role %s に Key Policy が kms:Decrypt を許す object %v が、IAM の s3:GetObject・s3:PutObject の Resource %v と一致しません", [name, role_arn(name), sort(keys), sort(iam)])
}

deny contains msg if {
	some name in key_object_names
	role_known(name)
	count(key_policies) > 0
	keys := key_objects(role_arn(name), "kms:generatedatakey")
	iam := iam_objects(name, ["s3:putobject"])
	keys != iam
	msg := sprintf("[PLAN-SBW-10] %s の role %s に Key Policy が kms:GenerateDataKey を許す object %v が、IAM の s3:PutObject の Resource %v と一致しません", [name, role_arn(name), sort(keys), sort(iam)])
}
