# resource の作成の順序の検査。宣言した module の resource が、depends_on に宣言した resource を持つこと。
#
# 走査の root の下の全ファイルを1つの入力として掛ける（--combine）。make が Conftest とは別の経路で数えた
# 構成ファイルの一覧（data.resource_order_files）と、Conftest が読んだファイルを突き合わせる（ADR-0702 決定15）。
package resource_order

import data.lib.hcl

default rules := []

rules := data.resource_order_decl.rules if is_array(data.resource_order_decl.rules)

default listed := set()

listed := {p | some p in data.resource_order_files} if is_array(data.resource_order_files)

# JSON の構成は検査が解釈しないので、数えたうえで落とす（ADR-0501 決定5、ADR-0103 決定11）。
is_tf_json(path) if endswith(path, ".tf.json")

read := {f.path | some f in input; is_config(f.path)}

is_config(path) if hcl.is_tf(path)

is_config(path) if is_tf_json(path)

rule_keys := {"module", "resource", "after", "adr"}

well_formed(r) if {
	is_object(r)
	{k | some k, _ in r} == rule_keys
	every k in rule_keys {
		is_string(r[k])
		r[k] != ""
	}
	count(split(r.resource, ".")) == 2
	count(split(r.after, ".")) == 2
}

deny contains msg if {
	count(rules) == 0
	msg := "RO001 [宣言の形] data.resource_order_decl.rules が空か、配列ではありません"
}

deny contains msg if {
	some i, r in rules
	not well_formed(r)
	msg := sprintf("RO001 [宣言の形] rules[%d] が module / resource / after / adr の4つの空でない文字列を持ちません（resource と after は <型>.<名前>）", [i])
}

deny contains msg if {
	count(listed) == 0
	msg := "RO002 [対象が0件] 走査の root に Terraform の構成ファイル（.tf と .tf.json）が1件もありません"
}

deny contains msg if {
	some p in listed
	not p in read
	msg := sprintf("RO002 [件数の不一致] %s を Conftest が読んでいません", [p])
}

deny contains msg if {
	some p in read
	not p in listed
	msg := sprintf("RO002 [件数の不一致] %s は独立の走査の一覧にありません", [p])
}

deny contains msg if {
	some p in read
	is_tf_json(p)
	msg := sprintf("RO006 [未対応の入力] %s は検査が解釈しない JSON の構成です → HCL（.tf）で書く", [p])
}

module_files(module) := {p | some p in read; hcl.dir(p) == module}

deny contains msg if {
	some r in rules
	well_formed(r)
	count(module_files(r.module)) == 0
	msg := sprintf("RO003 [module が無い] %s に構成ファイルがありません。module を動かしたなら resource_order の宣言を直してください", [r.module])
}

declared(module, address) := [c |
	some c in hcl.components(input)
	c.id == sprintf("%s#resource.%s", [module, address])
]

deny contains msg if {
	some r in rules
	well_formed(r)
	count(module_files(r.module)) > 0
	some address in [r.resource, r.after]
	n := count(declared(r.module, address))
	n != 1
	msg := sprintf("RO004 [resource が無い] %s の resource %s が %d 件です（1件を期待）。address を変えたなら resource_order の宣言を直してください", [r.module, address, n])
}

depends_on(body) := {trim_suffix(trim_prefix(d, "${"), "}") |
	some d in object.get(body, "depends_on", [])
	is_string(d)
}

deny contains msg if {
	some r in rules
	well_formed(r)
	some c in declared(r.module, r.resource)
	not r.after in depends_on(c.body)
	msg := sprintf("RO005 [順序] %s の %s が depends_on に %s を持ちません（%s）", [c.path, r.resource, r.after, r.adr])
}
