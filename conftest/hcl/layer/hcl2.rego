# 層の検査が、Conftest の hcl2 パーサが返す形のまま HCL のソースを読むための共通部分。
# path だけの補助は lib.hcl_input が正で、ここはそれへ委ねる。
#
# パーサは、ラベルを持つブロックをラベルごとの入れ子のオブジェクトに、本体を配列にして返す
# （`resource "a" "b" {}` は `resource.a.b[0]`）。式は `${...}` の文字列になる。
package lib.hcl

import data.lib.hcl_input

typed_kinds := hcl_input.typed_kinds

is_tf(path) if hcl_input.is_tf(path)

is_tftest(path) if hcl_input.is_tftest(path)

dir(path) := hcl_input.dir(path)

module_dir(path) := hcl_input.module_dir(path)

# components は、型を持つ構成要素をすべて返す。check の中の data も含む。
components(files) := {c |
	some f in files
	is_tf(f.path)
	some kind in typed_kinds
	some type, names in object.get(f.contents, kind, {})
	some name, bodies in names
	some body in bodies
	c := component(f.path, kind, type, name, body)
} | {c |
	some f in files
	is_tf(f.path)
	some _, checks in object.get(f.contents, "check", {})
	some chk in checks
	some type, names in object.get(chk, "data", {})
	some name, bodies in names
	some body in bodies
	c := component(f.path, "data", type, name, body)
}

component(path, kind, type, name, body) := {
	"id": sprintf("%s#%s.%s.%s", [dir(path), kind, type, name]),
	"key": sprintf("%s.%s", [kind, type]),
	"path": path,
	"body": body,
}

# removed_blocks は removed ブロックを返す。destroy の時点で走る provisioner を持てる。
removed_blocks(files) := {r |
	some f in files
	is_tf(f.path)
	some body in object.get(f.contents, "removed", [])
	r := {
		"id": sprintf("%s#removed.%s", [dir(f.path), trim_suffix(trim_prefix(body.from, "${"), "}")]),
		"path": f.path,
		"body": body,
	}
}

has_provisioner(body) if body.provisioner

has_provisioner(body) if body.dynamic.provisioner

# providers は、provider の設定をすべて返す。`.tftest.hcl` の provider はそのファイルを識別に使う。
providers(files) := {p |
	some f in files
	some name, bodies in object.get(f.contents, "provider", {})
	some body in bodies
	p := {
		"id": provider_id(f.path, name, body),
		"name": name,
		"path": f.path,
		"module": module_dir(f.path),
		"body": body,
	}
}

provider_id(path, name, body) := sprintf("%s#provider.%s.%s", [owner(path), name, body.alias]) if body.alias

provider_id(path, name, body) := sprintf("%s#provider.%s", [owner(path), name]) if not body.alias

owner(path) := path if is_tftest(path)

owner(path) := dir(path) if not is_tftest(path)

# required_sources は、module のディレクトリごとに、required_providers が宣言した source を返す。
# source を書かない宣言（文字列の版の制約だけ、または source の無いオブジェクト）は hashicorp/<名前> になる。
required_sources(files) := {[dir(f.path), name, required_source(name, entry)] |
	some f in files
	is_tf(f.path)
	some t in object.get(f.contents, "terraform", [])
	some rp in object.get(t, "required_providers", [])
	some name, entry in rp
}

required_source(_, entry) := entry.source if {
	is_object(entry)
	entry.source
}

required_source(name, entry) := sprintf("hashicorp/%s", [name]) if not entry.source

required_source(name, entry) := sprintf("hashicorp/%s", [name]) if is_string(entry)

# provider_source は provider の source address を返す。required_providers に無ければ hashicorp/<名前> とする。
provider_source(files, module, name) := normalize_source(s) if {
	some [d, n, s] in required_sources(files)
	d == module
	n == name
}

provider_source(files, module, name) := normalize_source(sprintf("hashicorp/%s", [name])) if {
	not declared_source(files, module, name)
}

declared_source(files, module, name) if {
	some [d, n, _] in required_sources(files)
	d == module
	n == name
}

normalize_source(s) := hcl_input.normalize_source(s)

# values_at は、body の中で、ブロックをまたいだキーの並びが dotted に一致する値をすべて返す。リストの値は
# リストのまま返し、その要素を別の値として返さない。
values_at(body, dotted) := [v |
	walk(body, [p, v])
	count(p) > 0
	is_string(p[count(p) - 1])
	keys := [k | some k in p; is_string(k)]
	fold_dynamic(concat(".", keys)) == dotted
]

# fold_dynamic は `dynamic.<名前>.content` を `<名前>` に畳む。dynamic で書いたブロックも、同じ引数として照合する。
fold_dynamic(dotted) := regex.replace(regex.replace(dotted, `(^|\.)dynamic\.([^.]+)\.content(\.|$)`, "${1}${2}${3}"), `(^|\.)dynamic\.([^.]+)\.content(\.|$)`, "${1}${2}${3}")

# has_key_anywhere は、body の任意の深さに key のブロックか引数が在るかを返す（dynamic の中を含む）。
has_key_anywhere(body, key) if {
	walk(body, [p, _])
	count(p) > 0
	p[count(p) - 1] == key
}

relative_source(s) if hcl_input.relative_source(s)

module_target(d, source) := hcl_input.module_target(d, source)

clean(path) := hcl_input.clean(path)
