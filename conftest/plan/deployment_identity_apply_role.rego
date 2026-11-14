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

classes := {"target", "key", "covered_by", "exclude"}

modules := object.get(bootstrap, "modules", [])

types := object.get(bootstrap, "types", {})

children := object.get(bootstrap, "children", {})

well_formed_type(t) if {
	is_object(t)
	t.class in {"target", "key"}
}

well_formed_type(t) if {
	is_object(t)
	t.class == "covered_by"
	is_string(t.parent)
	t.parent != ""
}

well_formed_type(t) if {
	is_object(t)
	t.class == "exclude"
	is_string(t.reason)
	trim_space(t.reason) != ""
}

well_formed_child(c) if {
	is_object(c)
	every s in object.get(c, "suffixes", []) {
		is_string(s)
		s != ""
	}
	is_boolean(object.get(c, "roots", false))
	is_boolean(object.get(c, "version_deletion", false))
}

well_formed_bootstrap if {
	is_array(modules)
	count(modules) > 0
	every m in modules {
		is_string(m)
		m != ""
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

child_arns(address, arn) := {sprintf("%s%s", [arn, s]) | some s in object.get(children[address], "suffixes", [])} | {sprintf("%s/%s", [arn, k]) |
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
	some arn in child_arns(r.address, r.change.after.arn)
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

denies_all_but_reads(s) if "*" in lib.as_array(object.get(s, "Action", []))

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

version_deletion_denied(arn) if {
	some s in statements
	unconditional_deny(s)
	on(s, arn)
	some a in lib.lower_all(object.get(s, "Action", []))
	a in {"s3:deleteobjectversion", "*"}
}

# Key Policy の key_id は plan で決まらないので、鍵と Key Policy は module の path で組にする。
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
	msg := "[PLAN-APPLY-00] data.policy_plan.deployment_identity.apply_role.bootstrap が、modules（空でない文字列の配列）/ types（class が target・key・covered_by（parent が必須）・exclude（reason が必須））/ children の形を満たしていません"
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

deny contains msg if {
	some r in resources
	class_of(r.type) == "covered_by"
	parent := types[r.type].parent
	count({p | some p in resources; p.type == parent; module_of(p) == module_of(r); class_of(p.type) in {"target", "key"}}) == 0
	msg := sprintf("[PLAN-APPLY-02] %s の親として宣言した型 %s の target か key が、同じ module の下にありません", [r.address, parent])
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
