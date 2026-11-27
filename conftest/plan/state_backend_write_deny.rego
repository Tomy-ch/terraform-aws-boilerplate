# state Bucket の object の書き込みを、Bucket Policy で CI の role と key と操作の組に限ること（ADR-0602 決定11、
# ADR-0604 決定9、state-backend/ADR-0104）。role の ARN（deployment-identity）と Bucket Policy・Key Policy（state-backend）は
# 別の root の plan に在るので、結合した入力で突き合わせる。役割は宣言した address から引き、role の名前で判定しない（ADR-0402 決定13）。
# 宣言の形は1回、入力の突き合わせは instance ごとに見る（lib/instances.rego）。
package plan.state_backend_write_deny

import data.plan.lib

di := object.get(data.policy_plan, "deployment_identity", {})

cfg := object.get(data.policy_plan, "state_backend", {})

declared_raw(name) := object.get(cfg, name, "")

# 宣言の address を instance の prefix へ付け替える。付け替えられない address はそのまま引き、00 で落とす。
at(raw, i) := lib.rebase(raw, i) if lib.rebasable(raw)

at(raw, _) := raw if not lib.rebasable(raw)

declared(i, name) := at(declared_raw(name), i)

role_address_raw(name) := object.get(object.get(di, name, {}), "role", "")

role_address(i, name) := at(role_address_raw(name), i)

after(i, address) := c.rc.change.after if {
	some c in lib.changes
	c.rc.address == address
	lib.in_root(c.path, i)
}

# 関数は評価の結果を持ち越さないので、instance ごとの値は instance の key で引く規則に置く。
ikey(i) := sprintf("%v/%s", [i.root, i.name])

creating_keys contains ikey(i) if {
	some i in lib.instances
	lib.creating(i)
}

creating(i) if ikey(i) in creating_keys

referenced_roles := role_names | {name | some name in key_object_names}

role_arns[[ikey(i), name]] := after(i, role_address(i, name)).arn if {
	some i in lib.instances
	some name in referenced_roles
}

role_arn(i, name) := role_arns[[ikey(i), name]]

bucket_arns[ikey(i)] := after(i, declared(i, "bucket")).arn if some i in lib.instances

# 作成の plan では Bucket の ARN は決まらないが、名前は決まる。ARN を名前と委任の partition から組む。
bucket_arns[ikey(i)] := sprintf("arn:%s:s3:::%s", [instance_account(i)[0], after(i, declared(i, "bucket")).bucket]) if {
	some i in lib.instances
	creating(i)
	not planned_bucket_arn(i)
	is_string(after(i, declared(i, "bucket")).bucket)
}

planned_bucket_arn(i) if is_string(after(i, declared(i, "bucket")).arn)

bucket_arn(i) := bucket_arns[ikey(i)]

state_key_arns[ikey(i)] := after(i, declared(i, "state_key")).arn if some i in lib.instances

state_key_arn(i) := state_key_arns[ikey(i)]

object_arn(i, key) := sprintf("%s/%s", [bucket_arn(i), key])

lock_key(name) := object.get(object.get(di, name, {}), "lock_key", "")

state_keys_of(root) := {root.state, sprintf("%s.tflock", [root.state])}

root_keys(i, root) := {object_arn(i, k) | some k in state_keys_of(root)}

object_write_actions := {"s3:deleteobject", "s3:deleteobjectversion", "s3:putobject", "s3:replicatedelete", "s3:replicateobject"}

writer_names := array.concat([root.writer | some root in object.get(cfg, "roots", [])], [object.get(root, "unlocker", "") | some root in object.get(cfg, "roots", [])])

# 同じ role を2つの役割に宣言すると writers の key が重なり、評価そのものが落ちる。先に SBW-00 で止める。
distinct_writers if count({n | some n in writer_names}) == count(writer_names)

writer_entries(i) := array.concat(
	[{"arn": arn, "keys": root_keys(i, root), "denied": {"s3:deleteobjectversion", "s3:replicatedelete", "s3:replicateobject"}} |
		distinct_writers
		some root in object.get(cfg, "roots", [])
		arn := writer_arn(i, root.writer)
	],
	[{"arn": arn, "keys": {object_arn(i, lock_key(root.unlocker))}, "denied": {"s3:deleteobjectversion", "s3:putobject", "s3:replicatedelete", "s3:replicateobject"}} |
		distinct_writers
		some root in object.get(cfg, "roots", [])
		arn := writer_arn(i, root.unlocker)
	],
)

writers(i) := {e.arn: {"keys": e.keys, "denied": e.denied} | some e in writer_entries(i)}

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

deny contains msg if {
	some e in lib.instance_shape_errors
	msg := sprintf("[PLAN-SBW-00] %s", [e])
}

# 作成の plan（creating）でも、読める Bucket Policy は読む。Bucket・鍵・role の ARN を要る判定だけを、それが決まるときに見る。
bucket_policies(i) := bucket_policies_of[ikey(i)]

bucket_policies_of[ikey(i)] := {r |
	some r in lib.readable
	r.document.address == declared(i, "bucket_policy")
	lib.in_root(r.document.path, i)
} if some i in lib.instances

key_policies(i) := key_policies_of[ikey(i)]

key_policies_of[ikey(i)] := {r |
	some r in lib.readable
	r.document.address == declared(i, "key_policy")
	lib.in_root(r.document.path, i)
} if some i in lib.instances

statements(i) := {{"path": r.document.path, "statement": s} |
	some r in bucket_policies(i)
	some s in lib.statements(r.body)
}

actions(s) := {a | some a in lib.lower_all(object.get(s, "Action", []))}

resources(s) := {x | some x in lib.as_array(object.get(s, "Resource", []))}

not_resources(s) := {x | some x in lib.as_array(object.get(s, "NotResource", []))}

# IAM は条件の演算子と条件キーの大文字小文字を区別しない。
conditions(s) := {lower(op): {lower(k): v | some k, v in kv} | some op, kv in object.get(s, "Condition", {})}

principal_arn_values(s, op) := {v | some v in lib.as_array(conditions(s)[op]["aws:principalarn"])}

tls(s) if conditions(s) == {"bool": {"aws:securetransport": "false"}}

denies(i) := {a |
	some a in statements(i)
	a.statement.Effect == "Deny"
}

others(i, a) if {
	conditions(a.statement) == {"arnnotequals": object.get(conditions(a.statement), "arnnotequals", {})}
	object.keys(conditions(a.statement).arnnotequals) == {"aws:principalarn"}
	a.statement.Principal == "*"
	resources(a.statement) == {sprintf("%s/*", [bucket_arn(i)])}
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

encryption(i, a) if {
	a.statement.Principal == "*"
	actions(a.statement) == {"s3:putobject"}
	resources(a.statement) == {sprintf("%s/*", [bucket_arn(i)])}
}

other_than_kms(i, a) if {
	encryption(i, a)
	c := conditions(a.statement)
	object.keys(c) == {"null", "stringnotequals"}
	c.null == {sse: "false"}
	c.stringnotequals == {sse: "aws:kms"}
}

other_key(i, a) if {
	encryption(i, a)
	c := conditions(a.statement)
	object.keys(c) == {"null", "stringnotequals"}
	c.null == {sse_key: "false"}
	object.keys(c.stringnotequals) == {sse_key}
}

managed_key(i, a) if {
	encryption(i, a)
	c := conditions(a.statement)
	object.keys(c) == {"null", "stringequals"}
	c.stringequals == {sse: "aws:kms"}
	c.null == {sse_key: "true"}
}

deny contains msg if {
	some name in ["bucket", "bucket_policy", "key_policy", "state_key"]
	declared_raw(name) == ""
	msg := sprintf("[PLAN-SBW-00] data.policy_plan.state_backend.%s が宣言にありません", [name])
}

deny contains msg if {
	some name in ["bucket", "bucket_policy", "key_policy", "state_key"]
	declared_raw(name) != ""
	not lib.rebasable(declared_raw(name))
	msg := sprintf("[PLAN-SBW-00] data.policy_plan.state_backend.%s %q が state_backend.module か deployment_identity.module で始まりません", [name, declared_raw(name)])
}

deny contains msg if {
	some name in role_names
	role_address_raw(name) != ""
	not lib.rebasable(role_address_raw(name))
	msg := sprintf("[PLAN-SBW-00] data.policy_plan.deployment_identity.%s.role %q が state_backend.module か deployment_identity.module で始まりません", [name, role_address_raw(name)])
}

# 読めない値の案内。正規の instance は reference の mock の plan で、ほかの instance は作成の扱いで読む。
unreadable_hint(i) := "tests/policy-input.tftest.hcl の override_resource で与えてください" if i.root == null

unreadable_hint(i) := sprintf("その instance の state の鍵を作る plan なら、data.policy_plan.state_backend.instances[%q] の %s に creating を宣言してください", [i.root, i.name]) if is_string(i.root)

deny contains msg if {
	some i in lib.instances
	not creating(i)
	declared_raw("bucket_policy") != ""
	count(bucket_policies(i)) == 0
	msg := sprintf("[PLAN-SBW-00] %sstate Bucket の Bucket Policy %q が入力にありません。address が変わったなら data.policy_plan.state_backend を直してください", [lib.instance_tag(i), declared(i, "bucket_policy")])
}

deny contains msg if {
	some i in lib.instances
	creating(i)
	declared_raw("bucket_policy") != ""
	not lib.present(declared(i, "bucket_policy"), i)
	msg := sprintf("[PLAN-SBW-00] %sstate Bucket の Bucket Policy %q が入力にありません。address が変わったなら data.policy_plan.state_backend を直してください", [lib.instance_tag(i), declared(i, "bucket_policy")])
}

deny contains msg if {
	some i in lib.instances
	declared_raw("key_policy") != ""
	count(key_policies(i)) == 0
	msg := sprintf("[PLAN-SBW-00] %sstate の鍵の Key Policy %q が入力にありません。address が変わったなら data.policy_plan.state_backend を直してください", [lib.instance_tag(i), declared(i, "key_policy")])
}

deny contains msg if {
	some i in lib.instances
	not creating(i)
	not bucket_known(i)
	msg := sprintf("[PLAN-SBW-00] %sstate Bucket %q の ARN を入力から読めません。%s", [lib.instance_tag(i), declared(i, "bucket"), unreadable_hint(i)])
}

deny contains msg if {
	some i in lib.instances
	not creating(i)
	not state_key_known(i)
	msg := sprintf("[PLAN-SBW-00] %sstate の鍵 %q の ARN を入力から読めません。%s", [lib.instance_tag(i), declared(i, "state_key"), unreadable_hint(i)])
}

# 宣言に無い instance の state Bucket と role は、どの規則も読まない。
kind_name := {"state_backend": "state-backend", "deployment_identity": "deployment-identity"}

deny contains msg if {
	some e in lib.malformed_marker_entries
	msg := sprintf("[PLAN-SBW-00] %s: %s は宣言に無い instance を見つける印の address なのに、change を object で持っていません", [e.path, e.address])
}

deny contains msg if {
	some u in lib.undeclared_instances
	msg := sprintf("[PLAN-SBW-00] %s: %s の instance（prefix %q）が宣言にありません。data.policy_plan.state_backend.instances[%q] に足してください", [u.path, kind_name[u.kind], u.prefix, lib.root_name(u.path)])
}

deny contains msg if {
	some i in lib.instances
	lib.creating_unused(i)
	msg := sprintf("[PLAN-SBW-11] %screating を宣言したのに、state の鍵 %q を作る plan ではありません。作成を apply した後なら、data.policy_plan.state_backend.instances[%q] の %s から creating を外してください", [lib.instance_tag(i), declared(i, "state_key"), i.root, i.name])
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
	some i in lib.instances
	not creating(i)
	some name in role_names
	not role_known(i, name)
	msg := sprintf("[PLAN-SBW-00] %srole %q（data.policy_plan.deployment_identity.%s.role）の ARN を入力から読めません。address を直すか、%s", [lib.instance_tag(i), role_address(i, name), name, unreadable_hint(i)])
}

deny contains msg if {
	some i in lib.instances
	creating(i)
	some name in role_names
	not role_name_known(i, name)
	msg := sprintf("[PLAN-SBW-00] %srole %q（data.policy_plan.deployment_identity.%s.role）の名前を入力から読めません。address を直してください", [lib.instance_tag(i), role_address(i, name), name])
}

deny contains msg if {
	some i in lib.instances
	creating(i)
	count(key_policies(i)) > 0
	not key_policy_account(i)
	msg := sprintf("[PLAN-SBW-00] %s作成の plan の role の ARN を組むアカウントを、state の鍵の Key Policy %q のアカウントへの委任（arn:<partition>:iam::<account>:root）から1つに決められません", [lib.instance_tag(i), declared(i, "key_policy")])
}

deny contains msg if {
	some i in lib.instances
	creating(i)
	not bucket_name_account(i)
	msg := sprintf("[PLAN-SBW-00] %s作成の plan の state Bucket %q の名前から、account regional namespace のアカウント（<名前>-<account>-<region>-an）を読めません", [lib.instance_tag(i), declared(i, "bucket")])
}

deny contains msg if {
	some i in lib.instances
	creating(i)
	key_policy_account(i)[1] != bucket_name_account(i)
	msg := sprintf("[PLAN-SBW-00] %s作成の plan の state の鍵の Key Policy %q の委任のアカウント %s が、state Bucket %q の名前のアカウント %s と違います", [lib.instance_tag(i), declared(i, "key_policy"), key_policy_account(i)[1], declared(i, "bucket"), bucket_name_account(i)])
}

# 未定義になりうる値を not の中に直接書くと、未定義が not の外へ出て規則ごと黙る。
role_known(i, name) if is_string(role_arn(i, name))

bucket_known(i) if is_string(bucket_arn(i))

# Bucket Policy の書き手の ARN。作成の plan で決まらない ARN は、role の名前と委任のアカウントから組む。組めない書き手は
# writers に入らず、01 の例外の集合が一致しないことと、00（名前かアカウントが決まらない）で落ちる。
writer_arn(i, name) := role_arn(i, name) if role_known(i, name)

writer_arn(i, name) := built_role_arn(i, name) if {
	not role_known(i, name)
	creating(i)
}

state_key_known(i) if is_string(state_key_arn(i))

# 作成の plan では role の ARN が決まらない。role の `name`・`path` と instance_account から ARN を組む。
role_name_known(i, name) if is_string(after(i, role_address(i, name)).name)

role_path(a) := object.get(a, "path", "/") if is_string(object.get(a, "path", "/"))

role_path(a) := "/" if not is_string(object.get(a, "path", "/"))

key_policy_roots(i) := key_policy_roots_of[ikey(i)]

key_policy_roots_of[ikey(i)] := {[m[1], m[2]] |
	some r in key_policies(i)
	some s in lib.statements(r.body)
	s.Effect == "Allow"
	is_object(s.Principal)
	some p in lib.as_array(object.get(s.Principal, "AWS", []))
	is_string(p)
	some m in regex.find_all_string_submatch_n(`^arn:(aws[a-z-]*):iam::([0-9]{12}):root$`, p, 1)
} if some i in lib.instances

# 委任のアカウントがちょうど1つで、state Bucket の名前のアカウントと同じときだけ決まる。Bucket の名前は account regional
# namespace の形（<名前>-<account>-<region>-an）で、そのアカウントにしか作れない（PLAN-KMS-12 と同じ読み方）ので、Key Policy の
# 文言から独立に、作る先のアカウントを示す。決まらなければ、作成の plan の role は突き合わせず 00 で落とす。
key_policy_account(i) := x if {
	count(key_policy_roots(i)) == 1
	some x in key_policy_roots(i)
}

bucket_name_account(i) := m[0][1] if {
	name := after(i, declared(i, "bucket")).bucket
	is_string(name)
	m := regex.find_all_string_submatch_n(`^[a-z0-9][a-z0-9.-]*-([0-9]{12})-[a-z0-9-]+-an$`, name, 1)
	count(m) == 1
}

instance_account(i) := key_policy_account(i) if key_policy_account(i)[1] == bucket_name_account(i)

built_role_arn(i, name) := sprintf("arn:%s:iam::%s:role%s%s", [instance_account(i)[0], instance_account(i)[1], role_path(a), a.name]) if {
	a := after(i, role_address(i, name))
	is_string(a.name)
}

# Key Policy の principal と比べる宣言の role の ARN。作成の plan では組んだ ARN で、アカウントも比べる。
role_key(i, name) := role_arn(i, name) if not creating(i)

role_key(i, name) := built_role_arn(i, name) if creating(i)

role_identified(i, name) if role_key(i, name)

role_label(i, name) := role_arn(i, name) if role_known(i, name)

role_label(i, name) := role_address(i, name) if not role_known(i, name)

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
	some i in lib.instances
	is_string(bucket_arn(i))
	count(bucket_policies(i)) > 0
	count({a | some a in denies(i); others(i, a)}) != 1
	msg := sprintf("[PLAN-SBW-01] %sstate Bucket の Bucket Policy に、%s/* への object の書き込みを aws:PrincipalArn の ArnNotEquals だけを条件に Principal \"*\" に拒否する文が1つではありません", [lib.instance_tag(i), bucket_arn(i)])
}

deny contains msg if {
	some i in lib.instances
	some a in denies(i)
	others(i, a)
	principal_arn_values(a.statement, "arnnotequals") != object.keys(writers(i))
	msg := sprintf("[PLAN-SBW-01] %s%s: writer 以外の書き込みを拒否する文の例外 %v が、apply の role と lock を外す role の ARN %v と一致しません", [lib.instance_tag(i), a.path, sort(principal_arn_values(a.statement, "arnnotequals")), sort(object.keys(writers(i)))])
}

exact_role_arn(v) if regex.match(`^arn:aws[a-z-]*:iam::[0-9]{12}:role/[A-Za-z0-9+=,.@_/-]+$`, v)

deny contains msg if {
	some i in lib.instances
	some a in statements(i)
	some op, kv in conditions(a.statement)
	kv["aws:principalarn"]
	not op in {"arnequals", "arnnotequals"}
	msg := sprintf("[PLAN-SBW-02] %s%s: aws:PrincipalArn を %s で比べています。ArnEquals か ArnNotEquals で role の ARN と完全一致で比べてください", [lib.instance_tag(i), a.path, op])
}

deny contains msg if {
	some i in lib.instances
	some a in statements(i)
	some op, kv in conditions(a.statement)
	some v in lib.as_array(kv["aws:principalarn"])
	not exact_role_arn(v)
	msg := sprintf("[PLAN-SBW-02] %s%s: aws:PrincipalArn の値 %v が role の ARN の完全一致ではありません（user・assumed-role・root・ワイルドカードは受けません）", [lib.instance_tag(i), a.path, v])
}

# 書き込みの拒否を object に限る理由は state-backend/ADR-0104 決定3。
deny contains msg if {
	some i in lib.instances
	bucket_known(i)
	some a in denies(i)
	not tls(a.statement)
	xs := resources(a.statement) | not_resources(a.statement)
	some x in xs
	not object_resource(i, x)
	msg := sprintf("[PLAN-SBW-03] %s%s: TLS 以外の Deny の Resource・NotResource に、state Bucket の object でない %v があります", [lib.instance_tag(i), a.path, x])
}

object_resource(i, x) if startswith(x, sprintf("%s/", [bucket_arn(i)]))

deny contains msg if {
	some i in lib.instances
	some a in denies(i)
	not tls(a.statement)
	count(actions(a.statement) - object_write_actions) > 0
	msg := sprintf("[PLAN-SBW-03] %s%s: TLS 以外の Deny の Action %v が、object の書き込みの5つ（s3:PutObject・s3:DeleteObject・s3:DeleteObjectVersion・s3:ReplicateObject・s3:ReplicateDelete）の部分集合ではありません", [lib.instance_tag(i), a.path, sort(actions(a.statement))])
}

deny contains msg if {
	some i in lib.instances
	some a in denies(i)
	not tls(a.statement)
	a.statement.NotAction
	msg := sprintf("[PLAN-SBW-03] %s%s: TLS 以外の Deny が NotAction を使っています。Action を列挙してください", [lib.instance_tag(i), a.path])
}

deny contains msg if {
	some i in lib.instances
	some arn, w in writers(i)
	is_string(bucket_arn(i))
	count(bucket_policies(i)) > 0
	count({a |
		some a in denies(i)
		confining(a, arn)
		not_resources(a.statement) == w.keys
		count(resources(a.statement)) == 0
		actions(a.statement) == object_write_actions
	}) != 1
	msg := sprintf("[PLAN-SBW-04] %s%s を、自分の key %v の外への書き込みを Principal \"*\" と aws:PrincipalArn の ArnEquals だけの条件で拒否する文で閉じ込めていません", [lib.instance_tag(i), arn, sort(w.keys)])
}

deny contains msg if {
	some i in lib.instances
	some arn, w in writers(i)
	is_string(bucket_arn(i))
	count(bucket_policies(i)) > 0
	count({a |
		some a in denies(i)
		confining(a, arn)
		resources(a.statement) == w.keys
		count(not_resources(a.statement)) == 0
		actions(a.statement) == w.denied
	}) != 1
	msg := sprintf("[PLAN-SBW-04] %s%s の key %v への操作を、Principal \"*\" と aws:PrincipalArn の ArnEquals だけの条件の %v の拒否で閉じ込めていません", [lib.instance_tag(i), arn, sort(w.keys), sort(w.denied)])
}

deny contains msg if {
	some i in lib.instances
	some a in denies(i)
	some arn in principal_arn_values(a.statement, "arnequals")
	not writers(i)[arn]
	msg := sprintf("[PLAN-SBW-04] %s%s: 宣言した writer でない %s を閉じ込める文があります", [lib.instance_tag(i), a.path, arn])
}

# key の重なりは宣言だけで決まるので、instance に依らず1回見る。
deny contains msg if {
	some i, r1 in object.get(cfg, "roots", [])
	some j, r2 in object.get(cfg, "roots", [])
	i < j
	count(state_keys_of(r1) & state_keys_of(r2)) > 0
	msg := sprintf("[PLAN-SBW-05] root %q と %q の key が重なっています", [r1.writer, r2.writer])
}

deny contains msg if {
	some i in lib.instances
	some root in object.get(cfg, "roots", [])
	some a in denies(i)
	some arn in principal_arn_values(a.statement, "arnequals")
	arn != role_arn(i, root.writer)
	object_arn(i, root.state) in not_resources(a.statement)
	msg := sprintf("[PLAN-SBW-05] %s%s: %s が root の state の key %s に書けます", [lib.instance_tag(i), a.path, arn, object_arn(i, root.state)])
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

key_grants(i) := {{"document": r.document, "statement": s} |
	some r in key_policies(i)
	some s in lib.statements(r.body)
	s.Effect == "Allow"
	grants_crypto(s)
}

key_users(i) := {p |
	some g in key_grants(i)
	is_object(g.statement.Principal)
	some p in lib.as_array(object.get(g.statement.Principal, "AWS", []))
}

deny contains msg if {
	some i in lib.instances
	some g in key_grants(i)
	not aws_only(g.statement.Principal)
	msg := sprintf("[PLAN-SBW-06] %s%s: %s が AWS の role でない principal %v に暗号操作を許しています", [lib.instance_tag(i), g.document.path, g.document.address, g.statement.Principal])
}

aws_only(p) if {
	is_object(p)
	object.keys(p) == {"AWS"}
}

declared_key_users(i) := {role_key(i, name) | some name in object.get(cfg, "key_user_roles", [])}

deny contains msg if {
	some i in lib.instances
	count(key_policies(i)) > 0
	users := key_users(i)
	users != declared_key_users(i)
	msg := sprintf("[PLAN-SBW-06] %sstate の鍵の暗号操作を許す principal %v が、宣言（data.policy_plan.state_backend.key_user_roles）の role %v と一致しません", [lib.instance_tag(i), sort(users), sort(declared_key_users(i))])
}

deny contains msg if {
	some i in lib.instances
	some r in key_policies(i)
	some s in lib.statements(r.body)
	s.Effect == "Deny"
	msg := sprintf("[PLAN-SBW-06] %s%s: %s が Deny の文を持っています。拒否は Bucket Policy に置き、Key Policy には置きません", [lib.instance_tag(i), r.document.path, r.document.address])
}

deny contains msg if {
	some i in lib.instances
	is_string(bucket_arn(i))
	count(bucket_policies(i)) > 0
	count({a | some a in denies(i); other_than_kms(i, a)}) != 1
	msg := sprintf("[PLAN-SBW-07] %sstate Bucket の Bucket Policy に、方式の header が在って aws:kms でない s3:PutObject を Principal \"*\" に拒否する文が1つではありません", [lib.instance_tag(i)])
}

deny contains msg if {
	some i in lib.instances
	is_string(bucket_arn(i))
	count(bucket_policies(i)) > 0
	count({a | some a in denies(i); managed_key(i, a)}) != 1
	msg := sprintf("[PLAN-SBW-07] %sstate Bucket の Bucket Policy に、方式の header が aws:kms で鍵 ID の header が無い s3:PutObject を Principal \"*\" に拒否する文が1つではありません", [lib.instance_tag(i)])
}

deny contains msg if {
	some i in lib.instances
	is_string(bucket_arn(i))
	count(bucket_policies(i)) > 0
	count({a | some a in denies(i); other_key(i, a)}) != 1
	msg := sprintf("[PLAN-SBW-07] %sstate Bucket の Bucket Policy に、別の鍵 ID の s3:PutObject を Principal \"*\" に拒否する文が1つではありません", [lib.instance_tag(i)])
}

deny contains msg if {
	some i in lib.instances
	state_key_known(i)
	some a in denies(i)
	other_key(i, a)
	{v | some v in lib.as_array(conditions(a.statement).stringnotequals[sse_key])} != {state_key_arn(i)}
	msg := sprintf("[PLAN-SBW-07] %s%s: 別の鍵 ID を拒否する文の値が、state の鍵の ARN %v ではありません", [lib.instance_tag(i), a.path, state_key_arn(i)])
}

deny contains msg if {
	some i in lib.instances
	some a in statements(i)
	some k, v in object.get(conditions(a.statement), "null", {})
	not v in {"true", "false"}
	msg := sprintf("[PLAN-SBW-07] %s%s: Null の %s の値 %v が文字列の \"true\" か \"false\" ではありません", [lib.instance_tag(i), a.path, k, v])
}

tls_deny(i, a) if {
	tls(a.statement)
	a.statement.Principal == "*"
	actions(a.statement) == {"s3:*"}
	resources(a.statement) == {bucket_arn(i), sprintf("%s/*", [bucket_arn(i)])}
}

deny contains msg if {
	some i in lib.instances
	is_string(bucket_arn(i))
	count(bucket_policies(i)) > 0
	count({a | some a in denies(i); tls_deny(i, a)}) != 1
	msg := sprintf("[PLAN-SBW-08] %sstate Bucket の Bucket Policy に、%s と %s/* への s3:* を aws:SecureTransport が false の要求について Principal \"*\" に拒否する文が1つではありません", [lib.instance_tag(i), bucket_arn(i), bucket_arn(i)])
}

# Allow は IAM の側に置く。ここで持つ理由は conftest/plan/README.md の plan.state_backend_write_deny の行にある。
deny contains msg if {
	some i in lib.instances
	some a in statements(i)
	a.statement.Effect != "Deny"
	msg := sprintf("[PLAN-SBW-09] %s%s: state Bucket の Bucket Policy が Deny でない文（Effect %v）を持っています", [lib.instance_tag(i), a.path, a.statement.Effect])
}

# CI の role の Key Policy の object を、その role の IAM の S3 の object と突き合わせる（state-backend/ADR-0201 決定9）。
key_object_roles := object.get(cfg, "key_object_roles", null)

key_object_names := key_object_roles if is_array(key_object_roles)

default key_object_names := []

iam_policy_raw(name) := object.get(object.get(di, name, {}), "policy", "")

iam_policy(i, name) := at(iam_policy_raw(name), i)

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
	iam_policy_raw(name) == ""
	msg := sprintf("[PLAN-SBW-00] data.policy_plan.deployment_identity.%s.policy が宣言にありません", [name])
}

deny contains msg if {
	some name in key_object_names
	iam_policy_raw(name) != ""
	not lib.rebasable(iam_policy_raw(name))
	msg := sprintf("[PLAN-SBW-00] data.policy_plan.deployment_identity.%s.policy %q が state_backend.module か deployment_identity.module で始まりません", [name, iam_policy_raw(name)])
}

iam_documents(i, name) := {d |
	some d in lib.documents
	d.type == "aws_iam_role_policy"
	d.address == iam_policy(i, name)
	lib.in_root(d.path, i)
}

deny contains msg if {
	some i in lib.instances
	some name in key_object_names
	iam_policy_raw(name) != ""
	count(iam_documents(i, name)) == 0
	msg := sprintf("[PLAN-SBW-00] %s%s の IAM の policy %q が入力にありません。address が変わったなら data.policy_plan.deployment_identity.%s.policy を直してください", [lib.instance_tag(i), name, iam_policy(i, name), name])
}

# 作成の plan では、鍵の ARN を Resource に持つ IAM の policy は決まらない。決まらない policy の突き合わせは止め、決まる policy は見る。
deny contains msg if {
	some i in lib.instances
	not creating(i)
	some name in key_object_names
	some d in iam_documents(i, name)
	not lib.parsed(d)
	msg := sprintf("[PLAN-SBW-10] %s%s: %s の IAM の policy %s が plan で決まらないか、JSON の文書として読めません", [lib.instance_tag(i), d.path, name, d.address])
}

iam_readable(i, name) if {
	some d in iam_documents(i, name)
	lib.parsed(d)
}

iam_compared(i, _) if not creating(i)

iam_compared(i, name) if iam_readable(i, name)

iam_allows(i, name) := {{"path": r.document.path, "statement": s} |
	some r in lib.readable
	r.document.type == "aws_iam_role_policy"
	r.document.address == iam_policy(i, name)
	lib.in_root(r.document.path, i)
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

iam_objects(i, name, actions) := {x |
	some a in iam_allows(i, name)
	some action in actions
	allows_action(a.statement, action)
	some x in lib.as_array(object.get(a.statement, "Resource", []))
}

deny contains msg if {
	some i in lib.instances
	some name in key_object_names
	some a in iam_allows(i, name)
	a.statement.NotResource
	some action in ["s3:getobject", "s3:putobject"]
	allows_action(a.statement, action)
	msg := sprintf("[PLAN-SBW-10] %s%s: %s の IAM の policy %s の S3 の object の Allow が NotResource を使っています。Resource を列挙してください", [lib.instance_tag(i), a.path, name, iam_policy(i, name)])
}

key_role_grants(i, name) := {{"document": r.document, "statement": s} |
	some r in key_policies(i)
	some s in lib.statements(r.body)
	s.Effect == "Allow"
	is_object(s.Principal)
	some p in lib.as_array(object.get(s.Principal, "AWS", []))
	p == role_key(i, name)
}

key_objects(i, name, action) := {v |
	some g in key_role_grants(i, name)
	allows_action(g.statement, action)
	some v in lib.limiting_values(g.statement, "kms:encryptioncontext:aws:s3:arn")
}

deny contains msg if {
	some i in lib.instances
	some name in key_object_names
	role_identified(i, name)
	some g in key_role_grants(i, name)
	some action in ["kms:decrypt", "kms:generatedatakey"]
	allows_action(g.statement, action)
	count(lib.limiting_values(g.statement, "kms:encryptioncontext:aws:s3:arn")) == 0
	msg := sprintf("[PLAN-SBW-10] %s%s: %s が %s の role に、暗号操作を object（kms:EncryptionContext:aws:s3:arn）で限らずに許しています", [lib.instance_tag(i), g.document.path, g.document.address, name])
}

# IAM は同じ条件キーの2つの演算子を AND で読むので、値の和で比べると、どちらも満たせない文を一致と見誤る。
deny contains msg if {
	some i in lib.instances
	some name in key_object_names
	role_identified(i, name)
	some g in key_role_grants(i, name)
	count([k |
		some _, kv in object.get(g.statement, "Condition", {})
		some k, _ in kv
		lower(k) == "kms:encryptioncontext:aws:s3:arn"
	]) > 1
	msg := sprintf("[PLAN-SBW-10] %s%s: %s の %s の role への文が kms:EncryptionContext:aws:s3:arn を2つ以上の演算子（かキー）に置いています", [lib.instance_tag(i), g.document.path, g.document.address, name])
}

# 突き合わせる object が両側とも無いときは、一致ではなく何も見ていない。
deny contains msg if {
	some i in lib.instances
	some name in key_object_names
	role_identified(i, name)
	count(key_objects(i, name, "kms:decrypt")) == 0
	count(iam_objects(i, name, ["s3:getobject", "s3:putobject"])) == 0
	msg := sprintf("[PLAN-SBW-10] %s%s の role %s に、Key Policy も IAM も S3 の object を1つも許していません。宣言 key_object_roles か policy の address を確かめてください", [lib.instance_tag(i), name, role_label(i, name)])
}

# 文字列の完全一致で比べる。pattern 同士も文字列として比べ、当たる範囲の包含は読まない。
deny contains msg if {
	some i in lib.instances
	some name in key_object_names
	role_identified(i, name)
	iam_compared(i, name)
	count(key_policies(i)) > 0
	keys := key_objects(i, name, "kms:decrypt")
	iam := iam_objects(i, name, ["s3:getobject", "s3:putobject"])
	keys != iam
	msg := sprintf("[PLAN-SBW-10] %s%s の role %s に Key Policy が kms:Decrypt を許す object %v が、IAM の s3:GetObject・s3:PutObject の Resource %v と一致しません", [lib.instance_tag(i), name, role_label(i, name), sort(keys), sort(iam)])
}

deny contains msg if {
	some i in lib.instances
	some name in key_object_names
	role_identified(i, name)
	iam_compared(i, name)
	count(key_policies(i)) > 0
	keys := key_objects(i, name, "kms:generatedatakey")
	iam := iam_objects(i, name, ["s3:putobject"])
	keys != iam
	msg := sprintf("[PLAN-SBW-10] %s%s の role %s に Key Policy が kms:GenerateDataKey を許す object %v が、IAM の s3:PutObject の Resource %v と一致しません", [lib.instance_tag(i), name, role_label(i, name), sort(keys), sort(iam)])
}
