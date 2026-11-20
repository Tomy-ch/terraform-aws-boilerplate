package resource_order

rule := {
	"module": "m",
	"resource": "aws_iam_role.this",
	"after": "aws_iam_role_policy.first",
	"adr": "docs/adr/x.md",
}

# fixture は conftest/hcl/resource_order/fixtures の txtar を hcl-input fixtures が JSON にしたもの（data.fixtures）。
fx(name) := data.fixtures[name]

empty_doc := {"files": [], "blocks": []}

parsed_of(doc) := [f.path | some f in doc.files; f.parsed]

counts_of(doc) := {p: count([b | some b in doc.blocks; b.file == p]) | some p in parsed_of(doc)}

denies(doc, listed, rules) := result if {
	result := deny with input as doc
		with data.resource_order_decl as {"rules": rules}
		with data.resource_order_files as listed
		with data.hcl_parsed_files as parsed_of(doc)
		with data.hcl_block_counts as counts_of(doc)
}

with_id(msgs, id) := {m | some m in msgs; startswith(m, concat("", [id, " "]))}

test_RO001_allow_declared_rule if {
	count(denies(fx("ok"), ["m/main.tf"], [rule])) == 0
}

test_RO001_deny_no_rules if {
	count(with_id(denies(fx("ok"), ["m/main.tf"], []), "RO001")) == 1
}

test_RO001_deny_rule_without_adr if {
	msgs := denies(fx("ok"), ["m/main.tf"], [object.remove(rule, ["adr"])])
	count(with_id(msgs, "RO001")) == 1
}

test_RO001_deny_address_with_module_prefix if {
	msgs := denies(fx("ok"), ["m/main.tf"], [object.union(rule, {"after": "module.x.aws_iam_role_policy.first"})])
	count(with_id(msgs, "RO001")) == 1
}

test_RO002_allow_tftest_not_listed if {
	count(denies(fx("RO002_allow_tftest_not_listed"), ["m/main.tf"], [rule])) == 0
}

test_RO002_deny_zero_files if {
	msgs := denies(empty_doc, [], [rule])
	some m in with_id(msgs, "RO002")
	contains(m, "1件もありません")
}

test_RO002_deny_listed_but_not_read if {
	msgs := denies(fx("ok"), ["m/main.tf", "m/extra.tf"], [rule])
	some m in with_id(msgs, "RO002")
	contains(m, "m/extra.tf を入力（hcl-input）が読んでいません")
}

test_RO002_deny_read_but_not_listed if {
	msgs := denies(fx("RO002_deny_read_but_not_listed"), ["m/main.tf"], [rule])
	some m in with_id(msgs, "RO002")
	contains(m, "n/main.tf は独立の走査の一覧にありません")
}

test_RO003_allow_module_in_nested_dir if {
	r := object.union(rule, {"module": "a/b/m"})
	count(denies(fx("RO003_allow_module_in_nested_dir"), ["a/b/m/main.tf"], [r])) == 0
}

test_RO003_deny_module_missing if {
	msgs := denies(fx("RO003_deny_module_missing"), ["other/main.tf"], [rule])
	count(with_id(msgs, "RO003")) == 1
	count(with_id(msgs, "RO004")) == 0
}

test_RO004_allow_resources_in_separate_files if {
	count(denies(fx("RO004_allow_resources_in_separate_files"), ["m/main.tf", "m/policy.tf"], [rule])) == 0
}

test_RO004_deny_resource_missing if {
	msgs := denies(fx("RO004_deny_resource_missing"), ["m/main.tf"], [rule])
	some m in with_id(msgs, "RO004")
	contains(m, "aws_iam_role.this が 0 件")
}

test_RO004_deny_after_missing if {
	msgs := denies(fx("RO004_deny_after_missing"), ["m/main.tf"], [rule])
	some m in with_id(msgs, "RO004")
	contains(m, "aws_iam_role_policy.first が 0 件")
}

test_RO004_deny_resource_in_other_module if {
	msgs := denies(fx("RO004_deny_resource_in_other_module"), ["m/main.tf", "n/main.tf"], [rule])
	count(with_id(msgs, "RO004")) == 1
}

test_RO005_allow_among_other_dependencies if {
	count(denies(fx("RO005_allow_among_other_dependencies"), ["m/main.tf"], [rule])) == 0
}

test_RO005_deny_without_depends_on if {
	msgs := denies(fx("RO005_deny_without_depends_on"), ["m/main.tf"], [rule])
	some m in with_id(msgs, "RO005")
	contains(m, "m/main.tf の aws_iam_role.this が depends_on に aws_iam_role_policy.first を持ちません")
}

test_RO005_deny_depends_on_other_resource if {
	msgs := denies(fx("RO005_deny_depends_on_other_resource"), ["m/main.tf"], [rule])
	count(with_id(msgs, "RO005")) == 1
}

test_RO001_deny_rules_not_array if {
	msgs := deny with input as fx("ok")
		with data.resource_order_decl as {"rules": rule}
		with data.resource_order_files as ["m/main.tf"]
		with data.hcl_parsed_files as ["m/main.tf"]
		with data.hcl_block_counts as {"m/main.tf": 2}
	count(with_id(msgs, "RO001")) == 1
}

test_RO001_deny_empty_value if {
	msgs := denies(fx("ok"), ["m/main.tf"], [object.union(rule, {"module": ""})])
	count(with_id(msgs, "RO001")) == 1
}

test_RO001_deny_rule_as_string if {
	msgs := denies(fx("ok"), ["m/main.tf"], ["m#aws_iam_role.this"])
	count(with_id(msgs, "RO001")) == 1
}

test_RO001_deny_extra_key if {
	msgs := denies(fx("ok"), ["m/main.tf"], [object.union(rule, {"note": "x"})])
	count(with_id(msgs, "RO001")) == 1
}

test_RO006_allow_hcl_only if {
	count(with_id(denies(fx("ok"), ["m/main.tf"], [rule]), "RO006")) == 0
}

test_RO006_deny_json_config if {
	msgs := denies(fx("RO006_deny_json_config"), ["m/main.tf", "m/override.tf.json"], [rule])
	some m in with_id(msgs, "RO006")
	contains(m, "m/override.tf.json")
	count(with_id(msgs, "RO002")) == 0
}

test_RO007_allow_matching_input if {
	count(with_id(denies(fx("ok"), ["m/main.tf"], [rule]), "RO007")) == 0
}

test_RO007_deny_malformed_input_and_skip_rules_reading_it if {
	msgs := deny with input as {"files": [{"path": "m/main.tf"}], "blocks": []}
		with data.resource_order_decl as {"rules": [rule]}
		with data.resource_order_files as ["m/main.tf"]
		with data.hcl_parsed_files as ["m/main.tf"]
		with data.hcl_block_counts as {"m/main.tf": 0}
	count(with_id(msgs, "RO007")) == 1
	msgs == with_id(msgs, "RO007")
}

test_RO007_deny_count_mismatch if {
	msgs := deny with input as fx("ok")
		with data.resource_order_decl as {"rules": [rule]}
		with data.resource_order_files as ["m/main.tf"]
		with data.hcl_parsed_files as ["m/main.tf"]
		with data.hcl_block_counts as {"m/main.tf": 3}
	with_id(msgs, "RO007") == {"RO007 [入力の形] m/main.tf のトップレベルの block を 2 個読みましたが、行頭の数えは 3 個です → make resource-order-check から起動する。hcl-input の出力と conftest/hcl/lib/hcl_input.rego の読む形を揃える"}
}

# fixture_cases は fixtures の txtar のケース名すべて。ケースが欠けるか空なら、それを使うテストが空振りしないよう、ここで落とす。
fixture_cases := {
	"RO002_allow_tftest_not_listed",
	"RO002_deny_read_but_not_listed",
	"RO003_allow_module_in_nested_dir",
	"RO003_deny_module_missing",
	"RO003_deny_module_with_shared_prefix",
	"RO004_allow_resources_in_separate_files",
	"RO004_deny_address_only_in_other_module",
	"RO004_deny_after_missing",
	"RO004_deny_duplicate_in_declared_module",
	"RO004_deny_resource_in_other_module",
	"RO004_deny_resource_missing",
	"RO005_allow_among_other_dependencies",
	"RO005_deny_depends_on_other_resource",
	"RO005_deny_without_depends_on",
	"RO006_deny_json_config",
	"ok",
}

test_fixtures_present if {
	every name in fixture_cases {
		count(fx(name).files) > 0
	}
	{k | some k, _ in data.fixtures; not startswith(k, "lib_")} == fixture_cases
}

test_RO004_deny_duplicate_in_declared_module if {
	msgs := denies(fx("RO004_deny_duplicate_in_declared_module"), ["m/dup.tf", "m/main.tf"], [rule])
	some m in with_id(msgs, "RO004")
	contains(m, "aws_iam_role.this が 2 件です")
}

test_RO004_deny_address_only_in_other_module if {
	msgs := denies(fx("RO004_deny_address_only_in_other_module"), ["m/main.tf", "n/main.tf"], [rule])
	some m in with_id(msgs, "RO004")
	contains(m, "m の resource aws_iam_role.this が 0 件です")
}

test_RO001_deny_address_with_three_parts_and_non_string_after if {
	count(with_id(denies(fx("ok"), ["m/main.tf"], [object.union(rule, {"resource": "a.b.c"})]), "RO001")) == 1
	count(with_id(denies(fx("ok"), ["m/main.tf"], [object.union(rule, {"after": 1})]), "RO001")) == 1
}

test_RO002_deny_files_list_absent_or_not_array if {
	doc := fx("ok")
	msgs := deny with input as doc
		with data.resource_order_decl as {"rules": [rule]}
		with data.hcl_parsed_files as parsed_of(doc)
		with data.hcl_block_counts as counts_of(doc)
	some m in with_id(msgs, "RO002")
	contains(m, "1件もありません")
	msgs2 := denies(doc, {"x": 1}, [rule])
	some m2 in with_id(msgs2, "RO002")
	contains(m2, "1件もありません")
}

# module は前方一致ではなくディレクトリの一致で選ぶ。
test_RO003_deny_module_with_shared_prefix if {
	msgs := denies(fx("RO003_deny_module_with_shared_prefix"), ["modules/ab/main.tf"], [object.union(rule, {"module": "modules/a"})])
	some m in with_id(msgs, "RO003")
	contains(m, "modules/a に構成ファイルがありません")
}
