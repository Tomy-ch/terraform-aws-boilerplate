# deployment-identity の apply の role が、bootstrap の構成物を変更できないこと（ADR-0602 決定13、ADR-0604 決定10、
# deployment-identity/ADR-0201 決定5）。Allow は評価しない。明示の Deny はほかのどの policy の Allow にも勝つので、
# 構成物の ARN ごとに、読み取り以外のすべてを拒む条件なしの Deny が在ることを見る。
# 構成物（state-backend）と apply の role（deployment-identity）は別の root の plan に在るので、結合した入力で突き合わせる。
# 対象の ARN は検査する policy からではなく、構成物の resource の値と宣言から組む。
package plan.deployment_identity_apply_role

import data.plan.lib

di := object.get(data.policy_plan, "deployment_identity", {})

cfg := object.get(di, "apply_role", {})

sb := object.get(data.policy_plan, "state_backend", {})

bootstrap := object.get(cfg, "bootstrap", {})

# index key 付きの address（["apply"]）で宣言する。同じ resource のほかの instance と分けるため。
default target := ""

target := object.get(cfg, "policy", "")

default role := ""

role := object.get(cfg, "role", "")

after(address) := c.rc.change.after if {
	some c in lib.changes
	c.rc.address == address
}

arn_shaped(x) if {
	is_string(x)
	regex.match(`^arn:aws[a-z-]*:[a-z0-9-]+:[a-z0-9-]*:([0-9]{12})?:.+$`, x)
}

# --- 宣言 ---

modules := object.get(bootstrap, "modules", [])

types := object.get(bootstrap, "types", {})

children := object.get(bootstrap, "children", {})

nonempty_string(x) if {
	is_string(x)
	x != ""
}

# 宣言のキーは閉じた集合にする。綴りを誤ったキーは既定値として読まれ、要求が黙って消える。
well_formed_type(t) if {
	is_object(t)
	t.class == "target"
	object.keys(t) - {"class", "children"} == set()
	is_boolean(object.get(t, "children", false))
}

well_formed_type(t) if {
	is_object(t)
	t.class == "key"
	object.keys(t) == {"class"}
}

well_formed_type(t) if {
	is_object(t)
	t.class == "covered_by"
	object.keys(t) == {"class", "parent", "ref", "parent_ref"}
	nonempty_string(t.parent)
	nonempty_string(t.ref)
	nonempty_string(t.parent_ref)
}

well_formed_child(c) if {
	is_object(c)
	object.keys(c) - {"suffixes", "roots", "version_deletion"} == set()
	is_array(object.get(c, "suffixes", []))
	every s in object.get(c, "suffixes", []) {
		nonempty_string(s)
	}
	is_boolean(object.get(c, "roots", false))
	is_boolean(object.get(c, "version_deletion", false))
	yields_child(c)
}

yields_child(c) if count(object.get(c, "suffixes", [])) > 0

yields_child(c) if c.roots == true

well_formed_bootstrap if {
	is_array(modules)
	count(modules) > 0
	every m in modules {
		nonempty_string(m)
	}
	is_object(types)
	count(types) > 0
	every _, t in types {
		well_formed_type(t)
	}
	is_object(children)
	every _, c in children {
		well_formed_child(c)
	}
}

# --- 構成物 ---

in_scope(address) if {
	some m in modules
	startswith(address, m)
}

resources contains c.rc if {
	well_formed_bootstrap
	some c in lib.changes
	in_scope(c.rc.address)
}

class_of(type) := types[type].class

instances(type) := {r | some r in resources; r.type == type}

# 型の名前の前までが module の path（module.a.module.b.）。
module_of(r) := substring(r.address, 0, indexof(r.address, sprintf("%s.", [r.type])))

writer_roots := [root |
	some root in object.get(sb, "roots", [])
	is_object(root)
	object.get(root, "writer", "") != "apply_role"
	is_string(root.state)
]

suffix_arns(address, arn) := {sprintf("%s%s", [arn, s]) | some s in object.get(children[address], "suffixes", [])}

# 書き手が apply の role でない root の state と lock file。apply の role は読み取りも要らないので、* だけを認める。
root_arns(address, arn) := {sprintf("%s/%s", [arn, k]) |
	object.get(children[address], "roots", false) == true
	some root in writer_roots
	some k in [root.state, sprintf("%s.tflock", [root.state])]
}

targets contains arn if {
	some r in resources
	class_of(r.type) == "target"
	arn := r.change.after.arn
	arn_shaped(arn)
}

targets contains arn if {
	some r in resources
	class_of(r.type) == "target"
	arn_shaped(r.change.after.arn)
	r.address in object.keys(children)
	some arn in suffix_arns(r.address, r.change.after.arn)
}

strict_targets contains arn if {
	some r in resources
	class_of(r.type) == "target"
	arn_shaped(r.change.after.arn)
	r.address in object.keys(children)
	some arn in root_arns(r.address, r.change.after.arn)
}

version_deletion_targets contains sprintf("%s/*", [r.change.after.arn]) if {
	some r in resources
	object.get(object.get(children, r.address, {}), "version_deletion", false) == true
	arn_shaped(r.change.after.arn)
}

# --- apply の role の policy ---

statements contains s if {
	some r in lib.readable
	r.document.type == "aws_iam_role_policy"
	r.document.address == target
	some s in lib.statements(r.body)
}

unconditional_deny(s) if {
	s.Effect == "Deny"
	count(object.get(s, "Condition", {})) == 0
	not "NotResource" in object.keys(s)
}

on(s, arn) if arn in lib.as_array(object.get(s, "Resource", []))

read_name(a) if regex.match(`^[a-z0-9-]+:(get|list|describe|head)[a-z0-9]*$`, a)

denies_all(s) if {
	"*" in lib.as_array(object.get(s, "Action", []))
	not "NotAction" in object.keys(s)
}

denies_all_but_reads(s) if denies_all(s)

denies_all_but_reads(s) if {
	not "Action" in object.keys(s)
	"NotAction" in object.keys(s)
	every a in lib.lower_all(s.NotAction) {
		read_name(a)
	}
}

covered(arn) if {
	some s in statements
	unconditional_deny(s)
	on(s, arn)
	denies_all_but_reads(s)
}

covered_strictly(arn) if {
	some s in statements
	unconditional_deny(s)
	on(s, arn)
	denies_all(s)
}

version_deletion_denied(arn) if {
	some s in statements
	unconditional_deny(s)
	on(s, arn)
	some a in lib.lower_all(object.get(s, "Action", []))
	a in {"s3:deleteobjectversion", "*"}
}

# 鍵と Key Policy は module の path で組にする。Key Policy がどの鍵を指すかは PLAN-APPLY-02 が key_id で見る。
account_root(arn) if regex.match(`^arn:aws[a-z-]*:iam::[0-9]{12}:root$`, arn)

keys contains r if {
	some r in resources
	class_of(r.type) == "key"
}

key_policies contains r if {
	some r in lib.readable
	r.document.type == "aws_kms_key_policy"
	in_scope(r.document.address)
}

policy_module(r) := substring(r.document.address, 0, indexof(r.document.address, "aws_kms_key_policy."))

pairs contains {"key": k, "policy": r} if {
	some k in keys
	some r in key_policies
	policy_module(r) == module_of(k)
}

delegated(body) := {a |
	some s in lib.statements(body)
	s.Effect == "Allow"
	is_object(s.Principal)
	some p in lib.as_array(object.get(s.Principal, "AWS", []))
	account_root(p)
	some a in lib.lower_all(s.Action)
}

denied_on(arn) := {a |
	some s in statements
	unconditional_deny(s)
	on(s, arn)
	some a in lib.lower_all(object.get(s, "Action", []))
}

key_covered(_, denied) if "kms:*" in denied

key_covered(_, denied) if "*" in denied

key_covered(a, denied) if a in denied

# --- apply の role に付く policy ---

role_name := after(role).name

role_known if is_string(role_name)

attached contains {"path": c.path, "address": c.rc.address, "type": c.rc.type, "after": c.rc.change.after, "unknown": c.rc.change.after_unknown} if {
	some c in lib.changes
	c.rc.type in {"aws_iam_role_policy", "aws_iam_role_policy_attachment", "aws_iam_role_policies_exclusive", "aws_iam_role_policy_attachments_exclusive"}
}

role_key(a) := "role_name" if endswith(a.type, "_exclusive")

role_key(a) := "role" if not endswith(a.type, "_exclusive")

on_apply_role(a) if object.get(a.after, role_key(a), null) == role_name

exclusive(type) := {a | some a in attached; a.type == type; on_apply_role(a)}

# --- PLAN-APPLY-00: 入力と宣言の欠落 ---

deny contains msg if {
	count({d | some d in lib.documents; d.type == "aws_iam_role_policy"; d.address == target}) == 0
	msg := sprintf("[PLAN-APPLY-00] apply の role の policy %q が入力にありません。address が変わったなら data.policy_plan.deployment_identity.apply_role を直してください", [target])
}

deny contains msg if {
	not role_known
	msg := sprintf("[PLAN-APPLY-00] apply の role %q の名前が入力にありません。address が変わったなら data.policy_plan.deployment_identity.apply_role.role を直してください", [role])
}

deny contains msg if {
	role_known
	object.get(after(target), "role", null) != role_name
	msg := sprintf("[PLAN-APPLY-00] apply の role の policy %q が、apply の role %q に付いていません", [target, role_name])
}

# 検査は ARN を文字列で突き合わせる。role の ARN を名前から組む policy と、plan の ARN の形が食い違うと、Deny の Resource が対象と一致しない。
deny contains msg if {
	role_known
	some r in instances("aws_iam_role")
	not role_shape(r.change.after)
	msg := sprintf("[PLAN-APPLY-00] role %s の path が / でないか、ARN %v が :role/%v で終わりません", [r.address, object.get(r.change.after, "arn", null), object.get(r.change.after, "name", null)])
}

role_shape(a) if {
	object.get(a, "path", null) in {null, "/"}
	endswith(a.arn, sprintf(":role/%s", [a.name]))
}

deny contains msg if {
	some k in keys
	count({p | some p in pairs; p.key == k}) != 1
	msg := sprintf("[PLAN-APPLY-00] 鍵 %s と同じ module の下の Key Policy（aws_kms_key_policy）が %d 件です（1件を期待）", [k.address, count({p | some p in pairs; p.key == k})])
}

deny contains msg if {
	some r in key_policies
	count({p | some p in pairs; p.policy == r}) != 1
	msg := sprintf("[PLAN-APPLY-00] %s: Key Policy %s と同じ module の下の鍵が %d 件です（1件を期待）", [r.document.path, r.document.address, count({p | some p in pairs; p.policy == r})])
}

deny contains msg if {
	some k in keys
	not arn_shaped(object.get(k.change.after, "arn", null))
	msg := sprintf("[PLAN-APPLY-00] 鍵 %s の ARN が plan で決まっていないか、ARN の形ではありません。tests/policy-input.tftest.hcl の override_resource で与えてください", [k.address])
}

# 委任の Principal を読めない Key Policy では、突き合わせる Action が0件になり、PLAN-APPLY-01 が黙る。
deny contains msg if {
	some r in key_policies
	count(delegated(r.body)) == 0
	msg := sprintf("[PLAN-APPLY-00] %s: %s に、アカウントへの委任の Allow（Principal の AWS が :root の ARN）がありません", [r.document.path, r.document.address])
}

deny contains msg if {
	not well_formed_bootstrap
	msg := "[PLAN-APPLY-00] data.policy_plan.deployment_identity.apply_role.bootstrap が、modules（空でない文字列の配列）/ types（class が target（children は真偽値）・key・covered_by（parent・ref・parent_ref が必須）。ほかのキーを持たない）/ children（suffixes・roots・version_deletion だけを持ち、子の ARN を1つ以上生む）の形を満たしていません"
}

deny contains msg if {
	well_formed_bootstrap
	some m in modules
	count({c | some c in lib.changes; startswith(c.rc.address, m)}) == 0
	msg := sprintf("[PLAN-APPLY-00] bootstrap の module %q の下に resource が入力にありません。address が変わったなら data.policy_plan.deployment_identity.apply_role.bootstrap.modules を直してください", [m])
}

deny contains msg if {
	well_formed_bootstrap
	some type, t in types
	t.class in {"target", "key"}
	count(instances(type)) == 0
	msg := sprintf("[PLAN-APPLY-00] bootstrap の構成物の型 %s の resource が入力にありません", [type])
}

deny contains msg if {
	some r in resources
	class_of(r.type) == "target"
	not arn_shaped(object.get(r.change.after, "arn", null))
	msg := sprintf("[PLAN-APPLY-00] bootstrap の構成物 %s の ARN が plan で決まっていないか、ARN の形ではありません。tests/policy-input.tftest.hcl の override_resource で与えてください", [r.address])
}

deny contains msg if {
	well_formed_bootstrap
	some address, _ in children
	count({r | some r in resources; r.address == address}) == 0
	msg := sprintf("[PLAN-APPLY-00] data.policy_plan.deployment_identity.apply_role.bootstrap.children の %s が入力にありません", [address])
}

# 子を持つと宣言した型の target でない resource に書いた children は、どの ARN も生まず黙って捨てられる。
deny contains msg if {
	some r in resources
	r.address in object.keys(children)
	not child_holder(r)
	msg := sprintf("[PLAN-APPLY-00] data.policy_plan.deployment_identity.apply_role.bootstrap.children の %s は、children が true の target の型の resource ではありません", [r.address])
}

child_holder(r) if {
	class_of(r.type) == "target"
	object.get(types[r.type], "children", false) == true
}

deny contains msg if {
	count(writer_roots) == 0
	msg := "[PLAN-APPLY-00] data.policy_plan.state_backend.roots に、apply の role のほかの書き手の root がありません（identity root の state と lock file を組めない）"
}

# --- PLAN-APPLY-01: 鍵 ---

deny contains msg if {
	some p in pairs
	arn := p.key.change.after.arn
	denied := denied_on(arn)
	missing := {a | some a in delegated(p.policy.body); not key_covered(a, denied)}
	count(missing) > 0
	msg := sprintf("[PLAN-APPLY-01] %s: %s がアカウントへ委任する %v を、apply の role %q が鍵の ARN %v への Deny で拒んでいません", [p.policy.document.path, p.policy.document.address, sort(missing), target, arn])
}

# --- PLAN-APPLY-02: 分類の網羅 ---

deny contains msg if {
	some r in resources
	not r.type in object.keys(types)
	msg := sprintf("[PLAN-APPLY-02] bootstrap の構成物 %s の型 %s が、data.policy_plan.deployment_identity.apply_role.bootstrap.types で分類されていません", [r.address, r.type])
}

# 外の resource を指す子は、その親の ARN の Deny に守られない。
deny contains msg if {
	some r in resources
	class_of(r.type) == "covered_by"
	t := types[r.type]
	not points_to_guarded_parent(r, t)
	msg := sprintf("[PLAN-APPLY-02] %s の %s（%v）が、bootstrap の %s（target か key）の %s のどれとも一致しません", [r.address, t.ref, object.get(r.change.after, t.ref, null), t.parent, t.parent_ref])
}

points_to_guarded_parent(r, t) if {
	v := r.change.after[t.ref]
	nonempty_string(v)
	some p in resources
	p.type == t.parent
	class_of(p.type) in {"target", "key"}
	p.change.after[t.parent_ref] == v
}

deny contains msg if {
	some r in resources
	class_of(r.type) == "target"
	object.get(types[r.type], "children", false) == true
	not r.address in object.keys(children)
	msg := sprintf("[PLAN-APPLY-02] %s の子の ARN（object・log stream など）が data.policy_plan.deployment_identity.apply_role.bootstrap.children に宣言されていません", [r.address])
}

# --- PLAN-APPLY-03: 構成物の ARN ごとの Deny の形 ---

deny contains msg if {
	some arn in targets
	not covered(arn)
	msg := sprintf("[PLAN-APPLY-03] apply の role %q が、%s へのすべての Action（* か、読み取りの名前だけを除く NotAction）を条件なしの Deny で拒んでいません", [target, arn])
}

deny contains msg if {
	some arn in strict_targets
	not covered_strictly(arn)
	msg := sprintf("[PLAN-APPLY-03] apply の role %q が、%s へのすべての Action（*）を条件なしの Deny で拒んでいません", [target, arn])
}

# --- PLAN-APPLY-04: state の版の削除 ---

deny contains msg if {
	some arn in version_deletion_targets
	not version_deletion_denied(arn)
	msg := sprintf("[PLAN-APPLY-04] apply の role %q が、%s への s3:DeleteObjectVersion を条件なしの Deny で拒んでいません", [target, arn])
}

# --- PLAN-APPLY-05: apply の role に付く policy の排他 ---

deny contains msg if {
	some a in attached
	a.type in {"aws_iam_role_policy", "aws_iam_role_policy_attachment"}
	on_apply_role(a)
	a.address != target
	msg := sprintf("[PLAN-APPLY-05] %s: %s が apply の role に policy を付けています。apply の role の policy は %s だけに置いてください", [a.path, a.address, target])
}

deny contains msg if {
	some a in attached
	object.get(a.unknown, role_key(a), false) == true
	msg := sprintf("[PLAN-APPLY-05] %s: %s がどの role に付くかを plan で読めません", [a.path, a.address])
}

deny contains msg if {
	role_known
	some type in ["aws_iam_role_policies_exclusive", "aws_iam_role_policy_attachments_exclusive"]
	count(exclusive(type)) != 1
	msg := sprintf("[PLAN-APPLY-05] apply の role %s の %s が %d 件です（1件を期待）", [role_name, type, count(exclusive(type))])
}

deny contains msg if {
	some a in exclusive("aws_iam_role_policies_exclusive")
	object.get(a.after, "policy_names", null) != [object.get(after(target), "name", null)]
	msg := sprintf("[PLAN-APPLY-05] %s: %s の policy_names が %v ではありません", [a.path, a.address, [object.get(after(target), "name", null)]])
}

deny contains msg if {
	some a in exclusive("aws_iam_role_policy_attachments_exclusive")
	object.get(a.after, "policy_arns", null) != []
	msg := sprintf("[PLAN-APPLY-05] %s: %s の policy_arns が空ではありません", [a.path, a.address])
}

# role の引数で付ける policy は、上の型を通らない。
deny contains msg if {
	some c in lib.changes
	c.rc.address == role
	some attr in ["inline_policy", "managed_policy_arns"]
	not empty_known(c.rc.change, attr)
	msg := sprintf("[PLAN-APPLY-05] %s: apply の role %s の %s が空ではないか、plan で決まっていません", [c.path, role, attr])
}

empty_known(change, attr) if {
	not unknown_any(object.get(change.after_unknown, attr, false))
	object.get(change.after, attr, []) in {[], null}
}

# after_unknown は、値の一部だけが決まらないとき配列の要素に true を持つ。
unknown_any(x) if {
	walk(x, [_, v])
	v == true
}

deny contains msg if {
	role_known
	some c in lib.changes
	c.rc.type == "aws_iam_policy_attachment"
	role_name in lib.as_array(object.get(c.rc.change.after, "roles", []))
	msg := sprintf("[PLAN-APPLY-05] %s: %s が apply の role %s に managed policy を付けています", [c.path, c.rc.address, role_name])
}

deny contains msg if {
	some c in lib.changes
	c.rc.type == "aws_iam_policy_attachment"
	unknown_any(object.get(c.rc.change.after_unknown, "roles", false))
	msg := sprintf("[PLAN-APPLY-05] %s: %s がどの role に付くかを plan で読めません", [c.path, c.rc.address])
}
