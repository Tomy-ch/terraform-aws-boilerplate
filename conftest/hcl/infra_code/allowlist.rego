# 許可リストの項目のうち、HCL のソースから判定できる部分（ADR-0103 決定15）。所在の実在、symlink、
# 参照する ADR は infra-code-gate decl が見る。
package infra_code

import data.lib.hcl

file_functions := `^\$\{(?:file|filebase64|filesha256|filebase64sha256|filemd5|templatefile)\("([^"]*)"(?:,[^)]*)?\)\}$`

deny contains msg if {
	some item in decl.allowlist
	not item.id in detected_ids
	msg := sprintf("IC020 [当たらない項目] 許可リストの項目 %s は、どのインフラ内コードにも当たりません → 項目を消す", [item.id])
}

# refs は、許可リストに載ったコンポーネントが引数に書いた値と、その値の種類。
refs contains {"item": item, "component": c, "arg": arg, "value": v} if {
	some c in components
	some item in decl.allowlist
	item.id == c.id
	some arg in array.concat(object.get(decl.types[c.key], "path_args", []), object.get(decl.types[c.key], "body_args", []))
	some v in hcl.values_at(c.body, arg)
}

# path_of は、値が静的に確定するリポジトリ内のパスなら、それを返す。
path_of(ref) := p if {
	ref.arg in object.get(decl.types[ref.component.key], "path_args", [])
	p := static_path(ref.value, hcl.dir(ref.component.path))
}

path_of(ref) := p if {
	m := regex.find_all_string_submatch_n(file_functions, ref.value, 1)
	count(m) == 1
	p := static_path(m[0][1], hcl.dir(ref.component.path))
}

# static_path は、リテラルか、先頭だけが `${path.module}` のテンプレートを、宣言した module を基準に解く。
static_path(raw, dir) := hcl.clean(concat("/", [dir, raw])) if {
	not contains(raw, "${")
	not startswith(raw, "/")
}

static_path(raw, dir) := hcl.clean(concat("/", [dir, rest])) if {
	startswith(raw, "${path.module}/")
	rest := substring(raw, count("${path.module}/"), -1)
	not contains(rest, "${")
}

is_path(ref) if ref.arg in object.get(decl.types[ref.component.key], "path_args", [])

is_path(ref) if regex.match(file_functions, ref.value)

deny contains msg if {
	some ref in refs
	is_string(ref.value)
	is_path(ref)
	not path_of(ref)
	msg := sprintf("IC030 [path] %s: %s の %s は、静的に確定するリポジトリ内のパスではありません（%s）", [ref.component.path, ref.component.id, ref.arg, ref.value])
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
	is_string(ref.value)
	ref.item.owner == "repository"
	not is_path(ref)
	not contains(ref.value, "${")
	ref.item.source != ref.component.path
	msg := sprintf("IC033 [path] %s: %s の %s はインラインのコードなので、ソースの所在はこのファイル %s にする", [ref.component.path, ref.component.id, ref.arg, ref.component.path])
}

deny contains msg if {
	some ref in refs
	is_string(ref.value)
	not is_path(ref)
	contains(ref.value, "${")
	msg := sprintf("IC030 [path] %s: %s の %s は、静的に確定するリポジトリ内のパスでもインラインのコードでもありません（%s）", [ref.component.path, ref.component.id, ref.arg, ref.value])
}

# リストで渡す値（`external` の `program` 等）は、どの要素がコードかを決められないので、path を検査できない。
deny contains msg if {
	some ref in refs
	is_array(ref.value)
	msg := sprintf("IC034 [path] %s: %s の %s はリストなので path を検査できず、許可リストに載せられません", [ref.component.path, ref.component.id, ref.arg])
}

deny contains msg if {
	some ref in refs
	not is_string(ref.value)
	not is_array(ref.value)
	msg := sprintf("IC030 [path] %s: %s の %s は、path でもインラインのコードでもありません（%v）", [ref.component.path, ref.component.id, ref.arg, ref.value])
}
