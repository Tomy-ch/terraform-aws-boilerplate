# 層の規則の検査（ADR-0102 決定14・16・17・18、ADR-0301 決定21 と、その検証方法）。
#
# 入力は HCL のソースから作る JSON で、その形を読むのは lib.hcl_input だけである。
# 形が崩れているか独立の数と食い違えば LY004 だけで落とし、それを読む規則は評価しない（hcl.doc が undefined）。
package layer

import data.lib.hcl_input as hcl

default decl := {}

decl := data.layer_decl if is_object(data.layer_decl)

attachments := object.get(decl, "attachments", null)

not_attachments := object.get(decl, "not_attachments", null)

singletons := object.get(decl, "singletons", null)

default listed := set()

listed := {p | some p in data.layer_files} if is_array(data.layer_files)

read := hcl.config_paths(hcl.doc)

dirs := {hcl.dir(p) | some p in hcl.tf_paths(hcl.doc)}

layer(d) := "env" if regex.match(`^env(/[^/]+)+$`, d)

layer(d) := "usecase" if regex.match(`^usecases/[^/]+$`, d)

layer(d) := "internal" if regex.match(`^usecases/[^/]+/internal/[^/]+$`, d)

layer(d) := "reference" if regex.match(`^usecases/[^/]+/reference$`, d)

layer(d) := "module" if regex.match(`^modules/[^/]+$`, d)

usecase_of(d) := split(d, "/")[1]

# --- 宣言の形 ---

well_formed_parents(ps) if {
	is_object(ps)
	count(ps) > 0
	every arg, types in ps {
		is_string(arg)
		arg != ""
		is_array(types)
		count(types) > 0
		every t in types {
			is_string(t)
			t != ""
		}
	}
}

well_formed_attachment(a) if {
	is_object(a)
	{k | some k, _ in a} == {"parents", "optional", "parent_arguments", "whole_document"}
	is_boolean(a.optional)
	well_formed_parents(a.parents)
	is_boolean(a.whole_document)
	well_formed_parent_arguments(a)
}

well_formed_parent_arguments(a) if {
	is_object(a.parent_arguments)
	every t, args in a.parent_arguments {
		t in parent_types_of(a)
		t != "*"
		is_object(args)
		count(args) > 0
		every arg, v in args {
			non_empty_string(arg)
			unwritten_value(v)
		}
	}
}

unwritten_value(null)

unwritten_value(v) if v == []

parent_types_of(a) := {t | some _, ts in a.parents; some t in ts}

well_formed_reasons(x) if {
	is_object(x)
	every t, args in x {
		non_empty_string(t)
		is_object(args)
		count(args) > 0
		every arg, reason in args {
			non_empty_string(arg)
			non_empty_string(reason)
		}
	}
}

# Policy Test は、保護の対象の型の分をここから導く（ADR-0402 決定17）。
well_formed_unrelated(x) if {
	is_object(x)
	every t, args in x {
		non_empty_string(t)
		is_object(args)
		count(args) > 0
		every arg, e in args {
			non_empty_string(arg)
			is_object(e)
			{k | some k, _ in e} == {"reason", "unwritten"}
			non_empty_string(e.reason)
			unwritten_value(e.unwritten)
		}
	}
}

unrelated_deprecated := object.get(decl, "unrelated_deprecated_arguments", null)

inline_owned := object.get(decl, "inline_owned", null)

declared_parent_types := {t | some _, a in attachments; some t in parent_types_of(a); t != "*"} if is_object(attachments)

# parent_argument_decl は、attachment 型と同じ設定を書くと宣言した（親の型, 引数, それを宣言した attachment 型）の組。
parent_argument_decl := {[t, arg, at] |
	some at, a in attachments
	is_object(a.parent_arguments)
	some t, args in a.parent_arguments
	is_object(args)
	some arg, _ in args
} if is_object(attachments)

parent_argument_pairs := {[t, arg] | some [t, arg, _] in parent_argument_decl}

deny contains msg if {
	not is_object(attachments)
	msg := "LY001 [宣言の形] data.layer_decl.attachments がオブジェクトではありません"
}

deny contains msg if {
	is_object(attachments)
	count(attachments) == 0
	msg := "LY001 [宣言の形] data.layer_decl.attachments が空です"
}

deny contains msg if {
	is_object(attachments)
	some t, a in attachments
	not well_formed_attachment(a)
	msg := sprintf("LY001 [宣言の形] attachments.%s が parents（引数から親の型の空でない配列への空でないオブジェクト）・optional（真偽値）・parent_arguments（parents に在る具体の親の型から、引数と書かないときの値 null か [] への空でないオブジェクト）・whole_document（真偽値）だけを持ちません", [t])
}

deny contains msg if {
	is_object(attachments)
	some t, a in attachments
	is_object(a)
	a.whole_document == true
	count(a.parents) != 1
	msg := sprintf("LY001 [宣言の形] attachments.%s は whole_document が true ですが、親の引数がちょうど1つではありません", [t])
}

deny contains msg if {
	not well_formed_unrelated(unrelated_deprecated)
	msg := "LY001 [宣言の形] data.layer_decl.unrelated_deprecated_arguments が、親の型から、引数と reason（空でない理由）・unwritten（null か []）へのオブジェクトではありません"
}

deny contains msg if {
	not well_formed_reasons(inline_owned)
	msg := "LY001 [宣言の形] data.layer_decl.inline_owned が、親の型から、引数と空でない理由へのオブジェクトではありません"
}

deny contains msg if {
	some name, x in {"unrelated_deprecated_arguments": unrelated_deprecated, "inline_owned": inline_owned}
	is_object(x)
	some t, _ in x
	not t in declared_parent_types
	msg := sprintf("LY001 [宣言の形] %s.%s は、attachments のどの項目の親の型でもありません", [name, t])
}

deny contains msg if {
	is_object(unrelated_deprecated)
	some t, args in unrelated_deprecated
	some arg, _ in args
	[t, arg] in parent_argument_pairs
	msg := sprintf("LY001 [宣言の形] %s.%s が parent_arguments と unrelated_deprecated_arguments の両方にあります", [t, arg])
}

deny contains msg if {
	is_object(inline_owned)
	some t, args in inline_owned
	some arg, _ in args
	not [t, arg] in parent_argument_pairs
	msg := sprintf("LY001 [宣言の形] inline_owned.%s.%s は、どの attachment 型の parent_arguments にもありません", [t, arg])
}

deny contains msg if {
	some [t, arg, a1] in parent_argument_decl
	some [t2, arg2, a2] in parent_argument_decl
	t2 == t
	arg2 == arg
	a1 < a2
	attachments[a1].parent_arguments[t][arg] != attachments[a2].parent_arguments[t][arg]
	msg := sprintf("LY001 [宣言の形] %s.%s の書かないときの値が、%s と %s で食い違います", [t, arg, a1, a2])
}

deny contains msg if {
	not is_object(not_attachments)
	msg := "LY001 [宣言の形] data.layer_decl.not_attachments がオブジェクトではありません"
}

deny contains msg if {
	is_object(not_attachments)
	some t, reason in not_attachments
	not non_empty_string(reason)
	msg := sprintf("LY001 [宣言の形] not_attachments.%s の理由が空でない文字列ではありません", [t])
}

deny contains msg if {
	is_object(attachments)
	is_object(not_attachments)
	some t, _ in attachments
	not_attachments[t]
	msg := sprintf("LY001 [宣言の形] %s が attachments と not_attachments の両方にあります", [t])
}

non_empty_string(s) if {
	is_string(s)
	s != ""
}

# singletons は、アカウントかリージョンに1つしか置けない型から、それを持つユースケースの名前の配列へ（ADR-0102 決定16）。
well_formed_owners(owners) if {
	is_array(owners)
	count(owners) > 0
	count({o | some o in owners}) == count(owners)
	every o in owners {
		is_string(o)
		regex.match(`^[a-z0-9][a-z0-9-]*$`, o)
	}
}

deny contains msg if {
	not is_object(singletons)
	msg := "LY001 [宣言の形] data.layer_decl.singletons がオブジェクトではありません"
}

deny contains msg if {
	is_object(singletons)
	count(singletons) == 0
	msg := "LY001 [宣言の形] data.layer_decl.singletons が空です"
}

deny contains msg if {
	is_object(singletons)
	some t, owners in singletons
	not well_formed_owners(owners)
	msg := sprintf("LY001 [宣言の形] singletons.%s が、持ち主のユースケースの名前（usecases/ の下のディレクトリ名）の、重複の無い空でない配列ではありません", [t])
}

# アカウントかリージョンの単位で設定する型は attachment 型でない（README の「分類の基準」）。
deny contains msg if {
	is_object(singletons)
	is_object(not_attachments)
	some t, _ in singletons
	not not_attachments[t]
	msg := sprintf("LY001 [宣言の形] singletons.%s は not_attachments にありません", [t])
}

# 型名の誤りは、どの resource にも当たらないまま LY401 を空振りさせる。schema が読めないときは LY306 が落とす。
deny contains msg if {
	schema_ok
	is_object(singletons)
	some t, _ in singletons
	not schema[t]
	msg := sprintf("LY001 [宣言の形] singletons.%s が Provider の schema にありません → 型名を直す", [t])
}

# --- 対象と件数 ---

deny contains msg if {
	count(listed) == 0
	msg := "LY002 [対象が0件] 走査の root に Terraform の構成ファイル（.tf と .tf.json）が1件もありません"
}

deny contains msg if {
	hcl.input_ok
	some p in listed
	not p in read
	msg := sprintf("LY002 [件数の不一致] %s を入力（hcl-input）が読んでいません", [p])
}

deny contains msg if {
	some p in read
	not p in listed
	msg := sprintf("LY002 [件数の不一致] %s は独立の走査の一覧にありません", [p])
}

deny contains msg if {
	some p in read
	hcl.is_tf_json(p)
	msg := sprintf("LY003 [未対応の入力] %s は検査が解釈しない JSON の構成です → HCL（.tf）で書く", [p])
}

# --- 検査(1): usecase 直下に置かない block（決定14） ---

in_usecase(path) if layer(hcl.dir(path)) == "usecase"

deny contains msg if {
	some b in hcl.tf_blocks(hcl.doc, "resource")
	in_usecase(b.file)
	msg := sprintf("LY101 [usecase の resource] %s が resource %s.%s を持ちます（ADR-0102 決定14）→ module に置き、usecase は module を合成する", [b.file, b.labels[0], b.labels[1]])
}

deny contains msg if {
	some b in hcl.tf_blocks(hcl.doc, "provider")
	in_usecase(b.file)
	msg := sprintf("LY102 [usecase の provider] %s が provider %s を持ちます（ADR-0102 決定14）→ provider は env の root に置く", [b.file, b.labels[0]])
}

deny contains msg if {
	some p in hcl.tf_paths(hcl.doc)
	in_usecase(p)
	some kind in hcl.root_only_kinds(hcl.doc, p)
	msg := sprintf("LY103 [root 専用の block] %s が %s を持ちます。usecase は env から呼ばれる子 module です（ADR-0102 決定14）→ env の root に置く", [p, kind])
}

# --- 検査(2): module の source が指す先（決定17） ---

calls := hcl.module_calls(hcl.doc)

relative(s) if hcl.relative_source(s)

target(c) := hcl.module_target(c.dir, c.source)

deny contains msg if {
	some c in calls
	not relative(c.source)
	msg := sprintf("LY201 [相対パスでない source] %s の module.%s の source がリポジトリ内の相対パス（./ か ../ で始まるリテラル）ではありません（ADR-0102 決定17、ADR-0205 決定3）", [c.path, c.name])
}

deny contains msg if {
	some c in calls
	relative(c.source)
	not target(c)
	msg := sprintf("LY202 [root の外] %s の module.%s の source %s が走査の root の外を指します（ADR-0102 決定17）", [c.path, c.name, c.source])
}

deny contains msg if {
	some c in calls
	not layer(c.dir)
	msg := sprintf("LY205 [層が決まらない] %s は module を呼びますが、どの層の置き場（ADR-0102 決定12）にもありません", [c.path])
}

deny contains msg if {
	some c in calls
	t := target(c)
	layer(c.dir)
	not allowed(c.dir, t)
	msg := sprintf("LY203 [層が許さない先] %s（%s）の module.%s が %s を指します（ADR-0102 決定17）", [c.path, layer(c.dir), c.name, t])
}

allowed(d, t) if {
	layer(d) == "env"
	layer(t) == "usecase"
}

allowed(d, t) if {
	layer(d) in {"usecase", "internal", "module"}
	layer(t) == "module"
}

allowed(d, t) if {
	layer(d) in {"usecase", "internal"}
	layer(t) == "internal"
	usecase_of(t) == usecase_of(d)
}

allowed(d, t) if {
	layer(d) == "reference"
	t == concat("/", ["usecases", usecase_of(d)])
}

deny contains msg if {
	some c in calls
	t := target(c)
	not t in dirs
	msg := sprintf("LY204 [先に構成が無い] %s の module.%s が指す %s に .tf がありません", [c.path, c.name, t])
}

module_graph := {d: {t | some c in calls; c.dir == d; t := target(c); t in dirs} | some d in dirs}

deny contains msg if {
	some d in dirs
	d in graph.reachable(module_graph, module_graph[d])
	msg := sprintf("LY206 [循環] %s の module の呼び出しが自分に戻ります（ADR-0102 決定17）", [d])
}

# --- 検査(3): attachment 型の親が同じ module に在ること（決定18） ---

resources := [c | some c in hcl.components(hcl.doc); startswith(c.key, "resource.")]

type_of(c) := trim_prefix(c.key, "resource.")

address(c) := trim_prefix(split(c.id, "#")[1], "resource.")

resource_ids := {c.id | some c in resources}

name_of(c) := substring(address(c), count(type_of(c)) + 1, -1)

resource_attrs(c, path) := hcl.attrs_at(c.scope, path)

ref_token(r) := concat(".", [r.root, r.steps[0].name]) if first_step_named(r)

ref_token(r) := r.root if not first_step_named(r)

first_step_named(r) if r.steps[0].kind == "attr"

# 参照の根と最初の名前の直後の添字（段1）は親を選ぶだけなので、その中の参照を辿らない。within は祖先を
# すべて持つので、その添字の中の、さらに入れ子の添字の参照も外れる。
skipped(refs, r) if {
	some w in r.within
	w.step == 1
	first_step_named(refs[w.ref])
}

# for の条件（if）は要素を絞るだけで、親を指す値を作らないので、その中の参照を辿らない。
skipped(_, r) if r.filter

token_node(d, _, tok) := sprintf("local|%s|%s", [d, substring(tok, 6, -1)]) if startswith(tok, "local.")

token_node(_, cid, tok) := sprintf("each|%s", [cid]) if {
	startswith(tok, "each.")
	cid != ""
}

token_node(_, cid, tok) := sprintf("leaf|%s", [tok]) if {
	startswith(tok, "each.")
	cid == ""
}

token_node(_, _, tok) := sprintf("leaf|%s", [tok]) if {
	not startswith(tok, "local.")
	not startswith(tok, "each.")
	tok != "count.index"
}

nodes_of(d, cid, refs) := {token_node(d, cid, ref_token(r)) | some r in refs; not skipped(refs, r)}

local_attrs := hcl.local_attrs(hcl.doc)

# 同じ名前の local を複数の block が定義しても、辿る先を1つの節に合わせる（Terraform はこれを拒む）。
ref_graph := object.union(
	{sprintf("local|%s|%s", [d, n]): union({nodes_of(d, "", a.refs) | some [d2, a] in local_attrs; d2 == d; a.path == n}) |
		some [d, a0] in local_attrs
		n := a0.path
	},
	{sprintf("each|%s", [c.id]): union({nodes_of(hcl.dir(c.path), "", a.refs) | some a in resource_attrs(c, "for_each")}) | some c in resources},
)

# graph.reachable は graph のキーに無い節（葉）を返さないので、辿った節の辺の先から葉を集める。
# 定義の無い local も、辿れない参照として葉に残す。
leaves(c, refs) := leaves_in(ref_graph, c, refs)

leaves_in(g, c, refs) := {l |
	start := nodes_of(hcl.dir(c.path), c.id, refs)
	reached := graph.reachable(g, start)
	some n in start | {m | some r in reached; some m in g[r]}
	l := leaf_of_in(g, n)
}

leaf_of_in(_, n) := trim_prefix(n, "leaf|") if startswith(n, "leaf|")

leaf_of_in(g, n) := concat("", ["local.", split(n, "|")[2]]) if {
	startswith(n, "local|")
	not g[n]
}

keywords := {"var", "data", "module", "path", "terraform", "self", "each", "local", "count"}

allowed_leaf(c, types, leaf) if {
	parts := split(leaf, ".")
	count(parts) == 2
	not parts[0] in keywords
	parent_type_ok(types, parts[0])
	sprintf("%s#resource.%s", [hcl.dir(c.path), leaf]) in resource_ids
}

parent_type_ok(types, t) if t in types

parent_type_ok(types, _) if "*" in types

deny contains msg if {
	is_object(attachments)
	some c in resources
	a := attachments[type_of(c)]
	well_formed_attachment(a)
	some arg, types in a.parents
	some at in resource_attrs(c, arg)
	ls := leaves(c, at.refs)
	bad := {l | some l in ls; not allowed_leaf(c, types, l)}
	not_same_module(ls, bad)
	msg := sprintf("LY301 [親の同居] %s の %s の %s が、同じ module の親（%s）だけを参照していません: %s（ADR-0102 決定18）→ 親の resource と同じ module に置き、親を参照する", [c.path, address(c), arg, concat(", ", types), describe(ls, bad)])
}

not_same_module(ls, _) if count(ls) == 0

not_same_module(_, bad) if count(bad) > 0

describe(ls, _) := "参照がありません" if count(ls) == 0

describe(ls, bad) := concat(", ", sort(bad)) if count(bad) > 0

deny contains msg if {
	is_object(attachments)
	some c in resources
	a := attachments[type_of(c)]
	well_formed_attachment(a)
	a.optional == false
	every arg, _ in a.parents {
		count(resource_attrs(c, arg)) == 0
	}
	msg := sprintf("LY302 [親の引数が無い] %s の %s が、宣言した親の引数（%s）をどれも持ちません。Provider が引数の名前を変えたなら宣言を直す", [c.path, address(c), concat(", ", sort(object.keys(a.parents)))])
}

# --- 決定18 の共存: 親の inline の引数（LY304）と、排他に持つ引数の親への attachment 型（LY307） ---

owned_pairs := {[t, arg] | some t, args in inline_owned; some arg, _ in args} if is_object(inline_owned)

default owned_pairs := set()

# written_arg は、resource c が引数 arg を書いたかを返す。値と式は評価しない（null も [] も、属性を持たない
# block も dynamic も書いたと数える）。
written_arg(c, arg) if hcl.writes_arg(c.scope, arg)

deny contains msg if {
	is_object(attachments)
	some c in resources
	t := type_of(c)
	some arg in {a | some [t2, a] in parent_argument_pairs; t2 == t}
	not [t, arg] in owned_pairs
	written_arg(c, arg)
	ats := sort({at | some [t3, arg3, at] in parent_argument_decl; t3 == t; arg3 == arg})
	msg := sprintf("LY304 [親の inline の引数] %s の %s が %s を書いています。同じ設定を attachment 型（%s）が書くので、値を問わず書きません（ADR-0102 決定18）→ attachment 型の resource で書く", [c.path, address(c), arg, concat(", ", ats)])
}

deny contains msg if {
	is_object(attachments)
	some c in resources
	a := attachments[type_of(c)]
	well_formed_attachment(a)
	some t, args in a.parent_arguments
	some arg, _ in args
	[t, arg] in owned_pairs
	some parg, types in a.parents
	t in types
	some at in resource_attrs(c, parg)
	some l in leaves(c, at.refs)
	startswith(l, concat("", [t, "."]))
	allowed_leaf(c, types, l)
	msg := sprintf("LY307 [排他の引数の親への attachment] %s の %s が、%s を排他に持つと宣言した親 %s を指しています（ADR-0102 決定18）→ 親の %s で書く", [c.path, address(c), arg, l, arg])
}

# --- 決定18 の文書全体の attachment の重複（LY305・LY308） ---
#
# 比べるのは別々の block の間だけ。1つの block の count・for_each の instance どうしは比べない（README の「保証しないこと」）。

whole_document_resources := [c |
	some c in resources
	a := attachments[type_of(c)]
	well_formed_attachment(a)
	a.whole_document == true
]

whole_document_arg(c) := arg if some arg, _ in attachments[type_of(c)].parents

multi(c) if count(resource_attrs(c, "count")) > 0

multi(c) if count(resource_attrs(c, "for_each")) > 0

resource_ref(r) if {
	first_step_named(r)
	not r.root in keywords
}

selection(r) := "whole" if count(r.steps) == 1

selection(r) := "whole" if r.steps[1].kind == "attr"

selection(r) := concat("", ["literal:", literal_key(r.steps[1].index.value)]) if {
	r.steps[1].kind == "index"
	r.steps[1].index.form == "literal"
}

default selection(_) := "undecidable"

# this[0] と this["0"] を同じに読む（Terraform は数の形の文字列を数へ変換する）。等しいと読む側に倒す。
numeric_string := `^[+-]?[0-9]+(\.[0-9]+)?([eE][+-]?[0-9]+)?$`

literal_key(v) := sprintf("%v", [to_number(v)]) if {
	is_string(v)
	regex.match(numeric_string, v)
}

literal_key(v) := sprintf("%v", [v]) if is_number(v)

literal_key(v) := v if {
	is_string(v)
	not regex.match(numeric_string, v)
}

pairs_in(refs) := {[ref_token(r), selection(r)] | some r in refs; not skipped(refs, r); resource_ref(r)}

plain_local(r) if {
	r.root == "local"
	first_step_named(r)
	selection(r) == "whole"
}

selected_local(r) if {
	r.root == "local"
	first_step_named(r)
	selection(r) != "whole"
}

local_node(d, r) := sprintf("%s|%s", [d, r.steps[0].name])

# 同じ名前の local を複数の block が定義しても（Terraform はこれを拒む）、定義ごとの refs の集合として1つの節に合わせる。
local_defs := {k: {a2.refs | some [d2, a2] in local_attrs; sprintf("%s|%s", [d2, a2.path]) == k} |
	some [d, a] in local_attrs
	k := sprintf("%s|%s", [d, a.path])
}

local_pairs(n) := {p | some refs in object.get(local_defs, n, set()); some p in pairs_in(refs)}

local_edges(sel) := {n: {local_node(split(n, "|")[0], r) |
	some refs in defs
	some r in refs
	not skipped(refs, r)
	edge_kind(sel, r)
} | some n, defs in local_defs}

edge_kind(false, r) if plain_local(r)

edge_kind(true, r) if selected_local(r)

edge_kind("all", r) if plain_local(r)

edge_kind("all", r) if selected_local(r)

plain_graph := local_edges(false)

selected_graph := local_edges(true)

full_graph := local_edges("all")

# local を添字で選んだら、その先の葉はすべて undecidable にする。
block_pairs(d, refs) := direct | via_plain | via_selected if {
	direct := pairs_in(refs)
	start_plain := {local_node(d, r) | some r in refs; not skipped(refs, r); plain_local(r)}
	start_sel := {local_node(d, r) | some r in refs; not skipped(refs, r); selected_local(r)}
	reach_plain := graph.reachable(plain_graph, start_plain)
	via_plain := {p | some n in reach_plain; some p in local_pairs(n)}
	sel_roots := start_sel | {m | some n in reach_plain; some m in object.get(selected_graph, n, set())}
	reach_sel := graph.reachable(full_graph, sel_roots)
	via_selected := {[l, "undecidable"] | some n in reach_sel; some [l, _] in local_pairs(n)}
}

# block_pairs が拾わない葉（each を経たもの）は undecidable にする。
parent_pairs(c) := {[l, s] |
	arg := whole_document_arg(c)
	types := attachments[type_of(c)].parents[arg]
	some at in resource_attrs(c, arg)
	ps := block_pairs(hcl.dir(c.path), at.refs)
	found := {l2 | some [l2, _] in ps}
	some [l, s] in ps | {[l3, "undecidable"] | some l3 in leaves(c, at.refs); not l3 in found}
	allowed_leaf(c, types, l)
}

decided(s) if s == "whole"

decided(s) if startswith(s, "literal:")

same_selection(s1, s2) if {
	decided(s1)
	decided(s2)
	s1 == s2
}

same_selection(s1, s2) if {
	decided(s1)
	decided(s2)
	"whole" in {s1, s2}
}

whole_document_pair contains [c1, c2, l] if {
	some c1 in whole_document_resources
	some c2 in whole_document_resources
	c1.id < c2.id
	type_of(c1) == type_of(c2)
	hcl.dir(c1.path) == hcl.dir(c2.path)
	some [l, _] in parent_pairs(c1)
	some [l2, _] in parent_pairs(c2)
	l2 == l
}

equal_pair(c1, c2, l) if {
	not multi(c1)
	not multi(c2)
	some [l1, s1] in parent_pairs(c1)
	some [l2, s2] in parent_pairs(c2)
	l1 == l
	l2 == l
	same_selection(s1, s2)
}

undecided_pair(c1, c2, _) if multi(c1)

undecided_pair(c1, c2, _) if multi(c2)

undecided_pair(c1, c2, l) if {
	some [l1, s] in parent_pairs(c1) | parent_pairs(c2)
	l1 == l
	not decided(s)
}

deny contains msg if {
	some [c1, c2, l] in whole_document_pair
	equal_pair(c1, c2, l)
	msg := sprintf("LY305 [文書全体の重複] %s の %s と %s が、同じ親 %s の文書全体を書きます（ADR-0102 決定18）→ 1つの resource にまとめる", [c1.path, address(c1), address(c2), l])
}

deny contains msg if {
	some [c1, c2, l] in whole_document_pair
	not equal_pair(c1, c2, l)
	undecided_pair(c1, c2, l)
	msg := sprintf("LY308 [判定できない文書全体の重複] %s の %s と %s が、同じ親 %s を指し、同じ instance かを参照から判定できません（ADR-0102 決定18）→ 1つの resource にまとめるか、別々の instance をリテラルの添字で指す", [c1.path, address(c1), address(c2), l])
}

# --- ADR-0301 決定21: role への managed policy の付与が policy を参照すること（LY309） ---

policy_attachment_type := "aws_iam_role_policy_attachment"

# AWS managed policy の ARN は、アカウントの部分が aws である（arn:<partition>:iam::aws:policy/<path><name>）。
aws_managed_arn := `^arn:aws(-[a-z]+)*:iam::aws:policy/.+$`

local_key(d, n) := sprintf("local|%s|%s", [d, n])

local_exprs := {local_key(d, a0.path): [a.expr | some [d2, a] in local_attrs; d2 == d; a.path == a0.path] | some [d, a0] in local_attrs}

local_alias(e) := e.steps[0].name if {
	e.kind == "traversal"
	e.root == "local"
	count(e.steps) == 1
	e.steps[0].kind == "attr"
}

partition_attribute(e) if {
	e.kind == "traversal"
	e.root == "data"
	count(e.steps) == 3
	every s in e.steps {
		s.kind == "attr"
	}
	e.steps[0].name == "aws_partition"
	e.steps[2].name == "partition"
}

alias_graph := {n: {local_key(split(n, "|")[1], local_alias(e)) | some e in es; local_alias(e)} | some n, es in local_exprs}

# partition_ref は、e が data.aws_partition.<名前>.partition か、local.<名前> だけの別名を辿った先がそれだけかを返す。
partition_ref(_, e) if partition_attribute(e)

partition_ref(d, e) if {
	reached := graph.reachable(alias_graph, {local_key(d, local_alias(e))})
	every n in reached {
		every x in local_exprs[n] {
			alias_or_partition(x)
		}
	}
	some n in reached
	some x in local_exprs[n]
	partition_attribute(x)
}

alias_or_partition(e) if partition_attribute(e)

alias_or_partition(e) if local_alias(e)

partition_template(d, e) if {
	e.kind == "template"
	count(e.parts) == 3
	hcl.literal_string(e.parts[0]) == "arn:"
	regex.match(`^:iam::aws:policy/.+$`, hcl.literal_string(e.parts[2]))
	partition_ref(d, e.parts[1])
}

# policy_graph は ref_graph と同じ辿り方で、値の全体が partition_template の local から先を辿らない。
policy_graph := object.union(
	{n: union({nodes_of(split(n, "|")[1], "", a.refs) |
		some [d, a] in local_attrs
		local_key(d, a.path) == n
		not partition_template(d, a.expr)
	}) |
		some n, _ in local_exprs
	},
	{k: v | some k, v in ref_graph; startswith(k, "each|")},
)

managed_local(n) if {
	some x in local_exprs[n]
	partition_template(split(n, "|")[1], x)
}

# flow_exprs は、policy_arn の値になりうる式。属性の式と、辿った local の式（partition_template のものを除く）と、
# each を辿ったときの for_each の式である。
flow_exprs(c, at) := array.concat(
	[at.expr],
	[x |
		some n in graph.reachable(policy_graph, nodes_of(hcl.dir(c.path), c.id, at.refs))
		some x in flow_exprs_of(c, n)
	],
)

flow_exprs_of(_, n) := [x | some x in local_exprs[n]; not partition_template(split(n, "|")[1], x)] if startswith(n, "local|")

flow_exprs_of(c, n) := [a.expr | some a in resource_attrs(c, "for_each")] if startswith(n, "each|")

# each.key を書けば、for_each のキーも値になる。
keys_flow(at) if {
	some r in at.refs
	r.root == "each"
	r.steps[0].name == "key"
}

stop_kinds := {"literal", "traversal", "call", "other"}

# flow_nodes は、式 e の中の、値になりうる位置の式の節。リテラルの値・参照の段・関数の引数・中を読めない式の中には
# 入らない。オブジェクトのキーは、keys が真のときだけ数える。
flow_nodes(e, keys) := [x |
	m := {json.marshal(p): v | walk(e, [p, v])}
	walk(e, [p, x])
	is_object(x)
	is_string(x.kind)
	not stopped(m, p)
	key_ok(p, keys)
]

stopped(m, p) if {
	count(p) > 0
	some k in numbers.range(0, count(p) - 1)
	a := m[json.marshal(array.slice(p, 0, k))]
	is_object(a)
	a.kind in stop_kinds
}

key_ok(_, true) := true

key_ok(p, false) if not "key" in p

flow_literals(c, at) := {v |
	some e in flow_exprs(c, at)
	some x in flow_nodes(e, keys_flow_of(at))
	x.kind == "literal"
	some v in scalars(x.value, keys_flow_of(at))
}

default keys_flow_of(_) := false

keys_flow_of(at) := true if keys_flow(at)

scalars(v, keys) := {s | walk(v, [_, s]); not is_object(s); not is_array(s)} | object_keys(v, keys)

object_keys(_, false) := set()

object_keys(v, true) := {k | walk(v, [_, o]); is_object(o); some k, _ in o}

opaque_exprs(c, at) := {describe_opaque(x) |
	some e in flow_exprs(c, at)
	some x in flow_nodes(e, keys_flow_of(at))
	x.kind in {"call", "other"}
}

describe_opaque(x) := sprintf("%s()", [x.name]) if x.kind == "call"

describe_opaque(x) := x.syntax if x.kind == "other"

policy_leaves(c, at) := leaves_in(policy_graph, c, at.refs)

managed_locals(c, at) := {n | some n in graph.reachable(policy_graph, nodes_of(hcl.dir(c.path), c.id, at.refs)); managed_local(n)}

policy_problems(c, at) := [p |
	some p in [
		describe_problem("参照", {l | some l in policy_leaves(c, at); not allowed_leaf(c, {"aws_iam_policy"}, l)}),
		describe_problem("リテラル", {json.marshal(v) | some v in flow_literals(c, at); not aws_managed_literal(v)}),
		describe_problem("値を決められない式", opaque_exprs(c, at)),
	]
	p != ""
]

aws_managed_literal(v) if {
	is_string(v)
	regex.match(aws_managed_arn, v)
}

describe_problem(_, xs) := "" if count(xs) == 0

describe_problem(label, xs) := sprintf("%s %s", [label, concat(", ", sort(xs))]) if count(xs) > 0

policy_empty(c, at) if {
	count(policy_leaves(c, at)) == 0
	count(flow_literals(c, at)) == 0
	count(managed_locals(c, at)) == 0
}

policy_violation(c, at) := concat(" / ", policy_problems(c, at)) if {
	not partition_template(hcl.dir(c.path), at.expr)
	count(policy_problems(c, at)) > 0
}

policy_violation(c, at) := "参照もリテラルもありません" if {
	not partition_template(hcl.dir(c.path), at.expr)
	count(policy_problems(c, at)) == 0
	policy_empty(c, at)
}

default policy_hint(_) := ""

policy_hint(v) := "。関数の呼び出しや条件式は値を決められないので、policy を aws_iam_policy.x[0].arn のように添字で参照する" if contains(v, "値を決められない式")

deny contains msg if {
	some c in resources
	type_of(c) == policy_attachment_type
	some at in resource_attrs(c, "policy_arn")
	v := policy_violation(c, at)
	msg := sprintf("LY309 [policy の参照] %s の %s の policy_arn が、同じ module の aws_iam_policy の参照と AWS managed policy の ARN だけに決まりません: %s（ADR-0301 決定21）→ 同じ module に aws_iam_policy を置いて参照するか、AWS managed policy の ARN をリテラルか partition だけ data.aws_partition から組むテンプレートで書く%s", [c.path, address(c), v, policy_hint(v)])
}

deny contains msg if {
	some c in resources
	type_of(c) == policy_attachment_type
	count(resource_attrs(c, "policy_arn")) == 0
	msg := sprintf("LY309 [policy の参照] %s の %s が policy_arn を書いていません（ADR-0301 決定21）→ policy_arn に、同じ module の aws_iam_policy の参照か AWS managed policy の ARN を書く", [c.path, address(c)])
}

# --- 決定18 の分類の網羅: Provider の schema との突き合わせ（LY306） ---

schema := data.layer_schema

default schema_ok := false

schema_ok if {
	is_object(schema)
	count(schema) > 0
	every _, e in schema {
		well_formed_schema_entry(e)
	}
}

well_formed_schema_entry(e) if {
	is_object(e)
	{k | some k, _ in e} == {"arguments", "deprecated", "blocks"}
	every k in ["arguments", "deprecated", "blocks"] {
		is_array(e[k])
		every x in e[k] {
			non_empty_string(x)
		}
	}
	every x in e.deprecated {
		x in e.arguments
	}
	every x in e.blocks {
		x in e.arguments
	}
}

deny contains msg if {
	not schema_ok
	msg := "LY306 [schema の一覧] data.layer_schema が無いか、空か、形が違います（型ごとに arguments・deprecated・blocks の文字列の配列。deprecated と blocks は arguments の部分集合）→ make layer-check から起動する"
}

# 書ける引数を1つも持たない親の型は、抜き出しの式が壊れて全型が空になった状態と区別できないので落とす。
deny contains msg if {
	schema_ok
	some t in declared_parent_types
	count(schema[t].arguments) == 0
	msg := sprintf("LY306 [schema の引数が空] 親の型 %s の書ける引数が schema から1つも取れていません → scripts/provider-schema の抜き出しと、terraform providers schema -json の出力を確かめる", [t])
}

deny contains msg if {
	schema_ok
	some t in declared_parent_types
	not schema[t]
	msg := sprintf("LY306 [schema に無い親の型] 親の型 %s が Provider の schema にありません → 宣言の型名を直すか、Provider の版を確かめる", [t])
}

classified_args(t) := {arg | some [t2, arg] in parent_argument_pairs; t2 == t} | {arg | some arg, _ in object.get(unrelated_deprecated, t, {})}

deny contains msg if {
	schema_ok
	is_object(unrelated_deprecated)
	some t in declared_parent_types
	some arg in schema[t].deprecated
	not arg in classified_args(t)
	msg := sprintf("LY306 [分類されていない非推奨の引数] 親の型 %s の非推奨の引数 %s が、どの attachment 型の parent_arguments にも unrelated_deprecated_arguments にもありません（ADR-0102 決定18）→ 同じ設定を書く attachment 型の parent_arguments か、理由とともに unrelated_deprecated_arguments に置く", [t, arg])
}

declared_args := parent_argument_pairs | {[t, arg] | some t, args in unrelated_deprecated; some arg, _ in args} | owned_pairs

deny contains msg if {
	schema_ok
	is_object(unrelated_deprecated)
	some [t, arg] in declared_args
	schema[t]
	not arg in schema[t].arguments
	msg := sprintf("LY306 [schema に無い引数] 宣言の %s.%s が、Provider の schema の書ける引数にありません → 宣言の引数名を直す", [t, arg])
}

deny contains msg if {
	schema_ok
	is_object(unrelated_deprecated)
	some t, args in unrelated_deprecated
	schema[t]
	some arg, _ in args
	not arg in schema[t].deprecated
	msg := sprintf("LY306 [古い分類] unrelated_deprecated_arguments の %s.%s は、Provider の schema で非推奨ではありません → 宣言から外す", [t, arg])
}

deny contains msg if {
	schema_ok
	some [t, arg, at] in parent_argument_decl
	schema[t]
	arg in schema[t].arguments
	v := attachments[at].parent_arguments[t][arg]
	not unwritten_matches(schema[t], arg, v)
	msg := sprintf("LY306 [書かないときの値] attachments.%s.parent_arguments の %s.%s の値が schema の種類と合いません（block は []、属性は null）", [at, t, arg])
}

deny contains msg if {
	schema_ok
	well_formed_unrelated(unrelated_deprecated)
	some t, args in unrelated_deprecated
	schema[t]
	some arg, e in args
	arg in schema[t].arguments
	not unwritten_matches(schema[t], arg, e.unwritten)
	msg := sprintf("LY306 [書かないときの値] unrelated_deprecated_arguments の %s.%s の unwritten が schema の種類と合いません（block は []、属性は null）", [t, arg])
}

unwritten_matches(e, arg, v) if {
	arg in e.blocks
	v == []
}

unwritten_matches(e, arg, v) if {
	not arg in e.blocks
	v == null
}

# 宣言のどの区分にも無い型は、親を外から書き換える経路かどうかを判定できないので落とす。親の型に
# 現れるだけでは分類に数えない。
classified contains t if some t, _ in attachments

classified contains t if some t, _ in not_attachments

deny contains msg if {
	is_object(attachments)
	is_object(not_attachments)
	some t in {type_of(c) | some c in resources}
	not t in classified
	msg := sprintf("LY303 [分類されていない型] resource の型 %s が宣言（conftest/hcl/layer/decl.json）のどの区分にもありません → attachment なら親の引数と親の型を、そうでなければ理由を宣言に足す", [t])
}

# --- 決定16: アカウントかリージョンに1つしか置けない型の持ち主（LY401） ---

owned_by(path, owners) if {
	some o in owners
	startswith(path, concat("", ["usecases/", o, "/"]))
}

deny contains msg if {
	is_object(singletons)
	some c in resources
	owners := singletons[type_of(c)]
	well_formed_owners(owners)
	not owned_by(c.path, owners)
	msg := sprintf("LY401 [単一の型の持ち主] %s の %s は、アカウントかリージョンに1つしか置けない型で、持ち主のユースケース（%s）の外で宣言されています。後から apply した側が黙って上書きします（ADR-0102 決定16）→ 持ち主のユースケースに置き、ほかのユースケースは識別子を受け取る", [c.path, address(c), concat(", ", owners)])
}

# --- HCL の入力（hcl-input の出力。ADR-0501 決定5） ---

deny contains msg if {
	some e in hcl.input_errors
	msg := sprintf("LY004 [HCL の入力の形] %s → make layer-check から起動する。hcl-input の出力と conftest/hcl/lib/hcl_input.rego の読む形を揃える", [e])
}
