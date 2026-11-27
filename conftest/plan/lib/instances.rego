# state-backend と deployment-identity の instance。宣言の address は正規の instance（state_backend.module・
# deployment_identity.module の下）の形で1回だけ持ち、ほかの instance は prefix の組だけを宣言する
# （decl.json の state_backend.instances。key は root の名前）。deny を持たず、形の誤りは使う package が 00 で報告する。
package plan.lib

instance_sb_cfg := object.get(data.policy_plan, "state_backend", {})

instance_di_cfg := object.get(data.policy_plan, "deployment_identity", {})

module_prefix(p) if {
	is_string(p)
	regex.match(`^(module\.[a-z0-9_]+\.)+$`, p)
}

sb_module := m if {
	m := object.get(instance_sb_cfg, "module", null)
	module_prefix(m)
}

di_module := m if {
	m := object.get(instance_di_cfg, "module", null)
	module_prefix(m)
}

overlapping(a, b) if startswith(a, b)

overlapping(a, b) if startswith(b, a)

bootstrap_modules := object.get(instance_di_cfg, ["apply_role", "bootstrap", "modules"], [])

raw_instances := object.get(instance_sb_cfg, "instances", {})

instance_keys := {"name", "state_backend", "deployment_identity", "deployment_identity_root", "creating"}

well_formed_instance(e) if {
	is_object(e)
	every k, _ in e {
		k in instance_keys
	}
	is_string(e.name)
	e.name != ""
	module_prefix(e.state_backend)
	module_prefix(e.deployment_identity)
	di_root_shape(e)
	creating_shape(e)
}

di_root_shape(e) if not "deployment_identity_root" in object.keys(e)

di_root_shape(e) if {
	is_string(e.deployment_identity_root)
	e.deployment_identity_root != ""
}

creating_shape(e) if not "creating" in object.keys(e)

creating_shape(e) if {
	is_object(e.creating)
	object.keys(e.creating) == {"reason"}
	is_string(e.creating.reason)
	trim_space(e.creating.reason) != ""
}

instance_shape_errors contains "data.policy_plan.state_backend.module が、module の address の接頭辞（module.<名前>. の並び）ではありません" if not sb_module

instance_shape_errors contains "data.policy_plan.deployment_identity.module が、module の address の接頭辞（module.<名前>. の並び）ではありません" if not di_module

instance_shape_errors contains msg if {
	overlapping(sb_module, di_module)
	msg := sprintf("data.policy_plan.state_backend.module %q と deployment_identity.module %q が重なっています（同じか、一方が他方の接頭辞です）", [sb_module, di_module])
}

instance_shape_errors contains msg if {
	not sb_module in bootstrap_modules
	msg := sprintf("data.policy_plan.state_backend.module %q が deployment_identity.apply_role.bootstrap.modules にありません", [sb_module])
}

instance_shape_errors contains msg if {
	not di_module in bootstrap_modules
	msg := sprintf("data.policy_plan.deployment_identity.module %q が deployment_identity.apply_role.bootstrap.modules にありません", [di_module])
}

instance_shape_errors contains "data.policy_plan.state_backend.instances が object ではありません" if not is_object(raw_instances)

instance_shape_errors contains "data.policy_plan.state_backend.instances が空の object です。instance が無いなら、キーごと外してください" if {
	is_object(raw_instances)
	count(raw_instances) == 0
	"instances" in object.keys(instance_sb_cfg)
}

instance_shape_errors contains msg if {
	is_object(raw_instances)
	some root, list in raw_instances
	not non_empty_array(list)
	msg := sprintf("data.policy_plan.state_backend.instances[%q] が空でない配列ではありません", [root])
}

instance_shape_errors contains msg if {
	is_object(raw_instances)
	some root, list in raw_instances
	is_array(list)
	some e in list
	not well_formed_instance(e)
	msg := sprintf("data.policy_plan.state_backend.instances[%q] の要素 %v が、name と state_backend・deployment_identity（module の address の接頭辞）の文字列と、任意の deployment_identity_root（空でない文字列）と creating（空でない reason の文字列だけを持つ object）だけを持っていません", [root, e])
}

instance_shape_errors contains msg if {
	is_object(raw_instances)
	some root, _ in raw_instances
	ps := root_prefixes(root)
	some i, a in ps
	some j, b in ps
	i < j
	overlapping(a.prefix, b.prefix)
	msg := sprintf("data.policy_plan.state_backend.instances[%q] の %s %q と %s %q が重なっています（同じか、一方が他方の接頭辞です）", [root, a.label, a.prefix, b.label, b.prefix])
}

instance_shape_errors contains msg if {
	is_object(raw_instances)
	some root, list in raw_instances
	is_array(list)
	names := [e.name | some e in list; well_formed_instance(e)]
	count({n | some n in names}) != count(names)
	msg := sprintf("data.policy_plan.state_backend.instances[%q] の name %v が重なっています", [root, names])
}

non_empty_array(x) if {
	is_array(x)
	count(x) > 0
}

root_prefixes(root) := array.concat(
	[{"label": "state_backend.module", "prefix": sb_module}, {"label": "deployment_identity.module", "prefix": di_module}],
	[x |
		some e in raw_instances[root]
		well_formed_instance(e)
		some kind in ["state_backend", "deployment_identity"]
		x := {"label": sprintf("%s の %s", [e.name, kind]), "prefix": e[kind]}
	],
) if is_array(raw_instances[root])

# deployment_identity_root は、deployment-identity の側が別の root に在る instance（reference）の、その root の名前。
declared_members contains {"name": e.name, "root": root, "deployment_identity_root": object.get(e, "deployment_identity_root", root), "state_backend": e.state_backend, "deployment_identity": e.deployment_identity, "creating": object.get(e, "creating", null)} if {
	is_object(raw_instances)
	some root, list in raw_instances
	is_array(list)
	some e in list
	well_formed_instance(e)
}

# CI は root ごとに、root の名前のファイルで渡す。
root_name(path) := trim_suffix(regex.replace(path, `^.*[/\\]`, ""), ".json")

input_roots := {root_name(f.path) | some f in input}

# 正規の instance は root を持たず、結合した入力のどこかに在ればよい。宣言した instance は、その root のファイルが
# 入力に在るときだけ数え、そのファイルの中に在ることを求める。
instances contains {"name": "canonical", "root": null, "deployment_identity_root": null, "state_backend": sb_module, "deployment_identity": di_module, "creating": null}

instances contains m if {
	some m in declared_members
	m.root in input_roots
	m.deployment_identity_root in input_roots
}

instance_roots(inst) := {inst.root, inst.deployment_identity_root}

in_root(_, inst) if inst.root == null

in_root(path, inst) if {
	is_string(inst.root)
	root_name(path) in instance_roots(inst)
}

instance_tag(inst) := "" if inst.root == null

instance_tag(inst) := sprintf("%s の instance %s: ", [inst.root, inst.name]) if is_string(inst.root)

rebasable(addr) if {
	is_string(addr)
	startswith(addr, sb_module)
}

rebasable(addr) if {
	is_string(addr)
	startswith(addr, di_module)
}

rebase(addr, inst) := concat("", [inst.state_backend, substring(addr, count(sb_module), -1)]) if {
	is_string(addr)
	startswith(addr, sb_module)
}

rebase(addr, inst) := concat("", [inst.deployment_identity, substring(addr, count(di_module), -1)]) if {
	is_string(addr)
	not startswith(addr, sb_module)
	startswith(addr, di_module)
}

present(addr, inst) if {
	some f in input
	in_root(f.path, inst)
	some rc in f.contents.resource_changes
	rc.address == addr
}

# 宣言の外の instance を見つける印。正規の宣言の address から正規の接頭辞を除いて導くので、usecase の module の
# address が変われば、印も宣言と一緒に変わる（宣言が入力に無いことは各 package の 00 が落とす）。印の前の接頭辞が、
# 正規の接頭辞でも、そのファイルの root に宣言した接頭辞でもなければ数える。
marker_sources := {
	"state_backend": {"path": "deployment_identity.apply_identity_role.output_bucket", "address": object.get(instance_di_cfg, ["apply_identity_role", "output_bucket"], null), "prefix": sb_module},
	"deployment_identity": {"path": "deployment_identity.apply_identity_role.role", "address": object.get(instance_di_cfg, ["apply_identity_role", "role"], null), "prefix": di_module},
}

instance_markers[kind] := substring(m.address, count(m.prefix), -1) if {
	some kind, m in marker_sources
	is_string(m.address)
	startswith(m.address, m.prefix)
	count(m.address) > count(m.prefix)
}

instance_shape_errors contains msg if {
	some kind, m in marker_sources
	not instance_markers[kind]
	msg := sprintf("data.policy_plan.%s が %s.module で始まる address ではありません。宣言に無い instance を見つける印を導けません", [m.path, kind])
}

# 書かない変更（削除と管理からの除外）は instance を残さない。立ち上げを閉じる PR の removed の forget を数えない。
undeclared_instances contains {"path": f.path, "kind": kind, "prefix": prefix} if {
	some f in input
	some rc in f.contents.resource_changes
	is_string(rc.address)
	object.get(rc, "mode", "managed") == "managed"
	is_object(object.get(object.get(rc, "change", {}), "after", null))
	some kind, marker in instance_markers
	endswith(rc.address, marker)
	prefix := substring(rc.address, 0, count(rc.address) - count(marker))
	boundary(prefix)
	not known_prefix(kind, prefix, root_name(f.path))
}

# 印に当たるのに change を object で持たない要素は、書くかどうかを読めない。黙って数えずに済ませないよう、使う package が 00 で落とす。
malformed_marker_entries contains {"path": f.path, "address": rc.address} if {
	some f in input
	some rc in f.contents.resource_changes
	is_string(rc.address)
	some _, marker in instance_markers
	endswith(rc.address, marker)
	not is_object(object.get(rc, "change", null))
}

boundary("")

boundary(prefix) if endswith(prefix, ".")

known_prefix("state_backend", prefix, _) if prefix == sb_module

known_prefix("deployment_identity", prefix, _) if prefix == di_module

known_prefix("state_backend", prefix, root) if {
	some m in declared_members
	m.root == root
	m.state_backend == prefix
}

known_prefix("deployment_identity", prefix, root) if {
	some m in declared_members
	m.deployment_identity_root == root
	m.deployment_identity == prefix
}

# 作成の扱い（creating）は、その instance の state の鍵を作る plan でだけ効く。
key_created(inst) if {
	some f in input
	in_root(f.path, inst)
	some rc in f.contents.resource_changes
	rc.address == rebase(instance_sb_cfg.state_key, inst)
	rc.change.actions == ["create"]
}

creating(inst) if {
	is_object(inst.creating)
	key_created(inst)
}

creating_unused(inst) if {
	is_object(inst.creating)
	not key_created(inst)
}
