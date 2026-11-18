# 宣言した Bucket と鍵の resource-based policy が、宣言した1つだけであること（state-backend/ADR-0101・ADR-0104）。
# 宣言の外の resource が同じ resource を指すと、宣言した文書を読む Policy が読まない文書が実体になりうる。
package plan.resource_policy_exclusive

import data.plan.lib

# 宣言した resource と、それを指す policy の組。宣言の位置は data.policy_plan の下の key の並びで持ち、
# PLAN-RPE-00 も同じ表から宣言の欠けを見る。PutKeyPolicy は鍵の ID と ARN のどちらも受ける。mock の plan では
# key_id が鍵の id を指す。statements は policy の文の数の宣言の位置（state-backend/ADR-0201 決定8）。
specs := [
	{"type": "aws_s3_bucket_policy", "attr": "bucket", "target": ["state_backend", "bucket"], "policy": ["state_backend", "bucket_policy"], "refs": ["id", "bucket", "arn"]},
	{"type": "aws_s3_bucket_policy", "attr": "bucket", "target": ["deployment_identity", "apply_identity_role", "output_bucket"], "policy": ["deployment_identity", "apply_identity_role", "output_bucket_policy"], "refs": ["id", "bucket", "arn"], "statements": ["deployment_identity", "apply_identity_role", "output_bucket_policy_statements"]},
	{"type": "aws_s3_bucket_policy", "attr": "bucket", "target": ["state_backend", "apply_output_bucket"], "policy": ["state_backend", "apply_output_bucket_policy"], "refs": ["id", "bucket", "arn"], "statements": ["state_backend", "apply_output_bucket_policy_statements"]},
	{"type": "aws_kms_key_policy", "attr": "key_id", "target": ["state_backend", "state_key"], "policy": ["state_backend", "key_policy"], "refs": ["id", "key_id", "arn"]},
]

# 宣言が欠けた組は作らず、PLAN-RPE-00 で落とす。CI の実 plan は宣言の欠けを見るほかの package を掛けない。
pairs contains {"type": spec.type, "attr": spec.attr, "target": target, "policy": policy, "refs": spec.refs, "statements": object.get(spec, "statements", [])} if {
	some spec in specs
	target := object.get(data.policy_plan, spec.target, null)
	policy := object.get(data.policy_plan, spec.policy, null)
	is_string(target)
	is_string(policy)
}

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

declared_keys := {path | some spec in specs; some path in [spec.target, spec.policy]}

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

statement_count(path) := n if {
	n := object.get(data.policy_plan, path, null)
	is_number(n)
	n >= 1
	n == round(n)
}

deny contains msg if {
	some spec in specs
	path := object.get(spec, "statements", [])
	count(path) > 0
	not statement_count(path)
	msg := sprintf("[PLAN-RPE-00] 宣言の policy_plan.%s が、1以上の整数ではありません", [concat(".", path)])
}

# 文書が plan で決まらなければ数えない。宣言の文を足すか外す変更は、宣言の数も変える。
deny contains msg if {
	some p in pairs
	count(p.statements) > 0
	n := statement_count(p.statements)
	some r in lib.readable
	r.document.address == p.policy
	count(lib.statements(r.body)) != n
	msg := sprintf("[PLAN-RPE-03] %s: %s の文が %d 件で、宣言（policy_plan.%s）の %d 件と一致しません", [r.document.path, p.policy, count(lib.statements(r.body)), concat(".", p.statements), n])
}
