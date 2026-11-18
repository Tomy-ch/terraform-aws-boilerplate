# 層の規則の検査（ADR-0102 決定14・17・18。検証方法の3つ）。
#
# deny contains は行頭に置く。tf-local policy-result が行頭の deny contains を数える（README の「検査」）。
package layer

import data.lib.hcl

default decl := {}

decl := data.layer_decl if is_object(data.layer_decl)

attachments := object.get(decl, "attachments", null)

not_attachments := object.get(decl, "not_attachments", null)

default listed := set()

listed := {p | some p in data.layer_files} if is_array(data.layer_files)

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

resources := [c | some c in hcl.components(input); startswith(c.key, "resource.")]

type_of(c) := trim_prefix(c.key, "resource.")

address(c) := trim_prefix(split(c.id, "#")[1], "resource.")

resource_ids := {c.id | some c in resources}

name_of(c) := substring(address(c), count(type_of(c)) + 1, -1)

# resource_attrs は、hcl-input が c の block について出した、path の属性をすべて返す。
resource_attrs(c, path) := [a |
	some b in go_blocks
	b.kind == "resource"
	b.file == c.path
	b.labels == [type_of(c), name_of(c)]
	some a in b.attributes
	a.path == path
]

# 参照のトークンは、根と最初の段の名前（`aws_s3_bucket.this`、`local.x`）。最初の段が名前でなければ根だけ。
ref_token(r) := concat(".", [r.root, r.steps[0].name]) if first_step_named(r)

ref_token(r) := r.root if not first_step_named(r)

first_step_named(r) if r.steps[0].kind == "attr"

# 参照の根と最初の名前の直後の添字（段1）は親を選ぶだけなので、その中の参照を辿らない。within は祖先を
# すべて持つので、その添字の中の、さらに入れ子の添字の参照も外れる。属性の後の添字と2つ目以降の添字は辿る。
skipped(refs, r) if {
	some w in r.within
	w.step == 1
	first_step_named(refs[w.ref])
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

nodes_of(d, cid, refs) := {token_node(d, cid, ref_token(r)) | some r in refs; not skipped(refs, r)}

local_attrs := [[hcl.dir(b.file), a] |
	some b in go_blocks
	b.kind == "locals"
	some a in b.attributes
]

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
leaves(c, refs) := {l |
	start := nodes_of(hcl.dir(c.path), c.id, refs)
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
	go_ok
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
	go_ok
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

# --- HCL の入力（hcl-input の出力。ADR-0501 決定5） ---
#
# 検査(3) は参照を data.hcl_input から読む。形の崩れた入力は LY004 だけで落とし、それを読む規則は評価しない。
# 欠けを空として先へ進めると、親の引数が無いものとして LY301 が黙って通るため。

default hcl_input := null

hcl_input := data.hcl_input

go_ok if {
	is_object(hcl_input)
	go_files_ok
	is_array(hcl_input.blocks)
	every b in hcl_input.blocks {
		well_formed_block(b)
	}
}

go_files_ok if {
	is_array(hcl_input.files)
	every p in hcl_input.files {
		non_empty_string(p)
	}
}

go_blocks := hcl_input.blocks if go_ok

go_files := {p | some p in hcl_input.files} if go_ok

well_formed_block_head(b) if {
	is_object(b)
	{k | some k, _ in b} == {"file", "kind", "labels", "attributes"}
	non_empty_string(b.file)
	non_empty_string(b.kind)
	is_array(b.labels)
	every l in b.labels {
		is_string(l)
	}
	is_array(b.attributes)
}

well_formed_block(b) if {
	well_formed_block_head(b)
	every a in b.attributes {
		well_formed_attribute(a)
	}
}

well_formed_attribute(a) if {
	is_object(a)
	{k | some k, _ in a} == {"path", "refs"}
	non_empty_string(a.path)
	is_array(a.refs)
	every r in a.refs {
		well_formed_ref(a.refs, r)
	}
}

well_formed_ref(refs, r) if {
	is_object(r)
	{k | some k, _ in r} == {"root", "steps", "within"}
	non_empty_string(r.root)
	is_array(r.steps)
	every s in r.steps {
		well_formed_step(s)
	}
	is_array(r.within)
	every w in r.within {
		well_formed_within(refs, w)
	}
}

well_formed_step(s) if s == {"kind": "index"}

well_formed_step(s) if s == {"kind": "splat"}

well_formed_step(s) if {
	is_object(s)
	{k | some k, _ in s} == {"kind", "name"}
	s.kind == "attr"
	non_empty_string(s.name)
}

# within は、同じ属性の参照（配列の位置）の、添字の段を指す。
well_formed_within(refs, w) if {
	is_object(w)
	{k | some k, _ in w} == {"ref", "step"}
	refs[w.ref].steps[w.step].kind == "index"
}

deny contains msg if {
	not is_object(hcl_input)
	msg := "LY004 [HCL の入力の形] data.hcl_input がオブジェクトではありません → make layer-check が hcl-input の出力を --data で渡す"
}

deny contains msg if {
	is_object(hcl_input)
	not go_files_ok
	msg := "LY004 [HCL の入力の形] hcl_input.files が、空でない文字列の配列ではありません"
}

deny contains msg if {
	is_object(hcl_input)
	not is_array(object.get(hcl_input, "blocks", null))
	msg := "LY004 [HCL の入力の形] hcl_input.blocks が配列ではありません"
}

deny contains msg if {
	is_array(object.get(hcl_input, "blocks", null))
	some i, b in hcl_input.blocks
	not well_formed_block_head(b)
	msg := sprintf("LY004 [HCL の入力の形] hcl_input.blocks[%d] が、file・kind・labels（文字列の配列）・attributes（配列）だけを持つ形ではありません", [i])
}

deny contains msg if {
	is_array(object.get(hcl_input, "blocks", null))
	some i, b in hcl_input.blocks
	well_formed_block_head(b)
	some j, a in b.attributes
	not well_formed_attribute(a)
	msg := sprintf("LY004 [HCL の入力の形] hcl_input.blocks[%d].attributes[%d]（%s）が、path と refs（root・steps・within を持つ参照の配列）の形ではありません", [i, j, b.file])
}

deny contains msg if {
	go_ok
	some p in {f.path | some f in tf_files}
	not p in go_files
	msg := sprintf("LY002 [件数の不一致] %s を hcl-input が読んでいません", [p])
}

deny contains msg if {
	go_ok
	some p in go_files
	not p in {f.path | some f in tf_files}
	msg := sprintf("LY002 [件数の不一致] %s は hcl-input が読みましたが、Conftest は読んでいません", [p])
}

conftest_resource_keys := {[c.path, [type_of(c), name_of(c)]] | some c in resources}

go_resource_keys := {[b.file, b.labels] | some b in go_blocks; b.kind == "resource"}

deny contains msg if {
	go_ok
	some [path, labels] in conftest_resource_keys
	not [path, labels] in go_resource_keys
	msg := sprintf("LY005 [HCL の入力の突き合わせ] %s の resource %s を hcl-input が出していません", [path, concat(".", labels)])
}

deny contains msg if {
	go_ok
	some [path, labels] in go_resource_keys
	not [path, labels] in conftest_resource_keys
	msg := sprintf("LY005 [HCL の入力の突き合わせ] hcl-input が出した %s の resource %s を Conftest が読んでいません", [path, concat(".", labels)])
}

# 検査(3) が hcl-input から読む引数は、Conftest の側と数を合わせる。片方のパーサだけが読んだ引数を、
# 「親の引数が無い」として通さないため。
deny contains msg if {
	go_ok
	is_object(attachments)
	some c in resources
	a := attachments[type_of(c)]
	well_formed_attachment(a)
	some arg in {k | some k, _ in a.parents} | {"for_each"}
	want := count(hcl.values_at(c.body, arg))
	got := count(resource_attrs(c, arg))
	want != got
	msg := sprintf("LY005 [HCL の入力の突き合わせ] %s の %s の %s が、Conftest では %d 個、hcl-input では %d 個です", [c.path, address(c), arg, want, got])
}
