package layer

# 参照の取り出しは hcl-input が持つ（Go のテストと tree_test.rego が固定する）。ここでは規則（辿り方）を見る。

bucket := `resource "aws_s3_bucket" "this" {}
`

main_tf := "modules/m/main.tf"

policy(expr) := sprintf("resource \"aws_s3_bucket_policy\" \"this\" {\n  bucket = %s\n}\n", [expr])

# policy_denies は、bucket の式 expr を hcl-input が refs と読んだときの LY301 を返す。
test_LY301_allow_direct_parent if {
	count(with_id(denies(fx("LY301_allow_direct_parent")), "LY301")) == 0
}

test_LY301_allow_parent_through_locals_and_index if {
	count(with_id(denies(fx("LY301_allow_parent_through_locals_and_index")), "LY301")) == 0
}

test_LY301_allow_parent_through_each_value if {
	count(with_id(denies(fx("LY301_allow_parent_through_each_value")), "LY301")) == 0
}

test_LY301_allow_any_parent_type_in_same_module if {
	count(with_id(denies(fx("LY301_allow_any_parent_type_in_same_module")), "LY301")) == 0
}

test_LY301_deny_index_after_attribute if {
	some m in with_id(denies(fx("LY301_deny_index_after_attribute")), "LY301")
	contains(m, ": module.m（")
}

test_LY301_deny_second_index_after_reference if {
	some m in with_id(denies(fx("LY301_deny_second_index_after_reference")), "LY301")
	contains(m, ": module.m（")
}

test_LY301_allow_reference_in_first_index if {
	count(with_id(denies(fx("LY301_allow_reference_in_first_index")), "LY301")) == 0
}

test_LY301_allow_reference_nested_in_first_index if {
	count(with_id(denies(fx("LY301_allow_reference_nested_in_first_index")), "LY301")) == 0
}

# 1つ目の添字でも、参照の根の直後（段0）の添字は親を選ばない。
test_LY301_deny_index_directly_after_root if {
	some m in with_id(denies(fx("LY301_deny_index_directly_after_root")), "LY301")
	contains(m, "module.m")
}

test_LY301_allow_splat_and_key_index if {
	count(with_id(denies(fx("LY301_allow_splat_and_key_index_1")), "LY301")) == 0
	count(with_id(denies(fx("LY301_allow_splat_and_key_index_2")), "LY301")) == 0
}

test_LY301_deny_undefined_local if {
	some m in with_id(denies(fx("LY301_deny_undefined_local")), "LY301")
	contains(m, "local.nope")
}

test_LY301_deny_module_reference if {
	some m in with_id(denies(fx("LY301_deny_module_reference")), "LY301")
	contains(m, "module.b")
}

test_LY301_deny_variable if {
	some m in with_id(denies(fx("LY301_deny_variable")), "LY301")
	contains(m, "var.bucket")
}

test_LY301_deny_data_source if {
	some m in with_id(denies(fx("LY301_deny_data_source")), "LY301")
	contains(m, "data.aws_s3_bucket")
}

test_LY301_deny_root_only_reference if {
	some m in with_id(denies(fx("LY301_deny_root_only_reference")), "LY301")
	contains(m, ": x（")
}

test_LY301_deny_literal if {
	some m in with_id(denies(fx("LY301_deny_literal")), "LY301")
	contains(m, "参照がありません")
}

test_LY301_deny_wrong_parent_type if {
	some m in with_id(denies(fx("LY301_deny_wrong_parent_type")), "LY301")
	contains(m, "aws_sqs_queue.q")
}

test_LY301_deny_parent_not_declared_in_module if {
	some m in with_id(denies(fx("LY301_deny_parent_not_declared_in_module")), "LY301")
	contains(m, "aws_s3_bucket.elsewhere")
}

test_LY301_deny_parent_in_other_module if {
	count(with_id(denies(fx("LY301_deny_parent_in_other_module")), "LY301")) == 1
}

test_LY301_deny_module_through_two_locals if {
	some m in with_id(denies(fx("LY301_deny_module_through_two_locals")), "LY301")
	contains(m, "module.m")
}

test_LY301_deny_module_through_each_value if {
	some m in with_id(denies(fx("LY301_deny_module_through_each_value")), "LY301")
	contains(m, "module.m")
}

test_LY301_deny_module_through_each_key if {
	some m in with_id(denies(fx("LY301_deny_module_through_each_key")), "LY301")
	contains(m, "module.m")
}

test_LY301_deny_nested_parent if {
	msgs := with_id(denies(fx("LY301_deny_nested_parent")), "LY301")
	count(msgs) == 1
	some m in msgs
	contains(m, "protected_resource.s3_bucket.bucket_name")
	contains(m, "module.b")
}

dynamic_plan(expr) := concat("\n", [
	`resource "aws_guardduty_malware_protection_plan" "this" {`,
	"\tdynamic \"protected_resource\" {",
	"\t\tfor_each = [1]",
	"\t\tcontent {",
	"\t\t\ts3_bucket {",
	sprintf("\t\t\t\tbucket_name = %s", [expr]),
	"\t\t\t}",
	"\t\t}",
	"\t}",
	"}",
])

test_LY301_deny_parent_in_dynamic if {
	msgs := with_id(denies(fx("LY301_deny_parent_in_dynamic")), "LY301")
	count(msgs) == 1
	some m in msgs
	contains(m, "protected_resource.s3_bucket.bucket_name")
	contains(m, "module.b")
}

test_LY301_allow_parent_in_dynamic if {
	count(with_id(denies(fx("LY301_allow_parent_in_dynamic")), "LY301")) == 0
}

test_LY301_allow_count_index_outside_subscript if {
	count(with_id(denies(fx("LY301_allow_count_index_outside_subscript")), "LY301")) == 0
}

test_LY301_deny_local_resolving_to_literal if {
	some m in with_id(denies(fx("LY301_deny_local_resolving_to_literal")), "LY301")
	contains(m, "参照がありません")
}

test_LY301_deny_local_of_other_module if {
	some m in with_id(denies(fx("LY301_deny_local_of_other_module")), "LY301")
	contains(m, "local.x")
}

test_LY301_allow_local_in_sibling_file if {
	count(with_id(denies(fx("LY301_allow_local_in_sibling_file")), "LY301")) == 0
}

test_LY301_allow_local_defined_twice if {
	count(with_id(denies(fx("LY301_allow_local_defined_twice")), "LY301")) == 0
}

test_LY301_deny_each_inside_local if {
	some m in with_id(denies(fx("LY301_deny_each_inside_local")), "LY301")
	contains(m, "each.value")
}

test_LY301_allow_second_parent_type if {
	count(with_id(denies(fx("LY301_allow_second_parent_type")), "LY301")) == 0
}

test_LY301_deny_any_type_without_resource_in_module if {
	some m in with_id(denies(fx("LY301_deny_any_type_without_resource_in_module")), "LY301")
	contains(m, "aws_vpc.missing")
}

test_LY301_deny_one_of_several_parent_arguments if {
	some m in with_id(denies(fx("LY301_deny_one_of_several_parent_arguments")), "LY301")
	contains(m, "subnet_id")
}

test_LY301_deny_any_type_from_other_module if {
	count(with_id(denies(fx("LY301_deny_any_type_from_other_module")), "LY301")) == 1
}

test_LY302_allow_optional_without_parent if {
	count(with_id(denies(fx("LY302_allow_optional_without_parent")), "LY302")) == 0
}

test_LY302_deny_required_parent_missing if {
	some m in with_id(denies(fx("LY302_deny_required_parent_missing")), "LY302")
	contains(m, "（bucket）")
}

test_LY302_allow_one_of_several_required_parents if {
	count(with_id(denies(fx("LY302_allow_one_of_several_required_parents")), "LY302")) == 0
}

test_LY302_deny_none_of_several_required_parents if {
	some m in with_id(denies(fx("LY302_deny_none_of_several_required_parents")), "LY302")
	contains(m, "（rest_api_id, stage_name）")
}

test_LY303_allow_classified_types if {
	count(with_id(denies(fx("LY303_allow_classified_types")), "LY303")) == 0
}

test_LY303_deny_parent_type_not_classified if {
	d := {"attachments": fixture_decl.attachments, "not_attachments": object.remove(fixture_decl.not_attachments, ["aws_s3_bucket"])}
	msgs := denies_with(fx("LY301_allow_direct_parent"), listed_of(fx("LY301_allow_direct_parent")), d)
	some m in with_id(msgs, "LY303")
	contains(m, "aws_s3_bucket")
}

test_LY303_deny_unclassified_type if {
	some m in with_id(denies(fx("LY303_deny_unclassified_type")), "LY303")
	contains(m, "aws_sqs_queue")
}
