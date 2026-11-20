package root

# fixture は conftest/hcl/root/fixtures の txtar を hcl-input fixtures が JSON にしたもの（data.fixtures）。
fx(name) := data.fixtures[name]

fixture_decl := {
	"management_env": "mgmt",
	"usecases": {"b": "bootstrap", "c": "control"},
}

ok_tree := fx("ok_tree")

empty_doc := {"files": [], "blocks": []}

listed_of(doc) := [f.path | some f in doc.files]

parsed_of(doc) := [f.path | some f in doc.files; f.parsed]

counts_of(doc) := {p: hcl.module_count(doc, p) | some p in parsed_of(doc); hcl.is_tf(p)}

blocks_of(doc) := {p: count([b | some b in doc.blocks; b.file == p]) | some p in parsed_of(doc)}

denies_with(doc, listed, counts, d) := result if {
	result := deny with input as doc
		with data.root_decl as d
		with data.root_files as listed
		with data.root_module_counts as counts
		with data.hcl_parsed_files as parsed_of(doc)
		with data.hcl_block_counts as blocks_of(doc)
}

denies(doc) := denies_with(doc, listed_of(doc), counts_of(doc), fixture_decl)

denies_decl(d) := denies_with(ok_tree, listed_of(ok_tree), counts_of(ok_tree), d)

with_id(msgs, id) := {m | some m in msgs; startswith(m, concat("", [id, " "]))}

import data.lib.hcl_input as hcl


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
	msgs := denies_with(empty_doc, [], {}, fixture_decl)
	some m in with_id(msgs, "RT002")
	contains(m, "対象が0件")
}

test_RT002_deny_no_root if {
	msgs := denies(fx("RT002_deny_no_root"))
	some m in with_id(msgs, "RT002")
	contains(m, "root が0件")
}

test_RT002_deny_not_read if {
	msgs := denies_with(ok_tree, array.concat(listed_of(ok_tree), ["env/mem/extra.tf"]), counts_of(ok_tree), fixture_decl)
	some m in with_id(msgs, "RT002")
	contains(m, "env/mem/extra.tf を入力（hcl-input）が読んでいません")
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
	count(with_id(denies(fx("RT003_allow_override_outside_root")), "RT003")) == 0
}

test_RT003_deny_tf_json if {
	some m in with_id(denies(fx("RT003_deny_tf_json")), "RT003")
	contains(m, "env/mem/extra.tf.json")
}

test_RT003_deny_override if {
	some m in with_id(denies(fx("RT003_deny_override")), "RT003")
	contains(m, "env/mem/override.tf")
}

test_RT003_deny_suffixed_override if {
	some m in with_id(denies(fx("RT003_deny_suffixed_override")), "RT003")
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
	count(module_calls) == 4 with input as ok_tree with data.hcl_parsed_files as parsed_of(ok_tree) with data.hcl_block_counts as blocks_of(ok_tree)
	count(backends) == 3 with input as ok_tree with data.hcl_parsed_files as parsed_of(ok_tree) with data.hcl_block_counts as blocks_of(ok_tree)
	count(roots) == 3 with input as ok_tree with data.hcl_parsed_files as parsed_of(ok_tree) with data.hcl_block_counts as blocks_of(ok_tree)
	doc := fx("RT002_allow_reads_every_module_and_backend")
	count(cloud_blocks) == 1 with input as doc with data.hcl_parsed_files as parsed_of(doc) with data.hcl_block_counts as blocks_of(doc)
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
	some m in with_id(denies(fx("RT003_deny_tf_json_outside_root")), "RT003")
	contains(m, "usecases/w/extra.tf.json")
}

# --- RT005 ---

test_RT005_allow_matching_input if {
	count(with_id(denies(ok_tree), "RT005")) == 0
}

test_RT005_deny_malformed_input_and_skip_rules_reading_it if {
	msgs := deny with input as {"files": [], "blocks": [{"file": "env/mem/main.tf"}]}
		with data.root_decl as fixture_decl
		with data.root_files as ["env/mem/main.tf"]
		with data.root_module_counts as {}
		with data.hcl_parsed_files as []
		with data.hcl_block_counts as {}
	some m in with_id(msgs, "RT005")
	contains(m, "blocks[0] の形が違います")
	count(with_id(msgs, "RT002")) == 0
	count(with_id(msgs, "RT004")) == 0
	msgs == with_id(msgs, "RT005")
}

test_RT005_deny_count_mismatch if {
	msgs := deny with input as ok_tree
		with data.root_decl as fixture_decl
		with data.root_files as listed_of(ok_tree)
		with data.root_module_counts as counts_of(ok_tree)
		with data.hcl_parsed_files as array.concat(parsed_of(ok_tree), ["env/mem/gone.tf"])
		with data.hcl_block_counts as blocks_of(ok_tree)
	with_id(msgs, "RT005") == {"RT005 [入力の形] env/mem/gone.tf を構文として読んでいません（find は列挙しています） → make root-check から起動する。hcl-input の出力と conftest/hcl/lib/hcl_input.rego の読む形を揃える"}
}

# fixture_cases は fixtures の txtar のケース名すべて。ケースが欠けるか空なら、それを使うテストが空振りしないよう、ここで落とす。
fixture_cases := {
	"RT002_allow_reads_every_module_and_backend",
	"RT002_deny_no_root",
	"RT003_allow_override_outside_root",
	"RT003_deny_override",
	"RT003_deny_suffixed_override",
	"RT003_deny_tf_json",
	"RT003_deny_tf_json_outside_root",
	"RT101_deny_identity_calls_general_module",
	"RT101_deny_identity_calls_undeclared_usecase",
	"RT102_allow_workload_root_calls_control",
	"RT102_deny_member_calls_control",
	"RT103_deny_workload_calls_bootstrap",
	"RT103_deny_workload_calls_control",
	"RT104_allow_non_root_remote_source",
	"RT104_deny_escaping_source",
	"RT104_deny_interpolated_source",
	"RT104_deny_missing_source",
	"RT104_deny_remote_source",
	"RT201_allow_backend_outside_root_positions",
	"RT201_allow_terraform_block_without_backend",
	"RT201_deny_backend_missing",
	"RT201_deny_backend_outside_backend_tf",
	"RT201_deny_cloud_block",
	"RT201_deny_non_s3_backend",
	"RT201_deny_non_s3_backend_in_identity_root",
	"RT201_deny_two_backends",
	"RT201_deny_two_terraform_blocks_each_with_backend",
	"RT202_deny_state_path_expression",
	"RT202_deny_state_path_null",
	"RT202_deny_state_path_number",
	"RT202_deny_state_path_omitted",
	"RT202_deny_workload_state_path_in_identity_root",
	"RT202_deny_wrong_state_path",
	"RT203_deny_lockfile_expression",
	"RT203_deny_lockfile_false",
	"RT203_deny_lockfile_missing",
	"RT203_deny_lockfile_string",
	"RT204_deny_encrypt_expression",
	"RT204_deny_encrypt_false",
	"RT204_deny_encrypt_missing",
	"RT204_deny_encrypt_string",
	"RT205_deny_dynamodb_table",
	"RT205_deny_dynamodb_table_null",
	"RT206_allow_backend_outside_root",
	"RT206_deny_empty_kms_key_id",
	"RT206_deny_kms_key_id",
	"ok_tree",
}

test_fixtures_present if {
	every name in fixture_cases {
		count(fx(name).files) > 0
	}
	{k | some k, _ in data.fixtures; not startswith(k, "lib_")} == fixture_cases
}
