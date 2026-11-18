# 層の規則の検査（ADR-0102 決定14・17・18。検証方法の3つ）。
#
# 走査の root の下の全ファイルを1つの入力として掛ける（--combine）。make が Conftest とは別の経路で数えた
# 構成ファイルの一覧（data.layer_files）と、Conftest が読んだファイルを突き合わせる（ADR-0702 決定15）。
# deny contains はすべてこのファイルに置く。tf-local policy-result はこのファイルの行頭の deny contains を数え、
# 評価した規則の数と突き合わせる。
package layer

import data.lib.hcl

default decl := {}

decl := data.layer_decl if is_object(data.layer_decl)

attachments := object.get(decl, "attachments", null)

not_attachments := object.get(decl, "not_attachments", null)

default listed := set()

listed := {p | some p in data.layer_files} if is_array(data.layer_files)

# JSON の構成は検査が解釈しないので、数えたうえで落とす（ADR-0501 決定5、ADR-0103 決定11）。
is_tf_json(path) if endswith(path, ".tf.json")

is_config(path) if hcl.is_tf(path)

is_config(path) if is_tf_json(path)

read := {f.path | some f in input; is_config(f.path)}

tf_files := [f | some f in input; hcl.is_tf(f.path)]

dirs := {hcl.dir(f.path) | some f in tf_files}

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
	{k | some k, _ in a} == {"parents", "optional"}
	is_boolean(a.optional)
	well_formed_parents(a.parents)
}

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
	msg := sprintf("LY001 [宣言の形] attachments.%s が parents（引数から親の型の空でない配列への空でないオブジェクト）と optional（真偽値）だけを持ちません", [t])
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

# --- 対象と件数 ---

deny contains msg if {
	count(listed) == 0
	msg := "LY002 [対象が0件] 走査の root に Terraform の構成ファイル（.tf と .tf.json）が1件もありません"
}

deny contains msg if {
	some p in listed
	not p in read
	msg := sprintf("LY002 [件数の不一致] %s を Conftest が読んでいません", [p])
}

deny contains msg if {
	some p in read
	not p in listed
	msg := sprintf("LY002 [件数の不一致] %s は独立の走査の一覧にありません", [p])
}

deny contains msg if {
	some p in read
	is_tf_json(p)
	msg := sprintf("LY003 [未対応の入力] %s は検査が解釈しない JSON の構成です → HCL（.tf）で書く", [p])
}

# --- 検査(1): usecase 直下に置かない block（決定14） ---

usecase_files := [f | some f in tf_files; layer(hcl.dir(f.path)) == "usecase"]

deny contains msg if {
	some f in usecase_files
	some type, names in object.get(f.contents, "resource", {})
	some name, _ in names
	msg := sprintf("LY101 [usecase の resource] %s が resource %s.%s を持ちます（ADR-0102 決定14）→ module に置き、usecase は module を合成する", [f.path, type, name])
}

deny contains msg if {
	some f in usecase_files
	some name, _ in object.get(f.contents, "provider", {})
	msg := sprintf("LY102 [usecase の provider] %s が provider %s を持ちます（ADR-0102 決定14）→ provider は env の root に置く", [f.path, name])
}

deny contains msg if {
	some f in usecase_files
	some kind in root_only_blocks(f.contents)
	msg := sprintf("LY103 [root 専用の block] %s が %s を持ちます。usecase は env から呼ばれる子 module です（ADR-0102 決定14）→ env の root に置く", [f.path, kind])
}

root_only_blocks(contents) := {"import" | object.get(contents, "import", null) != null} | {k |
	some t in object.get(contents, "terraform", [])
	some k in ["backend", "cloud"]
	object.get(t, k, null) != null
}

# --- 検査(2): module の source が指す先（決定17） ---

calls contains c if {
	some f in tf_files
	some name, bodies in object.get(f.contents, "module", {})
	some body in bodies
	c := {"path": f.path, "dir": hcl.dir(f.path), "name": name, "source": object.get(body, "source", null)}
}

relative(s) if {
	is_string(s)
	not contains(s, "${")
	startswith(s, "./")
}

relative(s) if {
	is_string(s)
	not contains(s, "${")
	startswith(s, "../")
}

target(c) := hcl.clean(sprintf("%s/%s", [c.dir, c.source])) if relative(c.source)

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

resources := [c | some c in hcl.components(input); startswith(c.key, "resource.")]

type_of(c) := trim_prefix(c.key, "resource.")

address(c) := trim_prefix(split(c.id, "#")[1], "resource.")

resource_ids := {c.id | some c in resources}

# 値の文字列から、補間（`${...}`）の内側の参照を取り出す。辿り方は README の「親を指す引数の辿り方」。
ref_tokens(v) := {tok |
	some s in strings_in(v)
	some tok in string_tokens(s)
}

# 補間の対応を読めない文字列は、参照を取りこぼさないよう、許されない葉として返す。
unreadable := "（読めない式）"

strings_in(v) := {v} if is_string(v)

# オブジェクトのキーも式を持ちうる（`{ (var.x) = ... }`）ので、値と並べて読む。
strings_in(v) := {s | walk(v, [_, s]); is_string(s)} | {k | walk(v, [p, _]); some k in p; is_string(k)} if not is_string(v)

# 式の中の引用符の文字列。中の `{` `}` `$` は文字列の一部で、補間の対応にも参照にも数えない。
quoted := `"(?:[^"\\]|\\.)*"`

# 入れ子の `{}` は3段まで読む。それより深い補間は外に `${` が残るので unreadable になる。
interpolation := sprintf(`\$\{((?:[^{}"]|%s|\{(?:[^{}"]|%s|\{(?:[^{}"]|%s)*\})*\})*)\}`, [quoted, quoted, quoted])

interpolations(s) := [m[1] | some m in regex.find_all_string_submatch_n(interpolation, s, -1)]

string_tokens(s) := {unreadable} if unreadable_string(s)

string_tokens(s) := {tok |
	some expr in interpolations(s)
	some tok in expression_tokens(expr)
} if not unreadable_string(s)

# パーサは隣り合う文字列を連結して返す。次の形は、本物のエスケープや文字列と、連結の結果とを区別できない。
unreadable_string(s) if contains(regex.replace(s, interpolation, ""), "${")

unreadable_string(s) if contains(s, "$${")

# テンプレートの指示子（`%{ if }` `%{ for }`）の式は読まない。`%%{` はリテラルのエスケープ。
unreadable_string(s) if contains(replace(s, "%%{", ""), "%{")

# 式の中のコメントは、引用符を含めば文字列の対をずらす。
unreadable_string(s) if {
	some expr in interpolations(s)
	regex.match(`/\*|//|#`, literal_free(expr))
}

# 予約された根や resource の型と同じ名前の反復変数は、同じ式の外部参照を隠しうる。
unreadable_string(s) if {
	some expr in interpolations(s)
	some name in for_iterators(literal_free(expr))
	name in keywords | resource_types
}

resource_types := {type_of(c) | some c in resources}

# 式の中の、補間を持たない引用符の文字列は参照ではない。`for` の反復変数は参照の根でない。
expression_tokens(expr) := {tok |
	body := strip_index(literal_free(expr))
	iterators := for_iterators(literal_free(expr))
	some m in regex.find_all_string_submatch_n(`(^|[^A-Za-z0-9_.])([A-Za-z_][A-Za-z0-9_-]*(\.[A-Za-z_*][A-Za-z0-9_-]*)+)`, body, -1)
	parts := split(m[2], ".")
	not parts[0] in iterators
	tok := concat(".", array.slice(parts, 0, 2))
}

for_iterators(expr) := {name |
	some m in regex.find_all_string_submatch_n(`\bfor\s+([A-Za-z_][A-Za-z0-9_-]*)(?:\s*,\s*([A-Za-z_][A-Za-z0-9_-]*))?\s+in\b`, expr, -1)
	some name in array.slice(m, 1, 3)
	name != ""
}

# 引用符の文字列を字句として左から切り出し、補間を持たないものを除く。補間を持つ文字列は中の参照を読むので残す。
literal_free(expr) := concat(" ", [t |
	some t in regex.find_n(sprintf(`%s|[^"]+`, [quoted]), expr, -1)
	keep_token(t)
])

keep_token(t) if not startswith(t, `"`)

keep_token(t) if {
	startswith(t, `"`)
	contains(t, "${")
}

# resource か local の参照（`<名前>.<名前>`）の直後の添字は親を選ぶだけなので、中を辿らない。属性の後の添字と、
# 2つ目以降の添字は辿る。剥がした跡の `#` は、続く添字を参照の直後と読ませない。
strip_index(s) := r if {
	pattern := `(^|[^A-Za-z0-9_.\]#])([A-Za-z_][A-Za-z0-9_-]*\.[A-Za-z_][A-Za-z0-9_-]*)\[[^\[\]]*\]`
	r := regex.replace(regex.replace(regex.replace(s, pattern, "$1$2#"), pattern, "$1$2#"), pattern, "$1$2#")
}

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

nodes_of(d, cid, v) := {n | some tok in ref_tokens(v); n := token_node(d, cid, tok)}

local_values := {[hcl.dir(f.path), name, value] |
	some f in tf_files
	some l in object.get(f.contents, "locals", [])
	some name, value in l
}

ref_graph := object.union(
	{sprintf("local|%s|%s", [d, n]): nodes_of(d, "", v) | some [d, n, v] in local_values},
	{sprintf("each|%s", [c.id]): nodes_of(hcl.dir(c.path), "", object.get(c.body, "for_each", null)) | some c in resources},
)

# graph.reachable は graph のキーに無い節（葉）を返さないので、辿った節の辺の先から葉を集める。
# 定義の無い local も、辿れない参照として葉に残す。
leaves(c, v) := {l |
	start := nodes_of(hcl.dir(c.path), c.id, v)
	reached := graph.reachable(ref_graph, start)
	some n in start | {m | some r in reached; some m in ref_graph[r]}
	l := leaf_of(n)
}

leaf_of(n) := trim_prefix(n, "leaf|") if startswith(n, "leaf|")

leaf_of(n) := concat("", ["local.", split(n, "|")[2]]) if {
	startswith(n, "local|")
	not ref_graph[n]
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
	some v in hcl.values_at(c.body, arg)
	ls := leaves(c, v)
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
		count(hcl.values_at(c.body, arg)) == 0
	}
	msg := sprintf("LY302 [親の引数が無い] %s の %s が、宣言した親の引数（%s）をどれも持ちません。Provider が引数の名前を変えたなら宣言を直す", [c.path, address(c), concat(", ", sort(object.keys(a.parents)))])
}

# 宣言のどの区分にも無い型は、親を外から書き換える経路かどうかを判定できないので落とす。親の型に
# 現れるだけでは分類に数えない。親になる型も、それ自身が attachment かどうかの判断を宣言に残す。
classified contains t if some t, _ in attachments

classified contains t if some t, _ in not_attachments

deny contains msg if {
	is_object(attachments)
	is_object(not_attachments)
	some t in {type_of(c) | some c in resources}
	not t in classified
	msg := sprintf("LY303 [分類されていない型] resource の型 %s が宣言（conftest/hcl/layer/decl.json）のどの区分にもありません → attachment なら親の引数と親の型を、そうでなければ理由を宣言に足す", [t])
}
