package root

tf(path, src) := {"path": path, "contents": parse_config("hcl2", src)}

fixture_decl := {
	"management_env": "mgmt",
	"usecases": {"b": "bootstrap", "c": "control"},
}

backend_src(key) := sprintf(`terraform {
	backend "s3" {
		key          = "%s"
		region       = "ap-northeast-1"
		use_lockfile = true
		encrypt      = true
	}
}
`, [key])

identity_backend := backend_src("identity/terraform.tfstate")

workload_backend := backend_src("workload/terraform.tfstate")

call(name, source) := sprintf("module \"%s\" {\n  source = \"%s\"\n}\n", [name, source])

ok_tree := [
	tf("usecases/b/main.tf", "resource \"aws_s3_bucket\" \"this\" {}\n"),
	tf("usecases/c/main.tf", "resource \"aws_organizations_organization\" \"this\" {}\n"),
	tf("usecases/w/main.tf", "resource \"aws_sqs_queue\" \"this\" {}\n"),
	tf("env/mgmt/identity/backend.tf", identity_backend),
	tf("env/mgmt/identity/main.tf", concat("\n", [call("b", "../../../usecases/b"), call("c", "../../../usecases/c")])),
	tf("env/mem/identity/backend.tf", identity_backend),
	tf("env/mem/identity/main.tf", call("b", "../../../usecases/b")),
	tf("env/mem/backend.tf", workload_backend),
	tf("env/mem/main.tf", call("w", "../../usecases/w")),
]

listed_of(files) := [f.path | some f in files]

counts_of(files) := {f.path: module_count(f) | some f in files; hcl.is_tf(f.path)}

denies_with(files, listed, counts, d) := result if {
	result := deny with input as files
		with data.root_decl as d
		with data.root_files as listed
		with data.root_module_counts as counts
}

denies(files) := denies_with(files, listed_of(files), counts_of(files), fixture_decl)

denies_decl(d) := denies_with(ok_tree, listed_of(ok_tree), counts_of(ok_tree), d)

with_id(msgs, id) := {m | some m in msgs; startswith(m, concat("", [id, " "]))}

# replaced は、files の path のファイルを src に差し替える（無ければ足す）。
replaced(files, path, src) := array.concat([f | some f in files; f.path != path], [tf(path, src)])

without(files, path) := [f | some f in files; f.path != path]

import data.lib.hcl

# --- RT001 ---

test_RT001_allow_declared if {
	count(denies(ok_tree)) == 0
}

test_RT001_deny_decl_not_object if {
	msgs := denies_with(ok_tree, listed_of(ok_tree), counts_of(ok_tree), [])
	some m in with_id(msgs, "RT001")
	contains(m, "data.root_decl がオブジェクトではありません")
}

test_RT001_deny_unknown_key if {
	msgs := denies_decl(object.union(fixture_decl, {"extra": 1}))
	some m in with_id(msgs, "RT001")
	contains(m, "extra")
}

test_RT001_deny_management_env_missing if {
	msgs := denies_decl({"usecases": fixture_decl.usecases})
	some m in with_id(msgs, "RT001")
	contains(m, "management_env が env の名前")
}

test_RT001_deny_management_env_not_token if {
	msgs := denies_decl(object.union(fixture_decl, {"management_env": "Mgmt/x"}))
	some m in with_id(msgs, "RT001")
	contains(m, "management_env が env の名前")
}

test_RT001_deny_usecases_not_object if {
	msgs := denies_decl({"management_env": "mgmt", "usecases": ["b"]})
	some m in with_id(msgs, "RT001")
	contains(m, "usecases がオブジェクトではありません")
}

test_RT001_deny_unknown_kind if {
	msgs := denies_decl({"management_env": "mgmt", "usecases": {"b": "bootstrap", "c": "workload"}})
	some m in with_id(msgs, "RT001")
	contains(m, "usecases.c")
}

test_RT001_deny_no_bootstrap if {
	msgs := denies_decl({"management_env": "mgmt", "usecases": {"c": "control"}})
	some m in with_id(msgs, "RT001")
	contains(m, "bootstrap の usecase が1つもありません")
}

test_RT001_deny_empty_usecases if {
	msgs := denies_decl({"management_env": "mgmt", "usecases": {}})
	some m in with_id(msgs, "RT001")
	contains(m, "bootstrap の usecase が1つもありません")
}

# --- RT002 ---

test_RT002_allow_counts_match if {
	count(with_id(denies(ok_tree), "RT002")) == 0
}

test_RT002_deny_no_files if {
	msgs := denies_with([], [], {}, fixture_decl)
	some m in with_id(msgs, "RT002")
	contains(m, "対象が0件")
}

test_RT002_deny_no_root if {
	files := [f | some f in ok_tree; startswith(f.path, "usecases/")]
	msgs := denies(files)
	some m in with_id(msgs, "RT002")
	contains(m, "root が0件")
}

test_RT002_deny_not_read if {
	msgs := denies_with(ok_tree, array.concat(listed_of(ok_tree), ["env/mem/extra.tf"]), counts_of(ok_tree), fixture_decl)
	some m in with_id(msgs, "RT002")
	contains(m, "env/mem/extra.tf を Conftest が読んでいません")
}

test_RT002_deny_not_listed if {
	msgs := denies_with(ok_tree, without_path(listed_of(ok_tree), "env/mem/main.tf"), counts_of(ok_tree), fixture_decl)
	some m in with_id(msgs, "RT002")
	contains(m, "env/mem/main.tf は独立の走査の一覧にありません")
}

without_path(paths, p) := [q | some q in paths; q != p]

# module block の取り出しが0件になっても、独立の走査の件数と食い違って落ちる。
test_RT002_deny_module_count_mismatch if {
	counts := object.union(counts_of(ok_tree), {"env/mgmt/identity/main.tf": 3})
	msgs := denies_with(ok_tree, listed_of(ok_tree), counts, fixture_decl)
	some m in with_id(msgs, "RT002")
	contains(m, "env/mgmt/identity/main.tf の module block を 2 件読みましたが、独立の走査は 3 件です")
}

test_RT002_deny_module_count_missing if {
	counts := object.remove(counts_of(ok_tree), ["env/mem/main.tf"])
	msgs := denies_with(ok_tree, listed_of(ok_tree), counts, fixture_decl)
	some m in with_id(msgs, "RT002")
	contains(m, "env/mem/main.tf の module block")
	contains(m, "数えていない")
}

# --- RT003 ---

test_RT003_allow_override_outside_root if {
	files := array.concat(ok_tree, [tf("usecases/w/override.tf", "locals {}\n")])
	count(with_id(denies(files), "RT003")) == 0
}

test_RT003_deny_tf_json if {
	files := array.concat(ok_tree, [{"path": "env/mem/extra.tf.json", "contents": {}}])
	some m in with_id(denies(files), "RT003")
	contains(m, "env/mem/extra.tf.json")
}

test_RT003_deny_override if {
	files := array.concat(ok_tree, [tf("env/mem/override.tf", "locals {}\n")])
	some m in with_id(denies(files), "RT003")
	contains(m, "env/mem/override.tf")
}

test_RT003_deny_suffixed_override if {
	files := array.concat(ok_tree, [tf("env/mgmt/identity/backend_override.tf", "locals {}\n")])
	some m in with_id(denies(files), "RT003")
	contains(m, "env/mgmt/identity/backend_override.tf")
}

# --- RT004 ---

test_RT004_allow_declared_usecases_exist if {
	count(with_id(denies(ok_tree), "RT004")) == 0
}

test_RT004_deny_declared_usecase_missing if {
	msgs := denies_decl({"management_env": "mgmt", "usecases": {"b": "bootstrap", "c": "control", "typo": "control"}})
	some m in with_id(msgs, "RT004")
	contains(m, "usecases.typo")
}

test_RT004_deny_management_identity_missing if {
	msgs := denies_decl(object.union(fixture_decl, {"management_env": "other"}))
	some m in with_id(msgs, "RT004")
	contains(m, "env/other/identity")
}

# conftest verify は木の中の decl.json も data に読むので、未定義ではなく null で宣言の欠落を表す。
test_RT001_deny_decl_null if {
	msgs := denies_with(ok_tree, listed_of(ok_tree), counts_of(ok_tree), null)
	some m in with_id(msgs, "RT001")
	contains(m, "data.root_decl がオブジェクトではありません")
}

test_RT001_deny_management_env_number if {
	msgs := denies_decl(object.union(fixture_decl, {"management_env": 1}))
	some m in with_id(msgs, "RT001")
	contains(m, "management_env が env の名前")
}

test_RT001_deny_kind_not_string if {
	msgs := denies_decl({"management_env": "mgmt", "usecases": {"b": "bootstrap", "c": 1}})
	some m in with_id(msgs, "RT001")
	contains(m, "usecases.c")
}

# 取り出しが空になっても allow のテストは通るので、ok_tree から読む数をここで固定する。
test_RT002_allow_reads_every_module_and_backend if {
	count(module_calls) == 4 with input as ok_tree
	count(backends) == 3 with input as ok_tree
	count(roots) == 3 with input as ok_tree
	count(cloud_blocks) == 1 with input as array.concat(ok_tree, [tf("env/mem/cloud.tf", "terraform {\n  cloud {\n    organization = \"x\"\n  }\n}\n")])
}

test_RT002_deny_root_files_not_array if {
	msgs := denies_with(ok_tree, {"x": 1}, counts_of(ok_tree), fixture_decl)
	some m in with_id(msgs, "RT002")
	contains(m, "対象が0件")
}

test_RT002_deny_module_counts_not_object if {
	msgs := denies_with(ok_tree, listed_of(ok_tree), [], fixture_decl)
	some m in with_id(msgs, "RT002")
	contains(m, "env/mgmt/identity/main.tf の module block")
}

test_RT003_deny_tf_json_outside_root if {
	files := array.concat(ok_tree, [{"path": "usecases/w/extra.tf.json", "contents": {}}])
	some m in with_id(denies(files), "RT003")
	contains(m, "usecases/w/extra.tf.json")
}
