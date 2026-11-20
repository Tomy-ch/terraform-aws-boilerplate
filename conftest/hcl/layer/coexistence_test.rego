package layer

# 参照は手で書き、HCL のソースは Conftest の側の block の構造を作る。

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
	"unrelated_deprecated_arguments": fixture_decl.unrelated_deprecated_arguments,
	"inline_owned": {"aws_route_table": {"route": "経路を排他に持つ"}},
}

cox_schema := object.union(fixture_schema, {
	"aws_iam_role": {"arguments": ["name", "inline_policy", "managed_policy_arns"], "deprecated": ["inline_policy", "managed_policy_arns"], "blocks": ["inline_policy"]},
	"aws_security_group": {"arguments": ["name", "ingress", "egress"], "deprecated": [], "blocks": []},
	"aws_route_table": {"arguments": ["vpc_id", "route"], "deprecated": [], "blocks": []},
})

cox_denies_with(files, blocks, d, sch) := result if {
	result := deny with input as files
		with data.layer_decl as d
		with data.layer_files as listed_of(files)
		with data.hcl_input as go_input(files, blocks)
		with data.layer_schema as sch
}

cox_denies(files, blocks) := cox_denies_with(files, blocks, cox_decl, cox_schema)

cox_ids(src, blocks, id) := with_id(cox_denies([tf(main_tf, src)], blocks), id)

# --- LY304 ---

test_LY304_deny_inline_policy_block if {
	msgs := cox_ids(`resource "aws_iam_role" "r" {
  inline_policy {}
}
`, [], "LY304")
	only_with(msgs, "aws_iam_role.r が inline_policy を書いています。同じ設定を attachment 型（aws_iam_role_policies_exclusive, aws_iam_role_policy）が書く")
}

test_LY304_deny_dynamic_inline_policy if {
	count(cox_ids(`resource "aws_iam_role" "r" {
  dynamic "inline_policy" {
    for_each = var.p
    content {
      name = inline_policy.key
    }
  }
}
`, [], "LY304")) == 1
}

test_LY304_deny_null_attribute if {
	count(cox_ids(`resource "aws_iam_role" "r" {
  managed_policy_arns = null
}
`, [], "LY304")) == 1
}

test_LY304_deny_empty_list if {
	msgs := cox_ids(`resource "aws_security_group" "s" {
  ingress = []
}
`, [], "LY304")
	only_with(msgs, "aws_security_group.s が ingress を書いています。同じ設定を attachment 型（aws_security_group_rule）が書く")
}

test_LY304_deny_written_only_in_hcl_input if {
	count(cox_ids(`resource "aws_iam_role" "r" {}
`, [blk(main_tf, "resource", ["aws_iam_role", "r"], [attr("managed_policy_arns", [])])], "LY304")) == 1
}

test_LY304_deny_dynamic_only_in_hcl_input if {
	count(cox_ids(`resource "aws_iam_role" "r" {}
`, [blk(main_tf, "resource", ["aws_iam_role", "r"], [attr("dynamic.inline_policy.for_each", [ref("var.p")])])], "LY304")) == 1
}

test_LY304_allow_default_security_group if {
	count(cox_ids(`resource "aws_default_security_group" "d" {
  ingress = []
  egress  = []
}
`, [], "LY304")) == 0
}

test_LY304_allow_unrelated_argument if {
	count(cox_ids(`resource "aws_iam_role" "r" {
  name = "x"
}
`, [], "LY304")) == 0
}

test_LY304_allow_inline_owned if {
	count(cox_ids(`resource "aws_route_table" "t" {
  route = []
}
`, [], "LY304")) == 0
}

# 同じ role に、排他の型と個別の型を併用する形は、親の引数を書かないので落とさない。
test_LY304_allow_exclusive_with_individual_attachment if {
	src := `resource "aws_iam_role" "r" {}

resource "aws_iam_role_policy_attachments_exclusive" "e" {
  role_name   = aws_iam_role.r.name
  policy_arns = ["arn"]
}

resource "aws_iam_role_policy_attachment" "a" {
  role       = aws_iam_role.r.name
  policy_arn = "arn"
}
`
	blocks := [
		blk(main_tf, "resource", ["aws_iam_role_policy_attachments_exclusive", "e"], [attr("role_name", [ref("aws_iam_role.r.name")])]),
		blk(main_tf, "resource", ["aws_iam_role_policy_attachment", "a"], [attr("role", [ref("aws_iam_role.r.name")])]),
	]
	count(cox_denies([tf(main_tf, src)], blocks)) == 0
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
	some m in cox_ids(route_src, [blk(main_tf, "resource", ["aws_route", "r"], [attr("route_table_id", [ref("aws_route_table.t.id")])])], "LY307")
	contains(m, "aws_route.r が、route を排他に持つと宣言した親 aws_route_table.t")
}

test_LY307_allow_without_inline_owned if {
	d := object.union(object.remove(cox_decl, ["inline_owned"]), {"inline_owned": {}})
	files := [tf(main_tf, route_src)]
	blocks := [blk(main_tf, "resource", ["aws_route", "r"], [attr("route_table_id", [ref("aws_route_table.t.id")])])]
	msgs := cox_denies_with(files, blocks, d, cox_schema)
	count(with_id(msgs, "LY307")) == 0
	count(with_id(msgs, "LY304")) == 1
}

# --- LY305・LY308 ---

lit(v) := {"root": "aws_s3_bucket", "steps": [
	{"kind": "attr", "name": "this"},
	{"kind": "index", "index": {"form": "literal", "value": v}},
	{"kind": "attr", "name": "id"},
], "within": []}

each_key_ref := {"root": "aws_s3_bucket", "steps": [
	{"kind": "attr", "name": "this"},
	{"kind": "index", "index": {"form": "each_key"}},
	{"kind": "attr", "name": "id"},
], "within": []}

two_policies(extra1, extra2) := sprintf(`resource "aws_s3_bucket_policy" "a" {
  %s
  bucket = "x"
}

resource "aws_s3_bucket_policy" "b" {
  %s
  bucket = "x"
}
`, [extra1, extra2])

pol(name, attrs) := blk(main_tf, "resource", ["aws_s3_bucket_policy", name], attrs)

dup_denies(extra1, extra2, attrs1, attrs2, id) := with_id(cox_denies(module_with(two_policies(extra1, extra2)), [pol("a", attrs1), pol("b", attrs2)]), id)

test_LY305_deny_same_whole_parent if {
	some m in dup_denies("", "", [attr("bucket", [ref("aws_s3_bucket.this.id")])], [attr("bucket", [ref("aws_s3_bucket.this.id")])], "LY305")
	contains(m, "aws_s3_bucket_policy.a と aws_s3_bucket_policy.b が、同じ親 aws_s3_bucket.this")
}

test_LY305_deny_same_literal if {
	count(dup_denies("", "", [attr("bucket", [lit("a")])], [attr("bucket", [lit("a")])], "LY305")) == 1
}

test_LY305_deny_number_and_numeric_string if {
	count(dup_denies("", "", [attr("bucket", [lit(0)])], [attr("bucket", [lit("0")])], "LY305")) == 1
}

test_LY305_deny_signed_and_zero_padded_strings if {
	count(dup_denies("", "", [attr("bucket", [lit(1)])], [attr("bucket", [lit("+1")])], "LY305")) == 1
	count(dup_denies("", "", [attr("bucket", [lit(1)])], [attr("bucket", [lit("01")])], "LY305")) == 1
}

test_LY305_deny_exponent_string_and_number if {
	count(dup_denies("", "", [attr("bucket", [lit(100000)])], [attr("bucket", [lit("1e5")])], "LY305")) == 1
}

test_LY305_deny_whole_and_literal if {
	count(dup_denies("", "", [attr("bucket", [ref("aws_s3_bucket.this.id")])], [attr("bucket", [lit(1)])], "LY305")) == 1
}

test_LY305_deny_through_local if {
	src := concat("", [two_policies("", ""), "locals {\n  b = aws_s3_bucket.this.id\n}\n"])
	blocks := [
		pol("a", [attr("bucket", [ref("local.b")])]),
		pol("b", [attr("bucket", [ref("aws_s3_bucket.this.id")])]),
		locals_blk(main_tf, [attr("b", [ref("aws_s3_bucket.this.id")])]),
	]
	count(with_id(cox_denies(module_with(src), blocks), "LY305")) == 1
}

# 同じ名前の local を2つの block が定義しても、評価を止めずに両方の定義を辿る。
test_LY305_deny_through_duplicated_local if {
	src := concat("", [two_policies("", ""), "resource \"aws_s3_bucket\" \"other\" {}\n\nlocals {\n  b = aws_s3_bucket.this.id\n}\n\nlocals {\n  b = aws_s3_bucket.other.id\n}\n"])
	blocks := [
		pol("a", [attr("bucket", [ref("local.b")])]),
		pol("b", [attr("bucket", [ref("aws_s3_bucket.other.id")])]),
		locals_blk(main_tf, [attr("b", [ref("aws_s3_bucket.this.id")])]),
		locals_blk(main_tf, [attr("b", [ref("aws_s3_bucket.other.id")])]),
	]
	count(with_id(cox_denies(module_with(src), blocks), "LY305")) == 1
}

test_LY305_deny_overlapping_literal_sets if {
	count(dup_denies("", "", [attr("bucket", [lit(0), lit(1)])], [attr("bucket", [lit(1)])], "LY305")) == 1
}

test_LY305_allow_distinct_literals if {
	msgs := cox_denies(module_with(two_policies("", "")), [pol("a", [attr("bucket", [lit(0)])]), pol("b", [attr("bucket", [lit(1)])])])
	count(with_id(msgs, "LY305")) == 0
	count(with_id(msgs, "LY308")) == 0
}

test_LY305_allow_distinct_parents if {
	src := concat("", [two_policies("", ""), "resource \"aws_s3_bucket\" \"other\" {}\n"])
	blocks := [
		pol("a", [attr("bucket", [ref("aws_s3_bucket.this.id")])]),
		pol("b", [attr("bucket", [ref("aws_s3_bucket.other.id")])]),
	]
	msgs := cox_denies(module_with(src), blocks)
	count(with_id(msgs, "LY305")) == 0
	count(with_id(msgs, "LY308")) == 0
}

test_LY305_allow_not_whole_document if {
	src := `resource "aws_iam_role" "r" {}

resource "aws_iam_role_policy" "a" {
  role = aws_iam_role.r.name
}

resource "aws_iam_role_policy" "b" {
  role = aws_iam_role.r.name
}
`
	blocks := [
		blk(main_tf, "resource", ["aws_iam_role_policy", "a"], [attr("role", [ref("aws_iam_role.r.name")])]),
		blk(main_tf, "resource", ["aws_iam_role_policy", "b"], [attr("role", [ref("aws_iam_role.r.name")])]),
	]
	count(cox_denies([tf(main_tf, src)], blocks)) == 0
}

test_LY308_deny_multi_and_single if {
	some m in dup_denies("for_each = aws_s3_bucket.this", "", [attr("for_each", [ref("aws_s3_bucket.this")]), attr("bucket", [each_key_ref])], [attr("bucket", [lit("a")])], "LY308")
	contains(m, "同じ instance かを参照から判定できません")
}

test_LY308_deny_two_multi if {
	count(dup_denies(
		"for_each = aws_s3_bucket.this", "for_each = aws_s3_bucket.this",
		[attr("for_each", [ref("aws_s3_bucket.this")]), attr("bucket", [each_key_ref])],
		[attr("for_each", [ref("aws_s3_bucket.this")]), attr("bucket", [each_key_ref])],
		"LY308",
	)) == 1
}

test_LY308_deny_conditional_count if {
	count(dup_denies("count = var.x ? 1 : 0", "", [attr("count", [ref("var.x")]), attr("bucket", [ref("aws_s3_bucket.this.id")])], [attr("bucket", [ref("aws_s3_bucket.this.id")])], "LY308")) == 1
}

test_LY308_deny_undecidable_index if {
	count(dup_denies("", "", [attr("bucket", [ref("aws_s3_bucket.this[].id"), ref_in("var.k", [[0, 1]])])], [attr("bucket", [lit("a")])], "LY308")) == 1
}

test_LY308_deny_selected_local if {
	src := concat("", [two_policies("", ""), "locals {\n  all = aws_s3_bucket.this\n}\n"])
	blocks := [
		pol("a", [attr("bucket", [ref("local.all[].id"), ref_in("var.k", [[0, 1]])])]),
		pol("b", [attr("bucket", [lit("a")])]),
		locals_blk(main_tf, [attr("all", [ref("aws_s3_bucket.this")])]),
	]
	count(with_id(cox_denies(module_with(src), blocks), "LY308")) == 1
}

# 1つの block の instance どうしは比べない（README の「保証しないこと」）。
test_LY308_allow_single_multi_block if {
	src := `resource "aws_s3_bucket_policy" "a" {
  for_each = aws_s3_bucket.this
  bucket   = each.value.id
}
`
	blocks := [pol("a", [attr("for_each", [ref("aws_s3_bucket.this")]), attr("bucket", [ref("each.value.id")])])]
	msgs := cox_denies(module_with(src), blocks)
	count(with_id(msgs, "LY305")) == 0
	count(with_id(msgs, "LY308")) == 0
}

test_LY308_allow_multi_with_distinct_parent if {
	src := concat("", [two_policies("for_each = aws_s3_bucket.this", ""), "resource \"aws_s3_bucket\" \"other\" {}\n"])
	blocks := [
		pol("a", [attr("for_each", [ref("aws_s3_bucket.this")]), attr("bucket", [each_key_ref])]),
		pol("b", [attr("bucket", [ref("aws_s3_bucket.other.id")])]),
	]
	count(with_id(cox_denies(module_with(src), blocks), "LY308")) == 0
}

# --- LY306 ---

test_LY306_deny_schema_missing if {
	msgs := cox_denies_with([ok_module], [], cox_decl, null)
	only_with(with_id(msgs, "LY306"), "data.layer_schema が無いか")
}

test_LY306_deny_schema_empty if {
	only_with(with_id(cox_denies_with([ok_module], [], cox_decl, {}), "LY306"), "LY306 [schema の一覧] data.layer_schema が無いか、空か")
}

test_LY306_deny_schema_entry_without_arguments if {
	sch := object.union(cox_schema, {"aws_vpc": {"arguments": [], "deprecated": [], "blocks": []}})
	only_with(with_id(cox_denies_with([ok_module], [], cox_decl, sch), "LY306"), "親の型 aws_vpc の書ける引数が schema から1つも取れていません")
}

test_LY306_deny_deprecated_not_in_arguments if {
	sch := object.union(cox_schema, {"aws_vpc": {"arguments": ["name"], "deprecated": ["x"], "blocks": []}})
	only_with(with_id(cox_denies_with([ok_module], [], cox_decl, sch), "LY306"), "LY306 [schema の一覧]")
}

test_LY306_deny_parent_type_missing if {
	sch := object.remove(cox_schema, ["aws_route_table"])
	only_with(with_id(cox_denies_with([ok_module], [], cox_decl, sch), "LY306"), "aws_route_table")
}

test_LY306_deny_unclassified_deprecated if {
	sch := object.union(cox_schema, {"aws_iam_role": {"arguments": ["name", "inline_policy", "managed_policy_arns", "x"], "deprecated": ["inline_policy", "managed_policy_arns", "x"], "blocks": ["inline_policy"]}})
	only_with(with_id(cox_denies_with([ok_module], [], cox_decl, sch), "LY306"), "aws_iam_role の非推奨の引数 x")
}

test_LY306_deny_declared_argument_not_in_schema if {
	sch := object.union(cox_schema, {"aws_security_group": {"arguments": ["name", "ingress"], "deprecated": [], "blocks": []}})
	only_with(with_id(cox_denies_with([ok_module], [], cox_decl, sch), "LY306"), "aws_security_group.egress")
}

test_LY306_deny_stale_unrelated if {
	sch := object.union(cox_schema, {"aws_s3_bucket": {"arguments": ["bucket", "policy", "acceleration_status", "versioning"], "deprecated": ["policy"], "blocks": ["versioning"]}})
	only_with(with_id(cox_denies_with([ok_module], [], cox_decl, sch), "LY306"), "古い分類")
}

test_LY306_deny_unwritten_value_mismatch if {
	sch := object.union(cox_schema, {"aws_iam_role": {"arguments": ["name", "inline_policy", "managed_policy_arns"], "deprecated": ["inline_policy", "managed_policy_arns"], "blocks": []}})
	with_id(cox_denies_with([ok_module], [], cox_decl, sch), "LY306") == {
		"LY306 [書かないときの値] attachments.aws_iam_role_policy.parent_arguments の aws_iam_role.inline_policy の値が schema の種類と合いません（block は []、属性は null）",
		"LY306 [書かないときの値] attachments.aws_iam_role_policies_exclusive.parent_arguments の aws_iam_role.inline_policy の値が schema の種類と合いません（block は []、属性は null）",
	}
}

test_LY306_allow_classified if {
	count(with_id(cox_denies([ok_module], []), "LY306")) == 0
}

# --- LY001（決定18 の宣言の形） ---

cox_decl_with(t, a) := object.union(cox_decl, {"attachments": {t: a}})

cox_lY001(d) := with_id(cox_denies_with([ok_module], [], d, cox_schema), "LY001")

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
	only_with(with_id(cox_denies_with([ok_module], [], d, cox_schema), "LY306"), "unrelated_deprecated_arguments の aws_s3_bucket.acceleration_status")
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

test_LY304_deny_nested_path_only_in_hcl_input if {
	msgs := cox_ids(`resource "aws_iam_role" "r" {}
`, [blk(main_tf, "resource", ["aws_iam_role", "r"], [attr("inline_policy.name", [])])], "LY304")
	only_with(msgs, "aws_iam_role.r が inline_policy を書いています")
}

# 引数名を接頭辞に持つ別の引数は、その引数を書いたと数えない。
test_LY304_allow_argument_name_prefix if {
	count(cox_ids(`resource "aws_iam_role" "r" {}
`, [blk(main_tf, "resource", ["aws_iam_role", "r"], [attr("inline_policy_extra", []), attr("dynamic.inline_policy_extra.for_each", [])])], "LY304")) == 0
}

# 別の resource が書いた引数は、その resource の分だけを数える。
test_LY304_deny_only_resource_that_writes if {
	src := `resource "aws_iam_role" "r" {}

resource "aws_iam_role" "q" {}
`
	msgs := cox_ids(src, [blk(main_tf, "resource", ["aws_iam_role", "q"], [attr("managed_policy_arns", [])])], "LY304")
	only_with(msgs, "aws_iam_role.q が managed_policy_arns を書いています")
}

# --- LY305・LY308 の参照の形 ---

whole_ref := ref("aws_s3_bucket.this.id")

# multi(c) の4つの規則は、どれか1つだけでも LY305 を LY308 に変える。
multi_only(extra1, extra2, attrs1, attrs2) if {
	count(dup_denies(extra1, extra2, attrs1, attrs2, "LY305")) == 0
	count(dup_denies(extra1, extra2, attrs1, attrs2, "LY308")) == 1
}

test_LY308_deny_count_only_in_hcl2 if {
	multi_only("count = 1", "", [attr("bucket", [whole_ref])], [attr("bucket", [whole_ref])])
}

test_LY308_deny_for_each_only_in_hcl2 if {
	multi_only("for_each = toset([\"a\"])", "", [attr("bucket", [whole_ref])], [attr("bucket", [whole_ref])])
}

test_LY308_deny_count_only_in_hcl_input if {
	multi_only("", "", [attr("count", []), attr("bucket", [whole_ref])], [attr("bucket", [whole_ref])])
}

test_LY308_deny_for_each_only_in_hcl_input if {
	multi_only("", "", [attr("for_each", []), attr("bucket", [whole_ref])], [attr("bucket", [whole_ref])])
}

# 比べる組の後ろの側（c2）だけが複数の instance を持つ。
test_LY308_deny_multi_only_second if {
	multi_only("", "count = 1", [attr("bucket", [whole_ref])], [attr("bucket", [whole_ref])])
}

test_LY305_allow_no_multi if {
	count(dup_denies("", "", [attr("bucket", [whole_ref])], [attr("bucket", [whole_ref])], "LY308")) == 0
}

count_index_ref := {"root": "aws_s3_bucket", "steps": [
	{"kind": "attr", "name": "this"},
	{"kind": "index", "index": {"form": "count_index"}},
	{"kind": "attr", "name": "id"},
], "within": []}

test_LY308_deny_count_index_selection if {
	msgs := dup_denies("", "", [attr("bucket", [count_index_ref])], [attr("bucket", [whole_ref])], "LY308")
	only_with(msgs, "aws_s3_bucket_policy.a と aws_s3_bucket_policy.b が、同じ親 aws_s3_bucket.this を指し")
}

test_LY308_deny_splat_selection if {
	msgs := dup_denies("", "", [attr("bucket", [ref("aws_s3_bucket.this[*].id")])], [attr("bucket", [whole_ref])], "LY308")
	only_with(msgs, "同じ親 aws_s3_bucket.this を指し")
}

# 属性の続かない1段の参照は、親の全体を選ぶ。
test_LY305_deny_reference_without_attribute if {
	msgs := dup_denies("", "", [attr("bucket", [ref("aws_s3_bucket.this")])], [attr("bucket", [whole_ref])], "LY305")
	only_with(msgs, "同じ親 aws_s3_bucket.this の文書全体を書きます")
}

test_LY305_deny_literal_and_whole if {
	count(dup_denies("", "", [attr("bucket", [lit(1)])], [attr("bucket", [whole_ref])], "LY305")) == 1
}

test_LY305_deny_decimal_string_and_number if {
	count(dup_denies("", "", [attr("bucket", [lit("1.0")])], [attr("bucket", [lit(1)])], "LY305")) == 1
}

test_LY305_allow_distinct_string_literals if {
	msgs := cox_denies(module_with(two_policies("", "")), [pol("a", [attr("bucket", [lit("a")])]), pol("b", [attr("bucket", [lit("b")])])])
	count(with_id(msgs, "LY305")) == 0
	count(with_id(msgs, "LY308")) == 0
}

test_LY305_deny_through_two_plain_locals if {
	src := concat("", [two_policies("", ""), "locals {\n  b = local.c\n  c = aws_s3_bucket.this.id\n}\n"])
	blocks := [
		pol("a", [attr("bucket", [ref("local.b")])]),
		pol("b", [attr("bucket", [whole_ref])]),
		locals_blk(main_tf, [attr("b", [ref("local.c")]), attr("c", [whole_ref])]),
	]
	only_with(with_id(cox_denies(module_with(src), blocks), "LY305"), "同じ親 aws_s3_bucket.this")
}

# plain な local の先で local を添字で選べば、その先の葉は判定できない。
test_LY308_deny_plain_local_to_selected_local if {
	src := concat("", [two_policies("", ""), "locals {\n  b = local.all[var.k].id\n  all = aws_s3_bucket.this\n}\n"])
	blocks := [
		pol("a", [attr("bucket", [ref("local.b")])]),
		pol("b", [attr("bucket", [lit("a")])]),
		locals_blk(main_tf, [attr("b", [ref("local.all[].id"), ref_in("var.k", [[0, 1]])]), attr("all", [ref("aws_s3_bucket.this")])]),
	]
	msgs := cox_denies(module_with(src), blocks)
	count(with_id(msgs, "LY305")) == 0
	count(with_id(msgs, "LY308")) == 1
}

# local をリテラルの添字で選んでも、local の中身の添字までは辿らない。
test_LY308_deny_local_selected_by_literal if {
	src := concat("", [two_policies("", ""), "locals {\n  all = aws_s3_bucket.this\n}\n"])
	blocks := [
		pol("a", [attr("bucket", [{"root": "local", "steps": [{"kind": "attr", "name": "all"}, {"kind": "index", "index": {"form": "literal", "value": 0}}, {"kind": "attr", "name": "id"}], "within": []}])]),
		pol("b", [attr("bucket", [lit(0)])]),
		locals_blk(main_tf, [attr("all", [ref("aws_s3_bucket.this")])]),
	]
	msgs := cox_denies(module_with(src), blocks)
	count(with_id(msgs, "LY305")) == 0
	count(with_id(msgs, "LY308")) == 1
}

# 別の module の同じ名前の親と attachment は比べない。
test_LY305_allow_same_names_in_other_module if {
	other_tf := "modules/n/main.tf"
	src_b := `resource "aws_s3_bucket" "this" {}

resource "aws_s3_bucket_policy" "b" {
  bucket = "x"
}
`
	src_a := concat("", [bucket, "resource \"aws_s3_bucket_policy\" \"a\" {\n  bucket = \"x\"\n}\n"])
	files := [tf(main_tf, src_a), tf(other_tf, src_b)]
	blocks := [
		pol("a", [attr("bucket", [whole_ref])]),
		blk(other_tf, "resource", ["aws_s3_bucket_policy", "b"], [attr("bucket", [whole_ref])]),
	]
	msgs := cox_denies(files, blocks)
	count(with_id(msgs, "LY305")) == 0
	count(with_id(msgs, "LY308")) == 0
	count(with_id(msgs, "LY301")) == 0
}

# --- LY306 の schema の形 ---

cox_ly306(sch) := with_id(cox_denies_with([ok_module], [], cox_decl, sch), "LY306")

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
	msgs := with_id(cox_denies_with([ok_module], [], d, cox_schema), "LY306")
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
