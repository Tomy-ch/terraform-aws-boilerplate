# root の構成と backend 設定の不変条件の検査（ADR-0604 決定2-4・7・8 と検証方法）。
#
# deny contains は行頭に置く。tf-local policy-result が行頭の deny contains を数える（README の「検査」）。
package root

import data.lib.hcl_input as hcl

# --- 入力の読み方 ---
#
# 入力は scripts/hcl-input が走査の root の下から作る JSON（`-format syntax`）で、その形に触れるのは、この節と
# lib.hcl_input だけである（README「HCL の読み方」）。崩れた入力の上では、それを読む規則を評価しない。

doc := input if hcl.input_ok

is_tf_json(path) if endswith(path, ".tf.json")

is_config(path) if hcl.is_tf(path)

is_config(path) if is_tf_json(path)

read := {f.path | some f in doc.files; is_config(f.path)}

tf_paths := {f.path | some f in doc.files; f.parsed; hcl.is_tf(f.path)}

dirs := {hcl.dir(p) | some p in tf_paths}

base_name(path) := parts[count(parts) - 1] if {
	parts := split(path, "/")
}

module_calls := hcl.module_calls(doc)

module_count(path) := hcl.module_count(doc, path)

backends := hcl.backends(doc)

cloud_blocks := hcl.cloud_blocks(doc)

literal(e) if hcl.is_literal(e)

has_attr(b, name) if name in object.keys(b.attrs)

# --- root の分類（ADR-0604 決定5） ---
#
# 2つの位置の外の root の検出は oidc-job-lint が持つ（README「root の決め方」）。

root_kind(d) := "identity" if regex.match(`^env/[^/]+/identity$`, d)

root_kind(d) := "workload" if regex.match(`^env/[^/]+$`, d)

env_of(d) := split(d, "/")[1]

roots := {d | some d in dirs; root_kind(d)}

usecase_of(t) := split(t, "/")[1] if regex.match(`^usecases/[^/]+$`, t)

expected_key := {"identity": "identity/terraform.tfstate", "workload": "workload/terraform.tfstate"}

# --- 宣言 ---

default decl := {}

# data.root_decl が無いときも RT001 で落とすため、無ければ null に読む。
default raw_decl := null

raw_decl := data.root_decl

decl := raw_decl if is_object(raw_decl)

management_env := object.get(decl, "management_env", null)

declared := object.get(decl, "usecases", null)

artifact_kinds := {"bootstrap", "control"}

default listed := set()

listed := {p | some p in data.root_files} if is_array(data.root_files)

default module_counts := {}

module_counts := data.root_module_counts if is_object(data.root_module_counts)

deny contains msg if {
	some e in hcl.input_errors
	msg := sprintf("RT005 [入力の形] %s → make root-check から起動する。hcl-input の出力と conftest/hcl/lib/hcl_input.rego の読む形を揃える", [e])
}

deny contains msg if {
	not is_object(raw_decl)
	msg := "RT001 [宣言の形] data.root_decl がオブジェクトではありません"
}

deny contains msg if {
	is_object(raw_decl)
	some k, _ in decl
	not k in {"management_env", "usecases"}
	msg := sprintf("RT001 [宣言の形] data.root_decl が未知の鍵 %s を持ちます（management_env と usecases だけ）", [k])
}

deny contains msg if {
	is_object(raw_decl)
	not env_token(management_env)
	msg := "RT001 [宣言の形] data.root_decl.management_env が env の名前（英小文字・数字・ハイフン）ではありません"
}

env_token(s) if {
	is_string(s)
	regex.match(`^[a-z0-9]+(-[a-z0-9]+)*$`, s)
}

deny contains msg if {
	is_object(raw_decl)
	not is_object(declared)
	msg := "RT001 [宣言の形] data.root_decl.usecases がオブジェクトではありません"
}

deny contains msg if {
	is_object(declared)
	some u, kind in declared
	not kind in artifact_kinds
	msg := sprintf("RT001 [宣言の形] usecases.%s が bootstrap と control のどちらでもありません", [u])
}

deny contains msg if {
	is_object(declared)
	not "bootstrap" in {kind | some _, kind in declared}
	msg := "RT001 [宣言の形] data.root_decl.usecases に bootstrap の usecase が1つもありません"
}

# --- 対象と件数 ---

deny contains msg if {
	count(listed) == 0
	msg := "RT002 [対象が0件] 走査の root に Terraform の構成ファイル（.tf と .tf.json）が1件もありません"
}

deny contains msg if {
	hcl.input_ok
	count(listed) > 0
	count(roots) == 0
	msg := "RT002 [root が0件] env/<env>/ にも env/<env>/identity/ にも .tf がありません"
}

deny contains msg if {
	hcl.input_ok
	some p in listed
	not p in read
	msg := sprintf("RT002 [件数の不一致] %s を入力（hcl-input）が読んでいません", [p])
}

deny contains msg if {
	some p in read
	not p in listed
	msg := sprintf("RT002 [件数の不一致] %s は独立の走査の一覧にありません", [p])
}

deny contains msg if {
	some p in tf_paths
	module_count(p) != object.get(module_counts, p, -1)
	msg := sprintf("RT002 [件数の不一致] %s の module block を %d 件読みましたが、独立の走査は %v 件です", [p, module_count(p), object.get(module_counts, p, "数えていない")])
}

deny contains msg if {
	some p in read
	is_tf_json(p)
	msg := sprintf("RT003 [未対応の入力] %s は検査が解釈しない JSON の構成です → HCL（.tf）で書く", [p])
}

deny contains msg if {
	some p in tf_paths
	root_kind(hcl.dir(p))
	override_file(base_name(p))
	msg := sprintf("RT003 [未対応の入力] %s は root の構成を上書きする override のファイルです。検査は上書きを合成しません → 元のファイルに書く", [p])
}

override_file(name) if name == "override.tf"

override_file(name) if endswith(name, "_override.tf")

deny contains msg if {
	hcl.input_ok
	is_object(declared)
	some u, _ in declared
	not concat("/", ["usecases", u]) in dirs
	msg := sprintf("RT004 [宣言が実体を指さない] usecases.%s を宣言していますが、usecases/%s に .tf がありません", [u, u])
}

deny contains msg if {
	hcl.input_ok
	env_token(management_env)
	not sprintf("env/%s/identity", [management_env]) in dirs
	msg := sprintf("RT004 [宣言が実体を指さない] management_env の %s の identity root（env/%s/identity）に .tf がありません", [management_env, management_env])
}

# --- root が呼ぶ usecase（ADR-0604 決定3・4） ---

root_calls := [c | some c in module_calls; root_kind(c.dir)]

deny contains msg if {
	some c in root_calls
	not hcl.module_target(c.dir, c.source)
	msg := sprintf("RT104 [source が解けない] %s の module.%s の source が、走査の root の中を指す相対パスのリテラルではありません。呼ぶ先を判定できません", [c.path, c.name])
}

deny contains msg if {
	some c in root_calls
	root_kind(c.dir) == "identity"
	t := hcl.module_target(c.dir, c.source)
	not declared_kind(t)
	msg := sprintf("RT101 [identity root が呼ぶ先] %s の module.%s が %s を呼びます。identity root が呼べるのは、bootstrap か統制の構成物を持つと宣言した usecase だけです（ADR-0604 決定3）→ workload root（env/%s/）から呼ぶ", [c.path, c.name, t, env_of(c.dir)])
}

deny contains msg if {
	some c in root_calls
	root_kind(c.dir) == "identity"
	env_of(c.dir) != management_env
	t := hcl.module_target(c.dir, c.source)
	declared_kind(t) == "control"
	msg := sprintf("RT102 [統制の usecase] %s の module.%s が統制の構成物を持つ %s を呼びます。呼べるのは管理アカウント（%v）の identity root だけです（ADR-0604 決定3）", [c.path, c.name, t, management_env])
}

deny contains msg if {
	some c in root_calls
	root_kind(c.dir) == "workload"
	t := hcl.module_target(c.dir, c.source)
	kind := declared_kind(t)
	msg := sprintf("RT103 [workload root が呼ぶ先] %s の module.%s が %s の構成物を持つ %s を呼びます（ADR-0604 決定4）→ identity root（env/%s/identity/）から呼ぶ", [c.path, c.name, kind, t, env_of(c.dir)])
}

declared_kind(t) := declared[usecase_of(t)] if is_object(declared)

# --- backend（ADR-0604 決定7・8） ---

deny contains msg if {
	some d in roots
	n := count([b | some b in backends; b.dir == d])
	n != 1
	msg := sprintf("RT201 [backend の数] root %s の backend が %d 件です（1件を期待）→ %s/backend.tf に backend \"s3\" を1つ置く", [d, n, d])
}

deny contains msg if {
	some b in backends
	root_kind(b.dir)
	b.path != sprintf("%s/backend.tf", [b.dir])
	msg := sprintf("RT201 [backend の置き場] %s が backend を持ちます → %s/backend.tf に置く", [b.path, b.dir])
}

deny contains msg if {
	some b in backends
	root_kind(b.dir)
	b.type != "s3"
	msg := sprintf("RT201 [backend の種類] %s の backend が %s です（s3 を期待）", [b.path, b.type])
}

deny contains msg if {
	some c in cloud_blocks
	root_kind(c.dir)
	msg := sprintf("RT201 [cloud block] %s が terraform の cloud block を持ちます。state は S3 backend に置きます（ADR-0604 決定7）", [c.path])
}

root_s3 := [b | some b in backends; root_kind(b.dir); b.type == "s3"]

deny contains msg if {
	some b in root_s3
	want := expected_key[root_kind(b.dir)]
	not literal_equals(b.attrs, "key", want)
	msg := sprintf("RT202 [backend の key] %s の key が %s のリテラルではありません（ADR-0604 決定7）", [b.path, want])
}

literal_equals(attrs, name, want) if {
	literal(attrs[name])
	attrs[name].value == want
}

deny contains msg if {
	some b in root_s3
	not literal_equals(b.attrs, "use_lockfile", true)
	msg := sprintf("RT203 [lock file] %s の use_lockfile が真偽値のリテラル true ではありません（ADR-0604 決定8）", [b.path])
}

deny contains msg if {
	some b in root_s3
	not literal_equals(b.attrs, "encrypt", true)
	msg := sprintf("RT204 [暗号化] %s の encrypt が真偽値のリテラル true ではありません（ADR-0604 決定7）", [b.path])
}

deny contains msg if {
	some b in root_s3
	has_attr(b, "dynamodb_table")
	msg := sprintf("RT205 [DynamoDB の lock] %s が dynamodb_table を持ちます。lock は lock file で取ります（ADR-0604 決定8）→ dynamodb_table を消す", [b.path])
}

deny contains msg if {
	some b in root_s3
	has_attr(b, "kms_key_id")
	msg := sprintf("RT206 [鍵の書き方] %s が kms_key_id を書きます → 書かず、init の -backend-config で渡す（ADR-0604 決定7。make の init が state の鍵と照合した値を渡す）", [b.path])
}
