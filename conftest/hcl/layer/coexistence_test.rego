package layer


cox_decl := {
	"attachments": object.union(fixture_decl.attachments, {
		"aws_iam_role_policy": {"parents": {"role": ["aws_iam_role"]}, "optional": false, "parent_arguments": {"aws_iam_role": {"inline_policy": []}}, "whole_document": false},
		"aws_iam_role_policies_exclusive": {"parents": {"role_name": ["aws_iam_role"]}, "optional": false, "parent_arguments": {"aws_iam_role": {"inline_policy": []}}, "whole_document": true},
		"aws_iam_role_policy_attachment": {"parents": {"role": ["aws_iam_role"]}, "optional": false, "parent_arguments": {"aws_iam_role": {"managed_policy_arns": null}}, "whole_document": false},
		"aws_iam_role_policy_attachments_exclusive": {"parents": {"role_name": ["aws_iam_role"]}, "optional": false, "parent_arguments": {"aws_iam_role": {"managed_policy_arns": null}}, "whole_document": true},
		"aws_security_group_rule": {"parents": {"security_group_id": ["aws_security_group"]}, "optional": false, "parent_arguments": {"aws_security_group": {"ingress": null, "egress": null}}, "whole_document": false},
		"aws_route": {"parents": {"route_table_id": ["aws_route_table"]}, "optional": false, "parent_arguments": {"aws_route_table": {"route": null}}, "whole_document": false},
	}),
	"not_attachments": object.union(fixture_decl.not_attachments, {
		"aws_iam_role": "親になる resource",
		"aws_security_group": "親になる resource",
		"aws_default_security_group": "既定の SG を引き取る resource",
		"aws_route_table": "親になる resource",
	}),
	"singletons": fixture_decl.singletons,
	"unrelated_deprecated_arguments": fixture_decl.unrelated_deprecated_arguments,
	"inline_owned": {"aws_route_table": {"route": "経路を排他に持つ"}},
}

cox_schema := object.union(fixture_schema, {
	"aws_iam_role": {"arguments": ["name", "inline_policy", "managed_policy_arns"], "deprecated": ["inline_policy", "managed_policy_arns"], "blocks": ["inline_policy"]},
	"aws_security_group": {"arguments": ["name", "ingress", "egress"], "deprecated": [], "blocks": []},
	"aws_route_table": {"arguments": ["vpc_id", "route"], "deprecated": [], "blocks": []},
})

cox_denies_with(doc, d, sch) := result if {
	result := deny with input as doc
		with data.layer_decl as d
		with data.layer_files as listed_of(doc)
		with data.hcl_parsed_files as parsed_of(doc)
		with data.hcl_block_counts as counts_of(doc)
		with data.layer_schema as sch
}

cox_denies(doc) := cox_denies_with(doc, cox_decl, cox_schema)

# --- LY304 ---

test_LY304_deny_inline_policy_block if {
	msgs := with_id(cox_denies(fx("LY304_deny_inline_policy_block")), "LY304")
	only_with(msgs, "aws_iam_role.r が inline_policy を書いています。同じ設定を attachment 型（aws_iam_role_policies_exclusive, aws_iam_role_policy）が書く")
}

test_LY304_deny_dynamic_inline_policy if {
	count(with_id(cox_denies(fx("LY304_deny_dynamic_inline_policy")), "LY304")) == 1
}

test_LY304_deny_null_attribute if {
	count(with_id(cox_denies(fx("LY304_deny_null_attribute")), "LY304")) == 1
}

test_LY304_deny_empty_list if {
	msgs := with_id(cox_denies(fx("LY304_deny_empty_list")), "LY304")
	only_with(msgs, "aws_security_group.s が ingress を書いています。同じ設定を attachment 型（aws_security_group_rule）が書く")
}

test_LY304_deny_dynamic_without_attributes if {
	count(with_id(cox_denies(fx("LY304_deny_dynamic_without_attributes")), "LY304")) == 1
}

test_LY304_allow_default_security_group if {
	count(with_id(cox_denies(fx("LY304_allow_default_security_group")), "LY304")) == 0
}

test_LY304_allow_unrelated_argument if {
	count(with_id(cox_denies(fx("LY304_allow_unrelated_argument")), "LY304")) == 0
}

test_LY304_allow_inline_owned if {
	count(with_id(cox_denies(fx("LY304_allow_inline_owned")), "LY304")) == 0
}

# 同じ role に、排他の型と個別の型を併用する形は、親の引数を書かないので落とさない。
test_LY304_allow_exclusive_with_individual_attachment if {
	count(cox_denies(fx("LY304_allow_exclusive_with_individual_attachment"))) == 0
}

# --- LY307 ---

route_src := `resource "aws_route_table" "t" {
	route = []
}

resource "aws_route" "r" {
	route_table_id = aws_route_table.t.id
}
`

test_LY307_deny_attachment_to_inline_owned_parent if {
	some m in with_id(cox_denies(fx("LY307_deny_attachment_to_inline_owned_parent")), "LY307")
	contains(m, "aws_route.r が、route を排他に持つと宣言した親 aws_route_table.t")
}

test_LY307_allow_without_inline_owned if {
	d := object.union(object.remove(cox_decl, ["inline_owned"]), {"inline_owned": {}})
	msgs := cox_denies_with(fx("LY307_deny_attachment_to_inline_owned_parent"), d, cox_schema)
	count(with_id(msgs, "LY307")) == 0
	count(with_id(msgs, "LY304")) == 1
}

# --- LY305・LY308 ---

test_LY305_deny_same_whole_parent if {
	some m in with_id(cox_denies(fx("LY305_deny_same_whole_parent")), "LY305")
	contains(m, "aws_s3_bucket_policy.a と aws_s3_bucket_policy.b が、同じ親 aws_s3_bucket.this")
}

test_LY305_deny_same_literal if {
	count(with_id(cox_denies(fx("LY305_deny_same_literal")), "LY305")) == 1
}

test_LY305_deny_number_and_numeric_string if {
	count(with_id(cox_denies(fx("LY305_deny_number_and_numeric_string")), "LY305")) == 1
}

test_LY305_deny_signed_and_zero_padded_strings if {
	count(with_id(cox_denies(fx("LY305_deny_signed_and_zero_padded_strings_1")), "LY305")) == 1
	count(with_id(cox_denies(fx("LY305_deny_signed_and_zero_padded_strings_2")), "LY305")) == 1
}

test_LY305_deny_exponent_string_and_number if {
	count(with_id(cox_denies(fx("LY305_deny_exponent_string_and_number")), "LY305")) == 1
}

test_LY305_deny_whole_and_literal if {
	count(with_id(cox_denies(fx("LY305_deny_whole_and_literal")), "LY305")) == 1
}

test_LY305_deny_through_local if {
	count(with_id(cox_denies(fx("LY305_deny_through_local")), "LY305")) == 1
}

# 同じ名前の local を2つの block が定義しても、評価を止めずに両方の定義を辿る。
test_LY305_deny_through_duplicated_local if {
	count(with_id(cox_denies(fx("LY305_deny_through_duplicated_local")), "LY305")) == 1
}

test_LY305_deny_overlapping_literal_sets if {
	count(with_id(cox_denies(fx("LY305_deny_overlapping_literal_sets")), "LY305")) == 1
}

test_LY305_allow_distinct_literals if {
	msgs := cox_denies(fx("LY305_allow_distinct_literals"))
	count(with_id(msgs, "LY305")) == 0
	count(with_id(msgs, "LY308")) == 0
}

test_LY305_allow_distinct_parents if {
	msgs := cox_denies(fx("LY305_allow_distinct_parents"))
	count(with_id(msgs, "LY305")) == 0
	count(with_id(msgs, "LY308")) == 0
}

test_LY305_allow_not_whole_document if {
	count(cox_denies(fx("LY305_allow_not_whole_document"))) == 0
}

test_LY308_deny_multi_and_single if {
	some m in with_id(cox_denies(fx("LY308_deny_multi_and_single")), "LY308")
	contains(m, "同じ instance かを参照から判定できません")
}

test_LY308_deny_two_multi if {
	count(with_id(cox_denies(fx("LY308_deny_two_multi")), "LY308")) == 1
}

test_LY308_deny_conditional_count if {
	count(with_id(cox_denies(fx("LY308_deny_conditional_count")), "LY308")) == 1
}

test_LY308_deny_undecidable_index if {
	count(with_id(cox_denies(fx("LY308_deny_undecidable_index")), "LY308")) == 1
}

test_LY308_deny_selected_local if {
	count(with_id(cox_denies(fx("LY308_deny_selected_local")), "LY308")) == 1
}

# 1つの block の instance どうしは比べない（README の「保証しないこと」）。
test_LY308_allow_single_multi_block if {
	msgs := cox_denies(fx("LY308_allow_single_multi_block"))
	count(with_id(msgs, "LY305")) == 0
	count(with_id(msgs, "LY308")) == 0
}

test_LY308_allow_multi_with_distinct_parent if {
	count(with_id(cox_denies(fx("LY308_allow_multi_with_distinct_parent")), "LY308")) == 0
}

# --- LY306 ---

test_LY306_deny_schema_missing if {
	msgs := cox_denies_with(fx("ok_module"), cox_decl, null)
	only_with(with_id(msgs, "LY306"), "data.layer_schema が無いか")
}

test_LY306_deny_schema_empty if {
	only_with(with_id(cox_denies_with(fx("ok_module"), cox_decl, {}), "LY306"), "LY306 [schema の一覧] data.layer_schema が無いか、空か")
}

test_LY306_deny_schema_entry_without_arguments if {
	sch := object.union(cox_schema, {"aws_vpc": {"arguments": [], "deprecated": [], "blocks": []}})
	only_with(with_id(cox_denies_with(fx("ok_module"), cox_decl, sch), "LY306"), "親の型 aws_vpc の書ける引数が schema から1つも取れていません")
}

test_LY306_deny_deprecated_not_in_arguments if {
	sch := object.union(cox_schema, {"aws_vpc": {"arguments": ["name"], "deprecated": ["x"], "blocks": []}})
	only_with(with_id(cox_denies_with(fx("ok_module"), cox_decl, sch), "LY306"), "LY306 [schema の一覧]")
}

test_LY306_deny_parent_type_missing if {
	sch := object.remove(cox_schema, ["aws_route_table"])
	only_with(with_id(cox_denies_with(fx("ok_module"), cox_decl, sch), "LY306"), "aws_route_table")
}

test_LY306_deny_unclassified_deprecated if {
	sch := object.union(cox_schema, {"aws_iam_role": {"arguments": ["name", "inline_policy", "managed_policy_arns", "x"], "deprecated": ["inline_policy", "managed_policy_arns", "x"], "blocks": ["inline_policy"]}})
	only_with(with_id(cox_denies_with(fx("ok_module"), cox_decl, sch), "LY306"), "aws_iam_role の非推奨の引数 x")
}

test_LY306_deny_declared_argument_not_in_schema if {
	sch := object.union(cox_schema, {"aws_security_group": {"arguments": ["name", "ingress"], "deprecated": [], "blocks": []}})
	only_with(with_id(cox_denies_with(fx("ok_module"), cox_decl, sch), "LY306"), "aws_security_group.egress")
}

test_LY306_deny_stale_unrelated if {
	sch := object.union(cox_schema, {"aws_s3_bucket": {"arguments": ["bucket", "policy", "acceleration_status", "versioning"], "deprecated": ["policy"], "blocks": ["versioning"]}})
	only_with(with_id(cox_denies_with(fx("ok_module"), cox_decl, sch), "LY306"), "古い分類")
}

test_LY306_deny_unwritten_value_mismatch if {
	sch := object.union(cox_schema, {"aws_iam_role": {"arguments": ["name", "inline_policy", "managed_policy_arns"], "deprecated": ["inline_policy", "managed_policy_arns"], "blocks": []}})
	with_id(cox_denies_with(fx("ok_module"), cox_decl, sch), "LY306") == {
		"LY306 [書かないときの値] attachments.aws_iam_role_policy.parent_arguments の aws_iam_role.inline_policy の値が schema の種類と合いません（block は []、属性は null）",
		"LY306 [書かないときの値] attachments.aws_iam_role_policies_exclusive.parent_arguments の aws_iam_role.inline_policy の値が schema の種類と合いません（block は []、属性は null）",
	}
}

test_LY306_allow_classified if {
	count(with_id(cox_denies(fx("ok_module")), "LY306")) == 0
}

# --- LY001（決定18 の宣言の形） ---

cox_decl_with(t, a) := object.union(cox_decl, {"attachments": {t: a}})

cox_lY001(d) := with_id(cox_denies_with(fx("ok_module"), d, cox_schema), "LY001")

test_LY001_allow_coexistence_decl if {
	count(cox_lY001(cox_decl)) == 0
}

test_LY001_deny_missing_parent_arguments if {
	only_with(cox_lY001(cox_decl_with("aws_x", {"parents": {"a": ["aws_vpc"]}, "optional": false, "whole_document": false})), "attachments.aws_x")
}

test_LY001_deny_parent_arguments_foreign_type if {
	only_with(cox_lY001(cox_decl_with("aws_x", {"parents": {"a": ["aws_vpc"]}, "optional": false, "parent_arguments": {"aws_subnet": {"b": null}}, "whole_document": false})), "attachments.aws_x")
}

test_LY001_deny_parent_arguments_bad_value if {
	only_with(cox_lY001(cox_decl_with("aws_x", {"parents": {"a": ["aws_vpc"]}, "optional": false, "parent_arguments": {"aws_vpc": {"b": "x"}}, "whole_document": false})), "attachments.aws_x")
}

test_LY001_deny_whole_document_not_boolean if {
	only_with(cox_lY001(cox_decl_with("aws_x", {"parents": {"a": ["aws_vpc"]}, "optional": false, "parent_arguments": {}, "whole_document": "true"})), "attachments.aws_x")
}

test_LY001_deny_whole_document_with_two_parents if {
	only_with(cox_lY001(cox_decl_with("aws_x", {"parents": {"a": ["aws_vpc"], "b": ["aws_subnet"]}, "optional": false, "parent_arguments": {}, "whole_document": true})), "ちょうど1つではありません")
}

test_LY001_deny_unrelated_missing if {
	only_with(cox_lY001(object.remove(cox_decl, ["unrelated_deprecated_arguments"])), "unrelated_deprecated_arguments")
}

test_LY001_deny_unrelated_without_unwritten if {
	d := object.union(object.remove(cox_decl, ["unrelated_deprecated_arguments"]), {"unrelated_deprecated_arguments": {"aws_s3_bucket": {"acceleration_status": "fixture"}}})
	only_with(cox_lY001(d), "unrelated_deprecated_arguments")
}

test_LY306_deny_unrelated_unwritten_mismatch if {
	d := object.union(object.remove(cox_decl, ["unrelated_deprecated_arguments"]), {"unrelated_deprecated_arguments": {"aws_s3_bucket": {"acceleration_status": {"reason": "fixture", "unwritten": []}}}})
	only_with(with_id(cox_denies_with(fx("ok_module"), d, cox_schema), "LY306"), "unrelated_deprecated_arguments の aws_s3_bucket.acceleration_status")
}

test_LY001_deny_inline_owned_empty_reason if {
	only_with(cox_lY001(object.union(cox_decl, {"inline_owned": {"aws_route_table": {"route": ""}}})), "inline_owned")
}

test_LY001_deny_reason_type_not_parent if {
	only_with(cox_lY001(object.union(cox_decl, {"unrelated_deprecated_arguments": {"aws_lb": {"x": {"reason": "y", "unwritten": null}}}})), "unrelated_deprecated_arguments.aws_lb")
}

test_LY001_deny_argument_in_both if {
	only_with(cox_lY001(object.union(cox_decl, {"unrelated_deprecated_arguments": {"aws_iam_role": {"inline_policy": {"reason": "x", "unwritten": []}}}})), "aws_iam_role.inline_policy が parent_arguments と unrelated_deprecated_arguments の両方")
}

test_LY001_deny_inline_owned_not_parent_argument if {
	only_with(cox_lY001(object.union(cox_decl, {"inline_owned": {"aws_iam_role": {"name": "x"}}})), "inline_owned.aws_iam_role.name")
}

test_LY001_deny_unwritten_value_conflict if {
	d := cox_decl_with("aws_iam_role_policy", {"parents": {"role": ["aws_iam_role"]}, "optional": false, "parent_arguments": {"aws_iam_role": {"inline_policy": null}}, "whole_document": false})
	only_with(cox_lY001(d), "書かないときの値が")
}

# --- LY304 の書いたと数える形 ---

test_LY304_deny_nested_attribute if {
	msgs := with_id(cox_denies(fx("LY304_deny_nested_attribute")), "LY304")
	only_with(msgs, "aws_iam_role.r が inline_policy を書いています")
}

# 引数名を接頭辞に持つ別の引数は、その引数を書いたと数えない。
test_LY304_allow_argument_name_prefix if {
	count(with_id(cox_denies(fx("LY304_allow_argument_name_prefix")), "LY304")) == 0
}

# 別の resource が書いた引数は、その resource の分だけを数える。
test_LY304_deny_only_resource_that_writes if {
	msgs := with_id(cox_denies(fx("LY304_deny_only_resource_that_writes")), "LY304")
	only_with(msgs, "aws_iam_role.q が managed_policy_arns を書いています")
}

# --- LY305・LY308 の参照の形 ---

# count か for_each のどちらか1つだけでも、LY305 を LY308 に変える。比べる組の後ろの側（c2）だけでも変える。
multi_only(name) if {
	count(with_id(cox_denies(fx(name)), "LY305")) == 0
	count(with_id(cox_denies(fx(name)), "LY308")) == 1
}

test_LY308_deny_count_on_first if {
	multi_only("LY308_deny_count_on_first")
}

test_LY308_deny_for_each_on_first if {
	multi_only("LY308_deny_for_each_on_first")
}

test_LY308_deny_multi_only_second if {
	multi_only("LY308_deny_multi_only_second")
}

test_LY305_allow_no_multi if {
	count(with_id(cox_denies(fx("LY305_deny_same_whole_parent")), "LY308")) == 0
}

test_LY308_deny_count_index_selection if {
	msgs := with_id(cox_denies(fx("LY308_deny_count_index_selection")), "LY308")
	only_with(msgs, "aws_s3_bucket_policy.a と aws_s3_bucket_policy.b が、同じ親 aws_s3_bucket.this を指し")
}

test_LY308_deny_splat_selection if {
	msgs := with_id(cox_denies(fx("LY308_deny_splat_selection")), "LY308")
	only_with(msgs, "同じ親 aws_s3_bucket.this を指し")
}

# 属性の続かない1段の参照は、親の全体を選ぶ。
test_LY305_deny_reference_without_attribute if {
	msgs := with_id(cox_denies(fx("LY305_deny_reference_without_attribute")), "LY305")
	only_with(msgs, "同じ親 aws_s3_bucket.this の文書全体を書きます")
}

test_LY305_deny_literal_and_whole if {
	count(with_id(cox_denies(fx("LY305_deny_literal_and_whole")), "LY305")) == 1
}

test_LY305_deny_decimal_string_and_number if {
	count(with_id(cox_denies(fx("LY305_deny_decimal_string_and_number")), "LY305")) == 1
}

test_LY305_allow_distinct_string_literals if {
	msgs := cox_denies(fx("LY305_allow_distinct_string_literals"))
	count(with_id(msgs, "LY305")) == 0
	count(with_id(msgs, "LY308")) == 0
}

test_LY305_deny_through_two_plain_locals if {
	only_with(with_id(cox_denies(fx("LY305_deny_through_two_plain_locals")), "LY305"), "同じ親 aws_s3_bucket.this")
}

# plain な local の先で local を添字で選べば、その先の葉は判定できない。
test_LY308_deny_plain_local_to_selected_local if {
	msgs := cox_denies(fx("LY308_deny_plain_local_to_selected_local"))
	count(with_id(msgs, "LY305")) == 0
	count(with_id(msgs, "LY308")) == 1
}

# local をリテラルの添字で選んでも、local の中身の添字までは辿らない。
test_LY308_deny_local_selected_by_literal if {
	msgs := cox_denies(fx("LY308_deny_local_selected_by_literal"))
	count(with_id(msgs, "LY305")) == 0
	count(with_id(msgs, "LY308")) == 1
}

# 別の module の同じ名前の親と attachment は比べない。
test_LY305_allow_same_names_in_other_module if {
	msgs := cox_denies(fx("LY305_allow_same_names_in_other_module"))
	count(with_id(msgs, "LY305")) == 0
	count(with_id(msgs, "LY308")) == 0
	count(with_id(msgs, "LY301")) == 0
}

# --- LY306 の schema の形 ---

cox_ly306(sch) := with_id(cox_denies_with(fx("ok_module"), cox_decl, sch), "LY306")

# object.union は object を入れ子まで合わせるので、既存の型に重ねず、新しい型の項目として足す。
schema_shape_denied(e) if only_with(cox_ly306(object.union(cox_schema, {"aws_zz": e})), "LY306 [schema の一覧]")

test_LY306_deny_block_not_in_arguments if {
	schema_shape_denied({"arguments": ["name"], "deprecated": [], "blocks": ["x"]})
}

test_LY306_deny_entry_missing_key if {
	schema_shape_denied({"arguments": ["name"], "deprecated": []})
}

test_LY306_deny_entry_extra_key if {
	schema_shape_denied({"arguments": ["name"], "deprecated": [], "blocks": [], "computed": []})
}

test_LY306_deny_entry_value_not_array if {
	every e in [
		{"arguments": "name", "deprecated": [], "blocks": []},
		{"arguments": ["name"], "deprecated": {}, "blocks": []},
		{"arguments": ["name"], "deprecated": [], "blocks": null},
	] {
		schema_shape_denied(e)
	}
}

test_LY306_deny_entry_empty_string if {
	schema_shape_denied({"arguments": ["name", ""], "deprecated": [], "blocks": []})
}

test_LY306_deny_entry_not_object if {
	schema_shape_denied(["name"])
}

test_LY306_deny_unrelated_argument_not_in_schema if {
	sch := object.union(cox_schema, {"aws_s3_bucket": {"arguments": ["bucket", "policy", "versioning"], "deprecated": ["policy"], "blocks": ["versioning"]}})
	msgs := cox_ly306(sch)
	count(msgs) == 2
	some m1 in msgs
	contains(m1, "宣言の aws_s3_bucket.acceleration_status が、Provider の schema の書ける引数にありません")
	some m2 in msgs
	contains(m2, "unrelated_deprecated_arguments の aws_s3_bucket.acceleration_status は、Provider の schema で非推奨ではありません")
}

# inline_owned の引数は parent_arguments にも在るのが正しい宣言なので、inline_owned にだけ在る引数（LY001 で落ちる宣言）で見る。
test_LY306_deny_inline_owned_argument_not_in_schema if {
	d := object.union(cox_decl, {"inline_owned": {"aws_route_table": {"route": "x", "gateway": "x"}}})
	msgs := with_id(cox_denies_with(fx("ok_module"), d, cox_schema), "LY306")
	only_with(msgs, "宣言の aws_route_table.gateway が、Provider の schema の書ける引数にありません")
}

# 別の親の型で分類された引数名は、この型の非推奨の引数の分類に数えない。
test_LY306_deny_argument_classified_for_other_type if {
	every arg in ["policy", "acceleration_status"] {
		sch := object.union(cox_schema, {"aws_iam_role": {"arguments": ["name", "inline_policy", "managed_policy_arns", arg], "deprecated": ["inline_policy", "managed_policy_arns", arg], "blocks": ["inline_policy"]}})
		only_with(cox_ly306(sch), sprintf("親の型 aws_iam_role の非推奨の引数 %s が", [arg]))
	}
}

# --- LY001 の決定18 の宣言の形 ---

with_unrelated(x) := object.union(object.remove(cox_decl, ["unrelated_deprecated_arguments"]), {"unrelated_deprecated_arguments": x})

unrelated_denied(x) if only_with(cox_lY001(with_unrelated(x)), "data.layer_decl.unrelated_deprecated_arguments が")

test_LY001_deny_unrelated_empty_reason if {
	unrelated_denied({"aws_s3_bucket": {"acceleration_status": {"reason": "", "unwritten": null}}})
}

test_LY001_deny_unrelated_unwritten_not_empty_value if {
	every v in ["", [null], {}, 0, false] {
		unrelated_denied({"aws_s3_bucket": {"acceleration_status": {"reason": "x", "unwritten": v}}})
	}
}

test_LY001_deny_unrelated_extra_key if {
	unrelated_denied({"aws_s3_bucket": {"acceleration_status": {"reason": "x", "unwritten": null, "since": "6.0"}}})
}

test_LY001_deny_unrelated_type_without_arguments if {
	unrelated_denied({"aws_s3_bucket": {}})
}

test_LY001_allow_unrelated_unwritten_empty_list if {
	count(cox_lY001(with_unrelated({"aws_s3_bucket": {"acceleration_status": {"reason": "x", "unwritten": []}}}))) == 0
}

test_LY001_deny_inline_owned_type_without_arguments if {
	d := object.union(object.remove(cox_decl, ["inline_owned"]), {"inline_owned": {"aws_route_table": {}}})
	only_with(cox_lY001(d), "data.layer_decl.inline_owned が")
}

test_LY001_deny_inline_owned_missing if {
	only_with(cox_lY001(object.remove(cox_decl, ["inline_owned"])), "data.layer_decl.inline_owned が")
}

test_LY001_deny_parent_arguments_wildcard_type if {
	d := cox_decl_with("aws_x", {"parents": {"a": ["*"]}, "optional": false, "parent_arguments": {"*": {"b": null}}, "whole_document": false})
	only_with(cox_lY001(d), "attachments.aws_x が parents")
}

# .tftest.hcl の block は module の構成ではないので、そこに書いた locals は local の定義に数えない。
test_LY305_allow_locals_only_in_tftest if {
	count(with_id(cox_denies(fx("LY305_allow_locals_only_in_tftest")), "LY305")) == 0
	some m in with_id(cox_denies(fx("LY305_allow_locals_only_in_tftest")), "LY301")
	contains(m, "local.b")
}

# 文書全体を置き換える型どうしでも、型が違えば同じ文書ではない。
test_LY305_allow_distinct_exclusive_types if {
	count(cox_denies(fx("LY305_allow_distinct_exclusive_types"))) == 0
}

# 添字が別々の変数なら、同じ instance かを判定できない。
test_LY308_deny_variable_indexes if {
	msgs := cox_denies(fx("LY308_deny_variable_indexes"))
	count(with_id(msgs, "LY305")) == 0
	only_with(with_id(msgs, "LY308"), "aws_s3_bucket_policy.x と aws_s3_bucket_policy.y が、同じ親 aws_s3_bucket.this")
}

# 条件式の両方の枝が親の葉で、一方の枝が他方の resource と同じ葉を指せば重複である。
test_LY305_deny_conditional_same_leaf if {
	msgs := cox_denies(fx("LY305_deny_conditional_same_leaf"))
	count(with_id(msgs, "LY301")) == 0
	only_with(with_id(msgs, "LY305"), "aws_s3_bucket_policy.x と aws_s3_bucket_policy.y が、同じ親 aws_s3_bucket.this")
}

test_LY305_allow_conditional_different_leaf if {
	count(cox_denies(fx("LY305_allow_conditional_different_leaf"))) == 0
}

# 何段の local を経ても、選び方が添字で辿れなければ判定できない側に倒す。
test_LY308_deny_local_chain if {
	msgs := cox_denies(fx("LY308_deny_local_chain"))
	count(with_id(msgs, "LY305")) == 0
	only_with(with_id(msgs, "LY308"), "aws_s3_bucket_policy.x と aws_s3_bucket_policy.y")
}

# 親の型を2つ持つ attachment 型は、排他に持つと宣言した親の型を指す resource だけが落ちる。
two_parent_decl := json.patch(cox_decl, [{"op": "replace", "path": "/attachments/aws_route/parents/route_table_id", "value": ["aws_route_table", "aws_vpc"]}])

test_LY307_deny_only_owned_parent_type if {
	msgs := cox_denies_with(fx("LY307_deny_only_owned_parent_type"), two_parent_decl, cox_schema)
	only_with(with_id(msgs, "LY307"), "aws_route.to_table が、route を排他に持つと宣言した親 aws_route_table.t")
}

# 同じ module に無い親は LY301 が落とし、LY307 は数えない。
test_LY307_allow_parent_in_other_module if {
	msgs := cox_denies(fx("LY307_allow_parent_in_other_module"))
	count(with_id(msgs, "LY307")) == 0
	count(with_id(msgs, "LY301")) == 1
}
