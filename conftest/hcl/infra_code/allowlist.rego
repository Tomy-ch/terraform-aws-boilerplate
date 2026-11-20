# 許可リストの項目のうち、HCL のソースから判定できる部分（ADR-0103 決定15）。所在の実在、symlink、
# 参照する ADR は infra-code-gate decl が見る。
package infra_code

import data.lib.hcl_input as hcl

# 第1引数のファイルを読む関数。どれが path を取るかは規則の知識なので、ここに置く。
file_functions := {"file", "filebase64", "filesha256", "filebase64sha256", "filemd5", "templatefile"}

deny contains msg if {
	hcl.input_ok
	some item in decl.allowlist
	not item.id in detected_ids
	msg := sprintf("IC020 [当たらない項目] 許可リストの項目 %s は、どのインフラ内コードにも当たりません → 項目を消す", [item.id])
}

# refs は、許可リストに載ったコンポーネントが引数に書いた値（属性か入れ子の block）と、その値の種類。
refs contains {"item": item, "component": c, "arg": arg, "value": v} if {
	some c in components
	some item in decl.allowlist
	item.id == c.id
	some arg in array.concat(object.get(decl.types[c.key], "path_args", []), object.get(decl.types[c.key], "body_args", []))
	some v in hcl.values_at(c.scope, arg)
}

# category は値を4つに分ける（conftest/hcl/infra_code/README.md の「値の読み方」）。
category(v) := "literal_string" if hcl.literal_string(v.expr)

category(v) := "expression" if v.expr.kind in {"template", "call", "traversal", "other"}

category(v) := "list" if v.expr.kind == "tuple"

category(v) := "list" if {
	hcl.is_literal(v.expr)
	is_array(v.expr.value)
}

category(v) := "list" if not v.expr

category(v) := "other" if v.expr.kind == "object"

category(v) := "other" if {
	hcl.is_literal(v.expr)
	not is_string(v.expr.value)
	not is_array(v.expr.value)
}

# file_call は、値が file 系の関数の呼び出しで、第1引数が文字列（補間を含んでよい）かを返す。
file_call(e) if {
	e.kind == "call"
	e.name in file_functions
	count(e.args) > 0
	e.args[0].kind in {"literal", "template"}
}

# path_of は、値が静的に確定するリポジトリ内のパスなら、それを返す。
path_of(ref) := p if {
	ref.arg in object.get(decl.types[ref.component.key], "path_args", [])
	p := static_path(ref.value.expr, hcl.dir(ref.component.path))
}

path_of(ref) := p if {
	file_call(ref.value.expr)
	p := static_path(ref.value.expr.args[0], hcl.dir(ref.component.path))
}

# static_path は、文字列のリテラルか、先頭だけが path.module のテンプレートを、宣言した module を基準に解く。
static_path(e, dir) := hcl.clean(concat("/", [dir, raw])) if {
	raw := hcl.literal_string(e)
	not startswith(raw, "/")
}

static_path(e, dir) := hcl.clean(concat("/", [dir, substring(rest, 1, -1)])) if {
	e.kind == "template"
	count(e.parts) > 1
	e.parts[0] == {"kind": "traversal", "root": "path", "steps": [{"kind": "attr", "name": "module"}]}
	tail := array.slice(e.parts, 1, count(e.parts))
	every part in tail {
		hcl.literal_string(part)
	}
	rest := concat("", [hcl.literal_string(part) | some part in tail])
	startswith(rest, "/")
}

is_path(ref) if ref.arg in object.get(decl.types[ref.component.key], "path_args", [])

is_path(ref) if file_call(ref.value.expr)

deny contains msg if {
	some ref in refs
	category(ref.value) in {"literal_string", "expression"}
	is_path(ref)
	not path_of(ref)
	msg := sprintf("IC030 [path] %s: %s の %s は、静的に確定するリポジトリ内のパスではありません（%d 行目）", [ref.component.path, ref.component.id, ref.arg, ref.value.line])
}

deny contains msg if {
	some ref in refs
	ref.item.owner == "repository"
	p := path_of(ref)
	p != ref.item.source
	not startswith(p, concat("", [ref.item.source, "/"]))
	msg := sprintf("IC031 [path] %s: %s の %s が指す %s は、ソースの所在 %s の下にありません", [ref.component.path, ref.component.id, ref.arg, p, ref.item.source])
}

deny contains msg if {
	some ref in refs
	ref.item.owner == "external"
	path_of(ref)
	msg := sprintf("IC032 [path] %s: 外部所有の項目 %s の %s がリポジトリ内のパスを指しています", [ref.component.path, ref.component.id, ref.arg])
}

deny contains msg if {
	some ref in refs
	category(ref.value) == "literal_string"
	ref.item.owner == "repository"
	not is_path(ref)
	ref.item.source != ref.component.path
	msg := sprintf("IC033 [path] %s: %s の %s はインラインのコードなので、ソースの所在はこのファイル %s にする", [ref.component.path, ref.component.id, ref.arg, ref.component.path])
}

deny contains msg if {
	some ref in refs
	category(ref.value) == "expression"
	not is_path(ref)
	msg := sprintf("IC030 [path] %s: %s の %s は、静的に確定するリポジトリ内のパスでもインラインのコードでもありません（%d 行目）", [ref.component.path, ref.component.id, ref.arg, ref.value.line])
}

# リストで渡す値（`external` の `program` 等）は、どの要素がコードかを決められないので、path を検査できない。
deny contains msg if {
	some ref in refs
	category(ref.value) == "list"
	msg := sprintf("IC034 [path] %s: %s の %s はリストなので path を検査できず、許可リストに載せられません", [ref.component.path, ref.component.id, ref.arg])
}

deny contains msg if {
	some ref in refs
	category(ref.value) == "other"
	msg := sprintf("IC030 [path] %s: %s の %s は、path でもインラインのコードでもありません（%d 行目）", [ref.component.path, ref.component.id, ref.arg, ref.value.line])
}
