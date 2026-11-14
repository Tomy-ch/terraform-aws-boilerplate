# 宣言した Bucket と鍵の resource-based policy が、宣言した1つだけであること（state-backend/ADR-0101・ADR-0104）。
# 宣言の外の resource が同じ resource を指すと、宣言した文書を読む Policy が読まない文書が実体になりうる。
package plan.resource_policy_exclusive

import data.plan.lib

state_backend := object.get(data.policy_plan, "state_backend", {})

apply_identity := object.get(object.get(data.policy_plan, "deployment_identity", {}), "apply_identity_role", {})

# 宣言が欠けた組は作らず、PLAN-RPE-00 で落とす。CI の実 plan は宣言の欠けを見るほかの package を掛けない。
pairs contains pair("aws_s3_bucket_policy", "bucket", state_backend.bucket, state_backend.bucket_policy, ["id", "bucket", "arn"]) if {
	is_string(state_backend.bucket)
	is_string(state_backend.bucket_policy)
}

pairs contains pair("aws_s3_bucket_policy", "bucket", apply_identity.output_bucket, apply_identity.output_bucket_policy, ["id", "bucket", "arn"]) if {
	is_string(apply_identity.output_bucket)
	is_string(apply_identity.output_bucket_policy)
}

# PutKeyPolicy は鍵の ID と ARN のどちらも受ける。mock の plan では key_id が鍵の id を指す。
pairs contains pair("aws_kms_key_policy", "key_id", state_backend.state_key, state_backend.key_policy, ["id", "key_id", "arn"]) if {
	is_string(state_backend.state_key)
	is_string(state_backend.key_policy)
}

pair(type, attr, target, policy, refs) := {"type": type, "attr": attr, "target": target, "policy": policy, "refs": refs}

target_after(p) := c.rc.change.after if {
	some c in lib.changes
	c.rc.address == p.target
	is_object(c.rc.change.after)
}

refs(p) := {v | some a in p.refs; v := object.get(target_after(p), a, null); is_string(v)}

on(c, p) if object.get(c.rc.change.after, p.attr, null) in refs(p)

unknown_ref(c, p) if object.get(object.get(c.rc.change, "after_unknown", {}), p.attr, false) == true

declared(type) := {p.policy | some p in pairs; p.type == type}

# 管理からの除外（forget）と削除は after を持たず、書かない。
writes(c) if is_object(c.rc.change.after)

deny contains msg if {
	some p in pairs
	some c in lib.changes
	c.rc.type == p.type
	writes(c)
	not c.rc.address in declared(p.type)
	on(c, p)
	msg := sprintf("[PLAN-RPE-01] %s: %s が %s を指します。その policy は %s だけに置いてください", [c.path, c.rc.address, p.target, p.policy])
}

# 指す先が plan で決まらなければ、宣言した resource を指すかを読めない。
deny contains msg if {
	some type in {p.type | some p in pairs}
	some c in lib.changes
	c.rc.type == type
	writes(c)
	not c.rc.address in declared(type)
	some p in pairs
	p.type == type
	unknown_ref(c, p)
	msg := sprintf("[PLAN-RPE-01] %s: %s がどの resource を指すかを plan で読めません。宣言した resource の policy は宣言した1つだけに置いてください", [c.path, c.rc.address])
}

# 宣言した policy と参照先を同じ plan で作ると、どちらの値も plan で決まらない。そのときは比べられないので通す。
deny contains msg if {
	some p in pairs
	some c in lib.changes
	c.rc.address == p.policy
	writes(c)
	not on(c, p)
	not created_together(c, p)
	msg := sprintf("[PLAN-RPE-02] %s: 宣言した %s が %s を指していません", [c.path, c.rc.address, p.target])
}

created_together(c, p) if {
	unknown_ref(c, p)
	some t in lib.changes
	t.rc.address == p.target
	some a in p.refs
	object.get(object.get(t.rc.change, "after_unknown", {}), a, false) == true
}

declared_keys := [
	["state_backend", "bucket"],
	["state_backend", "bucket_policy"],
	["state_backend", "state_key"],
	["state_backend", "key_policy"],
	["deployment_identity", "apply_identity_role", "output_bucket"],
	["deployment_identity", "apply_identity_role", "output_bucket_policy"],
]

declared_string(path) if {
	v := object.get(data.policy_plan, path, null)
	is_string(v)
	v != ""
}

deny contains msg if {
	some path in declared_keys
	not declared_string(path)
	msg := sprintf("[PLAN-RPE-00] 宣言の policy_plan.%s が、空でない文字列ではありません", [concat(".", path)])
}

in_input(address) if {
	some f in input
	some rc in f.contents.resource_changes
	rc.address == address
}

deny contains msg if {
	some path in declared_keys
	declared_string(path)
	address := object.get(data.policy_plan, path, null)
	not in_input(address)
	msg := sprintf("[PLAN-RPE-00] 宣言の policy_plan.%s の %s が入力にありません。address が変わったなら宣言を直してください", [concat(".", path), address])
}
