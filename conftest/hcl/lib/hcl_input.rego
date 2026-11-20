# scripts/hcl-input の `-format syntax` の JSON を読むための共通部分（ADR-0501 決定5）。検査ではない。
#
# 入力は {"files": [{"path", "parsed"}], "blocks": [Block]} で、Block の入れ子の block（blocks）と属性
# （attributes）は深さを問わず平らに並び、parent（blocks の位置、直下は -1）で親を引ける。形は shape_errors が、
# 読んだものの数は count_errors が、各検査の入力の規則として落とす。
#
# 構成要素の中を読む関数は scope（{"block", "at"}。at は入れ子の block の位置で、-1 なら block 全体）を取る。
package lib.hcl_input

typed_kinds := {"resource", "data", "ephemeral", "action"}

is_tf(path) if endswith(path, ".tf")

is_tftest(path) if endswith(path, ".tftest.hcl")

# dir はファイルを宣言した module のディレクトリを返す。
dir(path) := concat("/", array.slice(parts, 0, count(parts) - 1)) if {
	parts := split(path, "/")
	count(parts) > 1
}

dir(path) := "." if not contains(path, "/")

# module_dir は、`.tftest.hcl` については、それが検査する module のディレクトリを返す。
module_dir(path) := "." if {
	is_tftest(path)
	dir(path) == "tests"
}

module_dir(path) := trim_suffix(dir(path), "/tests") if {
	is_tftest(path)
	dir(path) != "tests"
}

module_dir(path) := dir(path) if not is_tftest(path)

# --- 式 ---

is_literal(e) if e.kind == "literal"

# literal_string は、e が文字列のリテラルならその値を返す。
literal_string(e) := e.value if {
	is_literal(e)
	is_string(e.value)
}

# address_of は、段がすべて属性の traversal を `.` で結んだ address を返す。添字や splat を含めば undefined。
address_of(e) := concat(".", array.concat([e.root], [s.name | some s in e.steps])) if {
	e.kind == "traversal"
	every s in e.steps {
		s.kind == "attr"
	}
}

# --- scope ---

scope_of(b) := {"block": b, "at": -1}

# in_subtree は、block b の入れ子の block の位置 j が、位置 i の block の子孫かを返す。入れ子は深さ優先に
# 並ぶので、i の子孫は i の直後から、親が i より前を指す block の手前までに連なる。
in_subtree(b, i, j) if {
	j > i
	every k in numbers.range(i + 1, j) {
		b.blocks[k].parent >= i
	}
}

# in_scope は、parent（入れ子の block の位置か -1）が scope の中かを返す。
in_scope(s, _) if s.at == -1

in_scope(s, p) if {
	s.at != -1
	p == s.at
}

in_scope(s, p) if {
	s.at != -1
	in_subtree(s.block, s.at, p)
}

prefix_of(s) := "" if s.at == -1

prefix_of(s) := concat("", [s.block.blocks[s.at].path, "."]) if s.at != -1

# attrs_in は scope の中の属性を、scope からの相対の path とともに返す。
attrs_in(s) := [object.union(a, {"rel": substring(a.path, count(prefix_of(s)), -1)}) |
	some a in s.block.attributes
	in_scope(s, a.parent)
]

# nested_in は scope の中の入れ子の block を、scope からの相対の path とともに返す。scope の block 自身は含まない。
nested_in(s) := [object.union(n, {"index": j, "rel": substring(n.path, count(prefix_of(s)), -1)}) |
	some j, n in s.block.blocks
	j != s.at
	in_scope(s, j)
]

# direct_attr は scope の直下の名前 name の属性の式を返す。
direct_attr(s, name) := a.expr if {
	some a in s.block.attributes
	a.parent == s.at
	a.path == concat("", [prefix_of(s), name])
}

# values_at は、scope の中で、相対の path が dotted に一致する属性（expr と line を持つ）と入れ子の block
# （type と line を持つ）を返す。dynamic は hcl-input が畳んでいる。
values_at(s, dotted) := array.concat(
	[a | some a in attrs_in(s); a.rel == dotted],
	[n | some n in nested_in(s); n.rel == dotted],
)

# has_key_anywhere は、scope の任意の深さに、名前 key の属性か、種類かラベルが key の block か、属性の値の
# オブジェクト（式でもリテラルでも）のキー key が在るかを返す。provider の設定は block を属性の形でも書ける。
has_key_anywhere(s, key) if {
	some a in attrs_in(s)
	parts := split(a.rel, ".")
	parts[count(parts) - 1] == key
}

has_key_anywhere(s, key) if {
	some n in nested_in(s)
	n.type == key
}

has_key_anywhere(s, key) if {
	some n in nested_in(s)
	key in n.labels
}

has_key_anywhere(s, key) if {
	some a in attrs_in(s)
	walk(a.expr, [_, v])
	is_object(v)
	v.kind == "object"
	some it in v.items
	literal_string(it.key) == key
}

has_key_anywhere(s, key) if {
	some a in attrs_in(s)
	walk(a.expr, [_, v])
	is_object(v)
	v.kind == "literal"
	walk(v.value, [q, _])
	count(q) > 0
	q[count(q) - 1] == key
}

# unresolved_expr は、scope の任意の深さの属性の式が、中を構文として読めない式（条件・for・splat・演算子・式の
# 添字。kind が other）か、キーがリテラルでないオブジェクトを含むかを返す。その中にどのキーが在るかは決められない。
unresolved_expr(s) if {
	some a in attrs_in(s)
	walk(a.expr, [_, v])
	is_object(v)
	v.kind == "other"
	is_string(v.syntax)
}

unresolved_expr(s) if {
	some a in attrs_in(s)
	walk(a.expr, [_, v])
	is_object(v)
	v.kind == "object"
	some it in v.items
	k := it.key
	not literal_string(k)
}

# has_provisioner は、scope の直下に provisioner の block（dynamic を含む）が在るかを返す。
has_provisioner(s) if {
	some n in nested_in(s)
	n.parent == s.at
	n.type == "provisioner"
}

has_provisioner(s) if {
	some n in nested_in(s)
	n.parent == s.at
	n.dynamic
	n.labels == ["provisioner"]
}

# --- 構成要素 ---

# components は、型を持つ構成要素をすべて返す。check の中の data も含む。
components(doc) := {c |
	some b in doc.blocks
	is_tf(b.file)
	b.type in typed_kinds
	count(b.labels) == 2
	c := component(b.file, b.type, b.labels[0], b.labels[1], scope_of(b))
} | {c |
	some b in doc.blocks
	is_tf(b.file)
	b.type == "check"
	some j, n in b.blocks
	n.parent == -1
	n.type == "data"
	count(n.labels) == 2
	c := component(b.file, "data", n.labels[0], n.labels[1], {"block": b, "at": j})
}

component(path, kind, type, name, s) := {
	"id": sprintf("%s#%s.%s.%s", [dir(path), kind, type, name]),
	"key": sprintf("%s.%s", [kind, type]),
	"path": path,
	"line": line_of(s),
	"scope": s,
}

line_of(s) := s.block.line if s.at == -1

line_of(s) := s.block.blocks[s.at].line if s.at != -1

# removed_blocks は removed ブロックを返す。destroy の時点で走る provisioner を持てる。from が属性だけの
# traversal でなければ、id はそれと分かる名前にし、許可リストに当たらないまま検出に残す。
removed_blocks(doc) := {r |
	some b in doc.blocks
	is_tf(b.file)
	b.type == "removed"
	s := scope_of(b)
	r := {
		"id": sprintf("%s#removed.%s", [dir(b.file), removed_from(s)]),
		"path": b.file,
		"scope": s,
	}
}

removed_from(s) := address_of(direct_attr(s, "from"))

removed_from(s) := "<解けない from>" if not direct_attr(s, "from")

removed_from(s) := "<解けない from>" if {
	e := direct_attr(s, "from")
	not address_of(e)
}

# providers は、provider の設定をすべて返す。`.tftest.hcl` の provider はそのファイルを識別に使う。
providers(doc) := {p |
	some b in doc.blocks
	b.type == "provider"
	count(b.labels) == 1
	s := scope_of(b)
	p := {
		"id": provider_id(b.file, b.labels[0], s),
		"name": b.labels[0],
		"path": b.file,
		"module": module_dir(b.file),
		"scope": s,
	}
}

provider_id(path, name, s) := sprintf("%s#provider.%s.%s", [owner(path), name, alias_of(s)]) if direct_attr(s, "alias")

provider_id(path, name, s) := sprintf("%s#provider.%s", [owner(path), name]) if not direct_attr(s, "alias")

alias_of(s) := literal_string(direct_attr(s, "alias"))

alias_of(s) := "<解けない alias>" if {
	e := direct_attr(s, "alias")
	not literal_string(e)
}

owner(path) := path if is_tftest(path)

owner(path) := dir(path) if not is_tftest(path)

# required_sources は、module のディレクトリごとに、required_providers が宣言した source を返す。source を
# 書かない宣言は hashicorp/<名前> になる。source が文字列のリテラルでなければ null を返す。
required_sources(doc) := {[dir(b.file), name, required_source(name, a.expr)] |
	some b in doc.blocks
	is_tf(b.file)
	b.type == "terraform"
	some j, n in b.blocks
	n.parent == -1
	n.type == "required_providers"
	some a in b.attributes
	a.parent == j
	name := substring(a.path, count(n.path) + 1, -1)
}

# オブジェクトのリテラルの source。
required_source(_, e) := e.value.source if {
	is_literal(e)
	is_object(e.value)
	is_string(e.value.source)
}

# 参照を含むオブジェクトの source。
required_source(_, e) := source_item(e) if e.kind == "object"

# source の無い宣言（版の制約だけの文字列、source の無いオブジェクト、その他の式）。
required_source(name, e) := sprintf("hashicorp/%s", [name]) if {
	not literal_object_source(e)
	not object_has_source(e)
}

literal_object_source(e) if {
	is_literal(e)
	is_object(e.value)
	is_string(e.value.source)
}

object_has_source(e) if {
	e.kind == "object"
	some it in e.items
	literal_string(it.key) == "source"
}

source_item(e) := v if {
	some it in e.items
	literal_string(it.key) == "source"
	v := literal_string(it.value)
}

source_item(e) := null if {
	some it in e.items
	literal_string(it.key) == "source"
	v := it.value
	not literal_string(v)
}

# provider_source は provider の source address を返す。required_providers に無ければ hashicorp/<名前> とする。
# source がリテラルでなければ null。
provider_source(doc, module, name) := normalize_or_null(s) if {
	some [d, n, s] in required_sources(doc)
	d == module
	n == name
}

provider_source(doc, module, name) := normalize_source(sprintf("hashicorp/%s", [name])) if {
	not declared_source(doc, module, name)
}

declared_source(doc, module, name) if {
	some [d, n, _] in required_sources(doc)
	d == module
	n == name
}

normalize_or_null(s) := normalize_source(s) if is_string(s)

normalize_or_null(s) := null if s == null

normalize_source(s) := lower(sprintf("registry.terraform.io/%s", [s])) if count(split(s, "/")) == 2

normalize_source(s) := lower(s) if count(split(s, "/")) != 2

# --- module と backend ---

# module_calls は module の呼び出しを返す。source は文字列のリテラルの値で、それ以外の式なら null。
module_calls(doc) := {c |
	some b in doc.blocks
	is_tf(b.file)
	b.type == "module"
	count(b.labels) == 1
	c := {"path": b.file, "dir": dir(b.file), "name": b.labels[0], "source": source_value(scope_of(b))}
}

# source_value は module の source の、文字列に限らないリテラルの値を返す。書かないか、リテラルでなければ null。
source_value(s) := e.value if {
	e := direct_attr(s, "source")
	is_literal(e)
}

source_value(s) := null if not direct_attr(s, "source")

source_value(s) := null if {
	e := direct_attr(s, "source")
	not is_literal(e)
}

# module_count は path のファイルの module block の数を返す。
module_count(doc, path) := count([b | some b in doc.blocks; b.file == path; b.type == "module"])

relative_source(s) if {
	is_string(s)
	not contains(s, "${")
	startswith(s, "./")
}

relative_source(s) if {
	is_string(s)
	not contains(s, "${")
	startswith(s, "../")
}

# module_target は、ディレクトリ dir の module の source が指すディレクトリを返す。相対パスでないか、
# 走査の root より上へ出るなら undefined。
module_target(d, source) := clean(sprintf("%s/%s", [d, source])) if relative_source(source)

# clean は `/` 区切りのパスの `.` と `..` を字面で解く。root より上へ出るパスは undefined。
clean(path) := concat("/", kept) if {
	segs := split(path, "/")
	not escapes(segs)
	kept := [segs[i] |
		some i in numbers.range(0, count(segs) - 1)
		weight(segs[i]) == 1
		not cancelled(segs, i)
	]
}

escapes(segs) if {
	some j in numbers.range(0, count(segs) - 1)
	sum([weight(segs[k]) | some k in numbers.range(0, j)]) < 0
}

cancelled(segs, i) if {
	some j in numbers.range(i + 1, count(segs) - 1)
	j > i
	sum([weight(segs[k]) | some k in numbers.range(i + 1, j)]) < 0
}

weight(s) := 0 if s in {".", ""}

weight(s) := -1 if s == ".."

weight(s) := 1 if not s in {".", "", ".."}

# backends は terraform block の直下の backend を返す。attrs は backend の直下の属性の名前から式へ。
backends(doc) := {x |
	some b in doc.blocks
	is_tf(b.file)
	b.type == "terraform"
	some j, n in b.blocks
	n.parent == -1
	n.type == "backend"
	s := {"block": b, "at": j}
	x := {
		"path": b.file,
		"dir": dir(b.file),
		"at": [b.line, j],
		"type": first_label(n),
		"attrs": {a.rel: a.expr | some a in attrs_in(s); a.parent == j},
	}
}

first_label(n) := n.labels[0] if count(n.labels) > 0

first_label(n) := "" if count(n.labels) == 0

# cloud_blocks は、cloud block を直下に持つ terraform block を返す（空の cloud {} も含む）。
cloud_blocks(doc) := {x |
	some b in doc.blocks
	is_tf(b.file)
	b.type == "terraform"
	some n in b.blocks
	n.parent == -1
	n.type == "cloud"
	x := {"path": b.file, "dir": dir(b.file), "at": b.line}
}

# --- 入力の形と数（fail closed） ---

default parsed_listed := null

parsed_listed := data.hcl_parsed_files

default block_counts := null

block_counts := data.hcl_block_counts

# input_errors は、各検査の入力（input）の形の崩れと、独立の数との食い違いを返す。形が崩れていれば数は見ない。
input_errors := shape_errors(input) | count_errors(input, parsed_listed, block_counts) if shape_errors(input) == set()

input_errors := shape_errors(input) if shape_errors(input) != set()

default input_ok := false

input_ok if input_errors == set()

# shape_errors は入力の形の崩れを文字列の集合で返す。空でなければ、それを読む規則を評価しない。
shape_errors(doc) := {"入力がオブジェクトではありません"} if not is_object(doc)

shape_errors(doc) := errs if {
	is_object(doc)
	errs := top_errors(doc) | file_errors(doc) | block_errors(doc)
}

top_errors(doc) := {"files が配列ではありません" | not is_array(object.get(doc, "files", null))} | {"blocks が配列ではありません" |
	not is_array(object.get(doc, "blocks", null))
} | {sprintf("未知のキー %s があります", [k]) |
	some k, _ in doc
	not k in {"files", "blocks"}
}

file_errors(doc) := {sprintf("files[%d] が path（空でない文字列）と parsed（真偽値）だけを持ちません", [i]) |
	is_array(object.get(doc, "files", null))
	some i, f in doc.files
	not well_formed_file(f)
}

well_formed_file(f) if {
	is_object(f)
	{k | some k, _ in f} == {"path", "parsed"}
	non_empty_string(f.path)
	is_boolean(f.parsed)
}

block_errors(doc) := {sprintf("blocks[%d] の形が違います（%s）", [i, object.get(b, "file", "?")]) |
	is_array(object.get(doc, "blocks", null))
	some i, b in doc.blocks
	not well_formed_block(b)
}

well_formed_block(b) if {
	is_object(b)
	{k | some k, _ in b} == {"file", "line", "type", "labels", "blocks", "attributes"}
	non_empty_string(b.file)
	is_number(b.line)
	non_empty_string(b.type)
	string_array(b.labels)
	is_array(b.blocks)
	is_array(b.attributes)
	every j, n in b.blocks {
		well_formed_nested(n, j)
	}
	every a in b.attributes {
		well_formed_attr(a, count(b.blocks))
	}
}

well_formed_nested(n, j) if {
	is_object(n)
	{k | some k, _ in n} == {"path", "parent", "line", "type", "labels", "dynamic"}
	non_empty_string(n.path)
	is_number(n.parent)
	n.parent >= -1
	n.parent < j
	is_number(n.line)
	non_empty_string(n.type)
	string_array(n.labels)
	is_boolean(n.dynamic)
}

well_formed_attr(a, nblocks) if {
	is_object(a)
	{k | some k, _ in a} == {"path", "parent", "line", "expr", "refs"}
	non_empty_string(a.path)
	is_number(a.parent)
	a.parent >= -1
	a.parent < nblocks
	is_number(a.line)
	well_formed_expr(a.expr)
	is_array(a.refs)
}

# well_formed_expr は、規則が読む深さ（式と、その子と孫）の節の形を検査する。Rego は再帰の関数を持てない。
well_formed_expr(e) if {
	well_formed_node(e)
	every c in children(e) {
		well_formed_node(c)
		every g in children(c) {
			well_formed_node(g)
		}
	}
}

children(e) := object.get(e, "parts", []) if e.kind == "template"

children(e) := object.get(e, "args", []) if e.kind == "call"

children(e) := object.get(e, "items", []) if e.kind == "tuple"

children(e) := [x | some it in object.get(e, "items", []); some x in [object.get(it, "key", null), object.get(it, "value", null)]] if e.kind == "object"

children(e) := [] if e.kind in {"literal", "traversal", "other"}

node_keys := {
	"literal": {"kind", "value"},
	"template": {"kind", "parts"},
	"call": {"kind", "name", "args", "expand"},
	"traversal": {"kind", "root", "steps"},
	"tuple": {"kind", "items"},
	"object": {"kind", "items"},
	"other": {"kind", "syntax"},
}

well_formed_node(e) if {
	is_object(e)
	{k | some k, _ in e} == node_keys[e.kind]
	node_fields_ok(e)
}

node_fields_ok(e) if e.kind in {"literal", "other"}

node_fields_ok(e) if {
	e.kind == "template"
	is_array(e.parts)
}

node_fields_ok(e) if {
	e.kind == "call"
	non_empty_string(e.name)
	is_array(e.args)
	is_boolean(e.expand)
}

node_fields_ok(e) if {
	e.kind == "traversal"
	non_empty_string(e.root)
	is_array(e.steps)
	every st in e.steps {
		well_formed_step(st)
	}
}

node_fields_ok(e) if {
	e.kind == "tuple"
	is_array(e.items)
}

node_fields_ok(e) if {
	e.kind == "object"
	is_array(e.items)
	every it in e.items {
		is_object(it)
		{k | some k, _ in it} == {"key", "value"}
	}
}

well_formed_step(st) if st == {"kind": "index"}

well_formed_step(st) if st == {"kind": "splat"}

well_formed_step(st) if {
	is_object(st)
	{k | some k, _ in st} == {"kind", "name"}
	st.kind == "attr"
	non_empty_string(st.name)
}

string_array(xs) if {
	is_array(xs)
	every x in xs {
		is_string(x)
	}
}

non_empty_string(s) if {
	is_string(s)
	s != ""
}

# count_errors は、hcl-input の出力を、それとは独立の経路の数と突き合わせる（ADR-0702 決定15）。
# parsed_listed は find が `.tf`・`.tftest.hcl` で列挙した一覧、block_counts は行頭の正規表現で数えた
# ファイルごとのトップレベルの block の数。
count_errors(doc, parsed_listed, block_counts) := {"hcl_parsed_files（find の一覧）が配列ではありません" |
	not is_array(parsed_listed)
} | {"hcl_block_counts（行頭の数え）がオブジェクトではありません" |
	not is_object(block_counts)
} | {sprintf("%s を構文として読んでいません（find は列挙しています）", [p]) |
	is_array(parsed_listed)
	some p in parsed_listed
	not p in parsed_paths(doc)
} | {sprintf("%s を構文として読みましたが、find の一覧にありません", [p]) |
	is_array(parsed_listed)
	some p in parsed_paths(doc)
	not p in {q | some q in parsed_listed}
} | {sprintf("%s のトップレベルの block を %d 個読みましたが、行頭の数えは %v 個です", [p, n, object.get(block_counts, p, "数えていない")]) |
	is_object(block_counts)
	some p in parsed_paths(doc)
	n := count([b | some b in doc.blocks; b.file == p])
	n != object.get(block_counts, p, -1)
} | {sprintf("%s の block を読みましたが、構文として読んだファイルの一覧にありません", [f]) |
	some f in {b.file | some b in doc.blocks}
	not f in parsed_paths(doc)
} | {sprintf("files に %s が %d 回あります", [p, n]) |
	some p in {f.path | some f in doc.files}
	n := count([f | some f in doc.files; f.path == p])
	n > 1
}

parsed_paths(doc) := {f.path | some f in doc.files; f.parsed}
