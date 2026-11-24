package layer

import data.lib.hcl_input as hcl

# fixture は conftest/hcl/layer/fixtures の txtar を hcl-input fixtures が JSON にしたもの（data.fixtures）。
# 独立の数は fixture そのものから作り、count_errors を素通しにする。突き合わせの規則は LY004 のテストが見る。
fx(name) := data.fixtures[name]

empty_doc := {"files": [], "blocks": []}

parsed_of(doc) := [f.path | some f in doc.files; f.parsed]

counts_of(doc) := {p: count([b | some b in doc.blocks; b.file == p]) | some p in parsed_of(doc)}

fixture_decl := {
	"attachments": {
		"aws_s3_bucket_policy": {"parents": {"bucket": ["aws_s3_bucket"]}, "optional": false, "parent_arguments": {"aws_s3_bucket": {"policy": null}}, "whole_document": true},
		"aws_flow_log": {"parents": {"vpc_id": ["aws_vpc"], "subnet_id": ["aws_subnet"]}, "optional": true, "parent_arguments": {}, "whole_document": false},
		"aws_ec2_tag": {"parents": {"resource_id": ["*"]}, "optional": false, "parent_arguments": {}, "whole_document": false},
		"aws_guardduty_malware_protection_plan": {"parents": {"protected_resource.s3_bucket.bucket_name": ["aws_s3_bucket"]}, "optional": false, "parent_arguments": {}, "whole_document": false},
		"aws_api_gateway_method_settings": {"parents": {"rest_api_id": ["aws_api_gateway_rest_api"], "stage_name": ["aws_api_gateway_stage"]}, "optional": false, "parent_arguments": {}, "whole_document": false},
		"aws_kms_alias": {"parents": {"target_key_id": ["aws_kms_key", "aws_kms_replica_key"]}, "optional": false, "parent_arguments": {}, "whole_document": false},
	},
	"not_attachments": {
		"aws_lb_listener": "子 resource",
		"aws_s3_bucket": "親になる resource",
		"aws_vpc": "親になる resource",
		"aws_subnet": "子 resource",
		"aws_api_gateway_rest_api": "親になる resource",
		"aws_api_gateway_stage": "子 resource",
		"aws_kms_key": "親になる resource",
		"aws_kms_replica_key": "親になる resource",
		"aws_s3_account_public_access_block": "アカウントの単位で設定する",
	},
	"singletons": {"aws_s3_account_public_access_block": ["account-baseline"]},
	"unrelated_deprecated_arguments": {"aws_s3_bucket": {"acceleration_status": {"reason": "fixture", "unwritten": null}}},
	"inline_owned": {},
}

fixture_schema := object.union(
	{t: {"arguments": ["name"], "deprecated": [], "blocks": []} |
		some t in {"aws_vpc", "aws_subnet", "aws_api_gateway_rest_api", "aws_api_gateway_stage", "aws_kms_key", "aws_kms_replica_key"}
	},
	{"aws_s3_bucket": {"arguments": ["bucket", "policy", "acceleration_status", "versioning"], "deprecated": ["policy", "acceleration_status"], "blocks": ["versioning"]}},
)

denies_raw(doc, listed, d, parsed, counts) := result if {
	result := deny with input as doc
		with data.layer_decl as d
		with data.layer_files as listed
		with data.hcl_parsed_files as parsed
		with data.hcl_block_counts as counts
		with data.layer_schema as fixture_schema
}

denies_with(doc, listed, d) := denies_raw(doc, listed, d, parsed_of(doc), counts_of(doc))

# listed_of は、find が列挙する構成ファイル（.tf と .tf.json）の一覧を返す。.tftest.hcl は入らない。
listed_of(doc) := [f.path | some f in doc.files; is_listed(f.path)]

is_listed(p) if hcl.is_tf(p)

is_listed(p) if hcl.is_tf_json(p)

denies(doc) := denies_with(doc, listed_of(doc), fixture_decl)

with_id(msgs, id) := {m | some m in msgs; startswith(m, concat("", [id, " "]))}

# only_with は、msgs がちょうど1つで、その文言が s を含むかを返す。
only_with(msgs, s) if {
	count(msgs) == 1
	some m in msgs
	contains(m, s)
}

test_LY001_allow_declared if {
	count(denies(fx("ok_module"))) == 0
}

test_LY001_deny_attachments_missing if {
	msgs := denies_with(fx("ok_module"), listed_of(fx("ok_module")), {"not_attachments": fixture_decl.not_attachments, "singletons": fixture_decl.singletons, "unrelated_deprecated_arguments": {}, "inline_owned": {}})
	only_with(with_id(msgs, "LY001"), "data.layer_decl.attachments がオブジェクトではありません")
}

test_LY001_deny_attachments_empty if {
	msgs := denies_with(fx("ok_module"), listed_of(fx("ok_module")), {"attachments": {}, "not_attachments": fixture_decl.not_attachments, "singletons": fixture_decl.singletons, "unrelated_deprecated_arguments": {}, "inline_owned": {}})
	only_with(with_id(msgs, "LY001"), "data.layer_decl.attachments が空です")
}

test_LY001_deny_attachments_array if {
	msgs := denies_with(fx("ok_module"), listed_of(fx("ok_module")), {"attachments": [], "not_attachments": fixture_decl.not_attachments, "singletons": fixture_decl.singletons, "unrelated_deprecated_arguments": {}, "inline_owned": {}})
	only_with(with_id(msgs, "LY001"), "data.layer_decl.attachments がオブジェクトではありません")
}

test_LY001_deny_parents_empty if {
	d := object.union(fixture_decl, {"attachments": {"aws_x": {"parents": {}, "optional": false}}})
	msgs := denies_with(fx("ok_module"), listed_of(fx("ok_module")), d)
	only_with(with_id(msgs, "LY001"), "attachments.aws_x")
}

test_LY001_deny_parent_types_empty if {
	d := object.union(fixture_decl, {"attachments": {"aws_x": {"parents": {"a": []}, "optional": false}}})
	msgs := denies_with(fx("ok_module"), listed_of(fx("ok_module")), d)
	only_with(with_id(msgs, "LY001"), "attachments.aws_x")
}

test_LY001_deny_parents_as_list if {
	d := object.union(fixture_decl, {"attachments": {"aws_x": {"parents": ["a"], "optional": false}}})
	msgs := denies_with(fx("ok_module"), listed_of(fx("ok_module")), d)
	only_with(with_id(msgs, "LY001"), "attachments.aws_x")
}

test_LY001_deny_optional_not_boolean if {
	d := object.union(fixture_decl, {"attachments": {"aws_x": {"parents": {"a": ["aws_y"]}, "optional": "false"}}})
	msgs := denies_with(fx("ok_module"), listed_of(fx("ok_module")), d)
	only_with(with_id(msgs, "LY001"), "attachments.aws_x")
}

test_LY001_deny_extra_key if {
	d := object.union(fixture_decl, {"attachments": {"aws_x": {"parents": {"a": ["aws_y"]}, "optional": false, "skip": true}}})
	msgs := denies_with(fx("ok_module"), listed_of(fx("ok_module")), d)
	only_with(with_id(msgs, "LY001"), "attachments.aws_x")
}

test_LY001_deny_not_attachments_missing if {
	msgs := denies_with(fx("ok_module"), listed_of(fx("ok_module")), object.remove(fixture_decl, ["not_attachments"]))
	only_with(with_id(msgs, "LY001"), "not_attachments がオブジェクトではありません")
}

test_LY001_deny_empty_reason if {
	d := object.union(fixture_decl, {"not_attachments": {"aws_lb_listener": ""}})
	msgs := denies_with(fx("ok_module"), listed_of(fx("ok_module")), d)
	only_with(with_id(msgs, "LY001"), "not_attachments.aws_lb_listener")
}

test_LY001_deny_type_in_both if {
	d := object.union(fixture_decl, {"not_attachments": {"aws_s3_bucket_policy": "子 resource"}})
	msgs := denies_with(fx("ok_module"), listed_of(fx("ok_module")), d)
	only_with(with_id(msgs, "LY001"), "両方")
}

test_LY001_deny_decl_not_object if {
	msgs := denies_with(fx("ok_module"), listed_of(fx("ok_module")), "x")
	with_id(msgs, "LY001") == {
		"LY001 [宣言の形] data.layer_decl.attachments がオブジェクトではありません",
		"LY001 [宣言の形] data.layer_decl.not_attachments がオブジェクトではありません",
		"LY001 [宣言の形] data.layer_decl.singletons がオブジェクトではありません",
		"LY001 [宣言の形] data.layer_decl.unrelated_deprecated_arguments が、親の型から、引数と reason（空でない理由）・unwritten（null か []）へのオブジェクトではありません",
		"LY001 [宣言の形] data.layer_decl.inline_owned が、親の型から、引数と空でない理由へのオブジェクトではありません",
	}
}

test_LY001_deny_entry_not_object if {
	d := object.union(fixture_decl, {"attachments": {"aws_x": "bucket"}})
	only_with(with_id(denies_with(fx("ok_module"), listed_of(fx("ok_module")), d), "LY001"), "attachments.aws_x")
}

test_LY001_deny_empty_argument_name if {
	d := object.union(fixture_decl, {"attachments": {"aws_x": {"parents": {"": ["aws_y"]}, "optional": false}}})
	only_with(with_id(denies_with(fx("ok_module"), listed_of(fx("ok_module")), d), "LY001"), "attachments.aws_x")
}

test_LY001_deny_empty_parent_type if {
	d := object.union(fixture_decl, {"attachments": {"aws_x": {"parents": {"a": [""]}, "optional": false}}})
	only_with(with_id(denies_with(fx("ok_module"), listed_of(fx("ok_module")), d), "LY001"), "attachments.aws_x")
}

test_LY001_deny_parent_types_as_string if {
	d := object.union(fixture_decl, {"attachments": {"aws_x": {"parents": {"a": "aws_y"}, "optional": false}}})
	only_with(with_id(denies_with(fx("ok_module"), listed_of(fx("ok_module")), d), "LY001"), "attachments.aws_x")
}

test_LY002_deny_files_list_missing if {
	doc := fx("ok_module")
	msgs := deny with input as doc with data.layer_decl as fixture_decl with data.layer_schema as fixture_schema
		with data.hcl_parsed_files as parsed_of(doc) with data.hcl_block_counts as counts_of(doc)
	some m in with_id(msgs, "LY002")
	contains(m, "modules/m/main.tf は独立の走査の一覧にありません")
}

# 配列でない一覧は0件の一覧と同じ文言で落ちる。読んだファイルが一覧に無いことの LY002 も並ぶので、
# 0件の一覧（test_LY002_deny_zero_files）とは件数で区別する。
test_LY002_deny_files_list_not_array if {
	doc := fx("ok_module")
	msgs := denies_raw(doc, "x", fixture_decl, parsed_of(doc), counts_of(doc))
	with_id(msgs, "LY002") == {
		"LY002 [対象が0件] 走査の root に Terraform の構成ファイル（.tf と .tf.json）が1件もありません",
		"LY002 [件数の不一致] modules/m/main.tf は独立の走査の一覧にありません",
	}
}

test_LY002_allow_tftest_not_listed if {
	count(denies_with(fx("LY002_allow_tftest_not_listed"), listed_of(fx("LY002_allow_tftest_not_listed")), fixture_decl)) == 0
}

test_LY002_deny_zero_files if {
	some m in with_id(denies_with(empty_doc, [], fixture_decl), "LY002")
	contains(m, "1件もありません")
}

test_LY002_deny_listed_but_not_read if {
	msgs := denies_with(fx("ok_module"), ["modules/m/main.tf", "modules/m/extra.tf"], fixture_decl)
	some m in with_id(msgs, "LY002")
	contains(m, "modules/m/extra.tf を入力（hcl-input）が読んでいません")
}

test_LY002_deny_read_but_not_listed if {
	msgs := denies_with(fx("LY002_deny_read_but_not_listed"), ["modules/m/main.tf"], fixture_decl)
	some m in with_id(msgs, "LY002")
	contains(m, "modules/n/main.tf は独立の走査の一覧にありません")
}

test_LY003_allow_hcl_only if {
	count(with_id(denies(fx("ok_module")), "LY003")) == 0
}

test_LY003_deny_tf_json if {
	some m in with_id(denies(fx("LY003_deny_tf_json")), "LY003")
	contains(m, "modules/m/override.tf.json")
}

test_LY002_allow_listed_matches_read if {
	count(with_id(denies(fx("ok_module")), "LY002")) == 0
}

# --- LY004（入力の形と、独立の経路の数との突き合わせ。ADR-0702 決定15） ---

ly004(doc, parsed, counts) := with_id(denies_raw(doc, listed_of(fx("ok_module")), fixture_decl, parsed, counts), "LY004")

ok_ly004(doc) := ly004(doc, parsed_of(doc), counts_of(doc))

test_LY004_allow_well_formed_input if {
	count(ok_ly004(fx("refs_shapes"))) == 0
}

test_LY004_deny_not_object if {
	only_with(ly004([], [], {}), "入力がオブジェクトではありません")
}

test_LY004_deny_missing_independent_counts if {
	doc := fx("ok_module")
	all := deny with input as doc with data.layer_decl as fixture_decl with data.layer_files as ["modules/m/main.tf"] with data.layer_schema as fixture_schema
	msgs := with_id(all, "LY004")
	count(msgs) == 2
	some m1 in msgs
	contains(m1, "hcl_parsed_files（find の一覧）が配列ではありません")
	some m2 in msgs
	contains(m2, "hcl_block_counts（行頭の数え）がオブジェクトではありません")
}

# 構文として読んだファイルの集合を、find の一覧と両向きに突き合わせる。
test_LY004_deny_parsed_set_mismatch if {
	doc := fx("ok_module")
	only_ly004(denies_raw(doc, listed_of(doc), fixture_decl, ["modules/m/main.tf", "modules/m/extra.tf"], counts_of(doc)), "modules/m/extra.tf を構文として読んでいません")
	only_ly004(denies_raw(doc, listed_of(doc), fixture_decl, [], counts_of(doc)), "modules/m/main.tf を構文として読みましたが、find の一覧にありません")
}

# only_ly004 は、deny がちょうど1つで、それが s を含む LY004 かを返す。独立の数と食い違う入力の上では、ほかの規則を評価しない。
only_ly004(msgs, s) if {
	only_with(msgs, s)
	count(with_id(msgs, "LY004")) == 1
}

test_LY004_deny_block_count_mismatch if {
	doc := fx("ok_module")
	only_ly004(denies_raw(doc, listed_of(doc), fixture_decl, parsed_of(doc), {"modules/m/main.tf": 2}), "modules/m/main.tf のトップレベルの block を 1 個読みましたが、行頭の数えは 2 個です")
}

# 参照の列の形（根・段・添字の形・within）は lib.hcl_input が見る。層の規則が辿る形なので、崩れれば LY004。
broken_ref(op) := ok_ly004(json.patch(fx("refs_shapes"), [op]))

test_LY004_deny_broken_refs if {
	every op in [
		{"op": "remove", "path": "/blocks/1/attributes/0/refs/0/within"},
		{"op": "replace", "path": "/blocks/1/attributes/0/refs/0/root", "value": ""},
		{"op": "replace", "path": "/blocks/1/attributes/0/refs/0/steps/0", "value": {"kind": "call"}},
		{"op": "replace", "path": "/blocks/1/attributes/0/refs/0/steps/0", "value": {"kind": "attr"}},
		{"op": "replace", "path": "/blocks/1/attributes/0/refs/0/steps/1", "value": {"kind": "index", "name": "x"}},
		{"op": "replace", "path": "/blocks/1/attributes/0/refs/0/steps/1", "value": {"kind": "index"}},
		{"op": "replace", "path": "/blocks/1/attributes/0/refs/0/steps/1/index/form", "value": "each"},
		{"op": "replace", "path": "/blocks/1/attributes/0/refs/1/within/0/step", "value": 0},
		{"op": "replace", "path": "/blocks/1/attributes/0/refs/1/within/0/ref", "value": 9},
	] {
		count(broken_ref(op)) == 1
	}
}

# 形の崩れた入力の上では、それを読む規則（LY002 の読んだ向き・LY1xx・LY2xx・LY3xx）を評価しない。
test_LY004_deny_alone_on_broken_input if {
	doc := json.patch(fx("refs_shapes"), [{"op": "replace", "path": "/blocks/1/attributes/0/refs/0/root", "value": ""}])
	msgs := denies_raw(doc, listed_of(doc), fixture_decl, parsed_of(doc), counts_of(doc))
	count(with_id(msgs, "LY004")) == 1
	{m | some m in msgs; not startswith(m, "LY004 ")} == set()
}

# fixture_cases は fixtures の txtar のケース名すべて。ケースが欠けるか空なら、それを使うテストが空振りしないよう、ここで落とす。
fixture_cases := {
	"LY002_allow_tftest_not_listed",
	"LY002_deny_read_but_not_listed",
	"LY003_deny_tf_json",
	"LY101_allow_resource_below_usecase",
	"LY101_deny_resource_in_usecase",
	"LY102_allow_provider_in_tests_and_reference",
	"LY102_deny_provider_in_usecase",
	"LY103_allow_env_root_only_blocks",
	"LY103_allow_required_providers_moved_removed",
	"LY103_deny_backend",
	"LY103_deny_cloud",
	"LY103_deny_import",
	"LY201_deny_git_source",
	"LY201_deny_github_shorthand",
	"LY201_deny_interpolated_relative_source_1",
	"LY201_deny_interpolated_relative_source_2",
	"LY201_deny_missing_source",
	"LY201_deny_non_string_source_1",
	"LY201_deny_non_string_source_2",
	"LY201_deny_registry_source",
	"LY201_deny_s3_source",
	"LY201_deny_url_source",
	"LY201_deny_variable_source",
	"LY202_deny_outside_root",
	"LY203_deny_env_to_env",
	"LY203_deny_env_to_identity_root",
	"LY203_deny_env_to_internal",
	"LY203_deny_env_to_local_module",
	"LY203_deny_env_to_module",
	"LY203_deny_env_to_reference",
	"LY203_deny_env_to_repository_root",
	"LY203_deny_internal_to_other_usecase_internal",
	"LY203_deny_internal_to_usecase",
	"LY203_deny_module_to_internal",
	"LY203_deny_module_to_unplaced_dir",
	"LY203_deny_module_to_usecase",
	"LY203_deny_reference_to_module",
	"LY203_deny_reference_to_other_usecase",
	"LY203_deny_reference_to_own_internal",
	"LY203_deny_usecase_to_env",
	"LY203_deny_usecase_to_other_internal",
	"LY203_deny_usecase_to_other_usecase",
	"LY203_deny_usecase_to_reference",
	"LY204_allow_module_dir_with_comment_only_tf",
	"LY204_deny_missing_target",
	"LY204_deny_source_dir_with_only_tftest",
	"LY205_allow_resource_only_in_unplaced_dir",
	"LY205_deny_call_from_deep_internal",
	"LY205_deny_call_from_env_dir_itself",
	"LY205_deny_call_from_nested_module_dir",
	"LY206_allow_chain",
	"LY206_deny_internal_cycle",
	"LY206_deny_module_cycle",
	"LY206_deny_self_call",
	"LY206_deny_three_node_cycle",
	"LY301_allow_any_parent_type_in_same_module",
	"LY301_allow_count_index_outside_subscript",
	"LY301_allow_direct_parent",
	"LY301_allow_local_defined_twice",
	"LY301_allow_local_in_sibling_file",
	"LY301_allow_parent_in_dynamic",
	"LY301_allow_parent_through_each_value",
	"LY301_allow_parent_through_locals_and_index",
	"LY301_allow_reference_in_first_index",
	"LY301_allow_reference_nested_in_first_index",
	"LY301_allow_second_parent_type",
	"LY301_allow_splat_and_key_index_1",
	"LY301_allow_splat_and_key_index_2",
	"LY301_deny_any_type_from_other_module",
	"LY301_deny_any_type_without_resource_in_module",
	"LY301_deny_data_source",
	"LY301_deny_each_inside_local",
	"LY301_deny_index_after_attribute",
	"LY301_deny_index_directly_after_root",
	"LY301_deny_literal",
	"LY301_deny_local_of_other_module",
	"LY301_deny_local_resolving_to_literal",
	"LY301_deny_module_reference",
	"LY301_deny_module_through_each_key",
	"LY301_deny_module_through_each_value",
	"LY301_deny_module_through_two_locals",
	"LY301_deny_nested_parent",
	"LY301_deny_one_of_several_parent_arguments",
	"LY301_deny_parent_in_dynamic",
	"LY301_deny_parent_in_other_module",
	"LY301_deny_parent_not_declared_in_module",
	"LY301_deny_root_only_reference",
	"LY301_deny_second_index_after_reference",
	"LY301_deny_undefined_local",
	"LY301_deny_variable",
	"LY301_deny_wrong_parent_type",
	"LY302_allow_one_of_several_required_parents",
	"LY302_allow_optional_without_parent",
	"LY302_deny_none_of_several_required_parents",
	"LY302_deny_required_parent_missing",
	"LY303_allow_classified_types",
	"LY303_deny_unclassified_type",
	"LY304_allow_argument_name_prefix",
	"LY304_allow_default_security_group",
	"LY304_allow_exclusive_with_individual_attachment",
	"LY304_allow_inline_owned",
	"LY304_allow_unrelated_argument",
	"LY304_deny_dynamic_inline_policy",
	"LY304_deny_dynamic_without_attributes",
	"LY304_deny_empty_list",
	"LY304_deny_inline_policy_block",
	"LY304_deny_nested_attribute",
	"LY304_deny_null_attribute",
	"LY304_deny_only_resource_that_writes",
	"LY305_allow_conditional_different_leaf",
	"LY305_allow_distinct_exclusive_types",
	"LY305_allow_distinct_literals",
	"LY305_allow_distinct_parents",
	"LY305_allow_distinct_string_literals",
	"LY305_allow_locals_only_in_tftest",
	"LY305_allow_not_whole_document",
	"LY305_allow_same_names_in_other_module",
	"LY305_deny_conditional_same_leaf",
	"LY305_deny_decimal_string_and_number",
	"LY305_deny_exponent_string_and_number",
	"LY305_deny_literal_and_whole",
	"LY305_deny_number_and_numeric_string",
	"LY305_deny_overlapping_literal_sets",
	"LY305_deny_reference_without_attribute",
	"LY305_deny_same_literal",
	"LY305_deny_same_whole_parent",
	"LY305_deny_signed_and_zero_padded_strings_1",
	"LY305_deny_signed_and_zero_padded_strings_2",
	"LY305_deny_through_duplicated_local",
	"LY305_deny_through_local",
	"LY305_deny_through_two_plain_locals",
	"LY305_deny_whole_and_literal",
	"LY307_allow_parent_in_other_module",
	"LY307_deny_attachment_to_inline_owned_parent",
	"LY307_deny_only_owned_parent_type",
	"LY308_allow_multi_with_distinct_parent",
	"LY308_allow_single_multi_block",
	"LY308_deny_conditional_count",
	"LY308_deny_count_index_selection",
	"LY308_deny_count_on_first",
	"LY308_deny_for_each_on_first",
	"LY308_deny_local_chain",
	"LY308_deny_local_selected_by_literal",
	"LY308_deny_multi_and_single",
	"LY308_deny_multi_only_second",
	"LY308_deny_plain_local_to_selected_local",
	"LY308_deny_selected_local",
	"LY308_deny_splat_selection",
	"LY308_deny_two_multi",
	"LY308_deny_undecidable_index",
	"LY308_deny_variable_indexes",
	"LY309_allow_aws_managed_literal",
	"LY309_allow_aws_managed_literal_other_partition",
	"LY309_allow_aws_managed_literal_through_local",
	"LY309_allow_aws_managed_partition_template",
	"LY309_allow_aws_managed_partition_through_local",
	"LY309_allow_aws_managed_template_through_local",
	"LY309_allow_direct_reference",
	"LY309_allow_each_value_with_literal_key",
	"LY309_allow_indexed_reference",
	"LY309_allow_other_attachment_type",
	"LY309_allow_reference_through_each",
	"LY309_allow_reference_through_local",
	"LY309_deny_conditional_with_customer_literal",
	"LY309_deny_data_source",
	"LY309_deny_each_from_call",
	"LY309_deny_each_label_literal",
	"LY309_deny_empty_tuple",
	"LY309_deny_for_each_mixed_literal",
	"LY309_deny_literal_customer_managed",
	"LY309_deny_local_conditional",
	"LY309_deny_local_resolving_to_literal",
	"LY309_deny_module_reference",
	"LY309_deny_partition_local_from_variable",
	"LY309_deny_partition_other_attribute",
	"LY309_deny_policy_and_variable",
	"LY309_deny_policy_in_other_module",
	"LY309_deny_template_customer_managed",
	"LY309_deny_template_name_interpolated",
	"LY309_deny_template_partition_account_literal",
	"LY309_deny_template_partition_from_variable",
	"LY309_deny_template_suffix_on_reference",
	"LY309_deny_template_without_arn_prefix",
	"LY309_deny_variable",
	"LY309_deny_without_policy_arn",
	"LY309_deny_wrong_resource_type",
	"LY401_allow_data_source_outside_owner",
	"LY401_allow_owner_internal",
	"LY401_allow_second_owner",
	"LY401_deny_env_root",
	"LY401_deny_general_module",
	"LY401_deny_other_usecase",
	"LY401_deny_usecase_name_prefix",
	"ok_module",
	"refs_shapes",
	"tree_attachment_module_ref",
	"tree_bucket_policy_distinct",
	"tree_bucket_policy_twice",
	"tree_comment_in_expression",
	"tree_cond_in_interpolation",
	"tree_conditional_count",
	"tree_deep_nesting",
	"tree_default_sg_empty",
	"tree_dynamic_parent_module",
	"tree_dynamic_parent_ok",
	"tree_empty_decl",
	"tree_env_to_module",
	"tree_escape1_quote_pair",
	"tree_escape2_brace_in_interpolation",
	"tree_escape3_template_directive",
	"tree_escape4_concatenated_dollar",
	"tree_escape5_comment_in_expression",
	"tree_escape5_comment_in_expression_line",
	"tree_escape6_iterator_shadowing",
	"tree_escape6_iterator_shadowing_resource_type",
	"tree_escape7_object_key",
	"tree_escaped_literal_then_parent",
	"tree_escaped_only",
	"tree_exclusive_twice",
	"tree_exclusive_with_attachment",
	"tree_exclusive_with_inline_policy",
	"tree_for_each_and_single",
	"tree_for_each_each_value",
	"tree_for_each_module",
	"tree_for_each_parent",
	"tree_for_each_per_bucket",
	"tree_index_after_attribute",
	"tree_index_after_reference",
	"tree_index_literal",
	"tree_inline_policy_block",
	"tree_inline_policy_dynamic",
	"tree_kms_key_policy",
	"tree_literal_distinct",
	"tree_literal_same",
	"tree_local_selected_in_one_block",
	"tree_managed_policy_arns_null",
	"tree_nested_parent_module",
	"tree_nested_parent_ok",
	"tree_no_tf",
	"tree_ok",
	"tree_remote_source",
	"tree_route_table_owned",
	"tree_route_to_owned",
	"tree_schema_empty",
	"tree_second_index_after_reference",
	"tree_sg_ingress_empty",
	"tree_splat_parent",
	"tree_template_directive_parent",
	"tree_unclassified_deprecated",
	"tree_unclassified_type",
	"tree_usecase_resource",
}

test_fixtures_present if {
	every name in fixture_cases {
		count(fx(name).files) > 0
	}
	{k | some k, _ in data.fixtures; not startswith(k, "lib_")} == fixture_cases
}

# --- LY001 の項目の形（正しい aws_s3_bucket_policy から、1つの性質だけを壊す） ---

policy_path := "/attachments/aws_s3_bucket_policy"

broken_decl(op) := json.patch(fixture_decl, [object.union(op, {"path": concat("", [policy_path, op.path])})])

ly001_of(d) := with_id(denies_with(fx("ok_module"), listed_of(fx("ok_module")), d), "LY001")

test_LY001_allow_policy_entry if {
	count(ly001_of(fixture_decl)) == 0
}

# 項目の形の崩れは、その項目の LY001 だけを出す。parents を空にするか引数を足すと、whole_document の引数の数の LY001 も並ぶ。
test_LY001_deny_each_broken_property if {
	shape := "だけを持ちません"
	every c in [
		[{"op": "replace", "path": "", "value": "x"}, shape],
		[{"op": "replace", "path": "/parents", "value": {}}, shape],
		[{"op": "replace", "path": "/parents", "value": ["bucket"]}, shape],
		[{"op": "add", "path": "/parents/", "value": ["aws_s3_bucket"]}, shape],
		[{"op": "replace", "path": "/parents/bucket", "value": []}, shape],
		[{"op": "replace", "path": "/parents/bucket", "value": "aws_s3_bucket"}, shape],
		[{"op": "replace", "path": "/parents/bucket/0", "value": ""}, shape],
		[{"op": "replace", "path": "/parents/bucket/0", "value": 1}, shape],
		[{"op": "replace", "path": "/optional", "value": "false"}, shape],
		[{"op": "remove", "path": "/optional"}, shape],
		[{"op": "add", "path": "/skip", "value": true}, shape],
		[{"op": "replace", "path": "/whole_document", "value": "true"}, shape],
		[{"op": "remove", "path": "/whole_document"}, shape],
		[{"op": "replace", "path": "/parent_arguments", "value": []}, shape],
		[{"op": "remove", "path": "/parent_arguments"}, shape],
		[{"op": "replace", "path": "/parent_arguments/aws_s3_bucket", "value": {}}, shape],
		[{"op": "replace", "path": "/parent_arguments/aws_s3_bucket/policy", "value": "x"}, shape],
		[{"op": "add", "path": "/parent_arguments/aws_s3_bucket/", "value": null}, shape],
		[{"op": "add", "path": "/parent_arguments/aws_vpc", "value": {"a": null}}, shape],
		[{"op": "add", "path": "/parents/key", "value": ["aws_s3_bucket"]}, "ちょうど1つではありません"],
	] {
		msgs := ly001_of(broken_decl(c[0]))
		some m in msgs
		contains(m, c[1])
		every x in msgs {
			contains(x, "attachments.aws_s3_bucket_policy")
		}
	}
}

test_LY001_deny_reason_not_string if {
	d := json.patch(fixture_decl, [{"op": "replace", "path": "/not_attachments/aws_vpc", "value": 1}])
	only_with(ly001_of(d), "not_attachments.aws_vpc の理由が空でない文字列ではありません")
}
