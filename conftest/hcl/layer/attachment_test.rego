package layer

# 参照の取り出しは hcl-input が持つ（Go のテストと layer-test.mk の通しが固定する）。ここでは、手で書いた参照の上で
# 規則（辿り方）だけを見る。HCL のソースは Conftest の側の block の構造（resource と親の引数の有無）を作る。

bucket := `resource "aws_s3_bucket" "this" {}
`

main_tf := "modules/m/main.tf"

policy(expr) := sprintf("resource \"aws_s3_bucket_policy\" \"this\" {\n  bucket = %s\n}\n", [expr])

policy_blk(refs) := policy_blk_with([attr("bucket", refs)])

policy_blk_with(attrs) := blk(main_tf, "resource", ["aws_s3_bucket_policy", "this"], attrs)

locals_blk(file, attrs) := blk(file, "locals", [], attrs)

module_with(src) := [tf(main_tf, concat("", [bucket, src]))]

attachment_denies(src, blocks) := with_id(denies_go(module_with(src), blocks), "LY301")

# policy_denies は、bucket の式 expr を hcl-input が refs と読んだときの LY301 を返す。
policy_denies(expr, refs) := attachment_denies(policy(expr), [policy_blk(refs)])

test_LY301_allow_direct_parent if {
	count(policy_denies("aws_s3_bucket.this.id", [ref("aws_s3_bucket.this.id")])) == 0
}

test_LY301_allow_parent_through_locals_and_index if {
	src := `locals {
	all = merge(aws_s3_bucket.this, { extra = aws_s3_bucket.other })
	keys = { a = "${var.prefix}-a" }
}

resource "aws_s3_bucket" "other" {}

resource "aws_s3_bucket_policy" "this" {
	for_each = local.keys
	bucket   = local.all[each.key].id
}
`
	blocks := [
		locals_blk(main_tf, [
			attr("all", [ref("aws_s3_bucket.this"), ref("aws_s3_bucket.other")]),
			attr("keys", [ref("var.prefix")]),
		]),
		policy_blk_with([
			attr("for_each", [ref("local.keys")]),
			attr("bucket", [ref("local.all[].id"), ref_in("each.key", [[0, 1]])]),
		]),
	]
	count(attachment_denies(src, blocks)) == 0
}

test_LY301_allow_parent_through_each_value if {
	src := `resource "aws_s3_bucket_policy" "this" {
	for_each = { a = aws_s3_bucket.this.id }
	bucket   = each.value
}
`
	blocks := [policy_blk_with([attr("for_each", [ref("aws_s3_bucket.this.id")]), attr("bucket", [ref("each.value")])])]
	count(attachment_denies(src, blocks)) == 0
}

test_LY301_allow_any_parent_type_in_same_module if {
	src := `resource "aws_ec2_tag" "this" {
	resource_id = aws_s3_bucket.this.id
}
`
	blocks := [blk(main_tf, "resource", ["aws_ec2_tag", "this"], [attr("resource_id", [ref("aws_s3_bucket.this.id")])])]
	count(attachment_denies(src, blocks)) == 0
}

test_LY301_deny_index_after_attribute if {
	some m in policy_denies(`aws_s3_bucket.this.tags["a"][module.m.x]`, [ref("aws_s3_bucket.this.tags[][]"), ref_in("module.m.x", [[0, 3]])])
	contains(m, ": module.m（")
}

test_LY301_deny_second_index_after_reference if {
	some m in policy_denies("aws_s3_bucket.this[0][module.m.x]", [ref("aws_s3_bucket.this[][]"), ref_in("module.m.x", [[0, 2]])])
	contains(m, ": module.m（")
}

test_LY301_allow_reference_in_first_index if {
	count(policy_denies("aws_s3_bucket.this[module.x.key].id", [ref("aws_s3_bucket.this[].id"), ref_in("module.x.key", [[0, 1]])])) == 0
}

test_LY301_allow_reference_nested_in_first_index if {
	refs := [ref("aws_s3_bucket.this[]"), ref_in("module.m.list[]", [[0, 1]]), ref_in("var.i", [[0, 1], [1, 2]])]
	count(policy_denies("aws_s3_bucket.this[module.m.list[var.i]]", refs)) == 0
}

# 1つ目の添字でも、参照の根の直後（段0）の添字は親を選ばない。
test_LY301_deny_index_directly_after_root if {
	refs := [ref("aws_s3_bucket[]"), ref_in("module.m.x", [[0, 0]])]
	some m in policy_denies("aws_s3_bucket[module.m.x]", refs)
	contains(m, "module.m")
}

test_LY301_allow_splat_and_key_index if {
	count(policy_denies(`one(aws_s3_bucket.this[*].id)`, [ref("aws_s3_bucket.this[*]")])) == 0
	count(policy_denies(`aws_s3_bucket.this["x"].id`, [ref("aws_s3_bucket.this[].id")])) == 0
}

test_LY301_deny_undefined_local if {
	some m in policy_denies(`"${local.nope}${aws_s3_bucket.this.id}"`, [ref("local.nope"), ref("aws_s3_bucket.this.id")])
	contains(m, "local.nope")
}

test_LY301_deny_module_reference if {
	some m in policy_denies("module.b.id", [ref("module.b.id")])
	contains(m, "module.b")
}

test_LY301_deny_variable if {
	some m in policy_denies("var.bucket", [ref("var.bucket")])
	contains(m, "var.bucket")
}

test_LY301_deny_data_source if {
	some m in policy_denies("data.aws_s3_bucket.x.id", [ref("data.aws_s3_bucket.x.id")])
	contains(m, "data.aws_s3_bucket")
}

test_LY301_deny_root_only_reference if {
	some m in policy_denies("x", [ref("x")])
	contains(m, ": x（")
}

test_LY301_deny_literal if {
	some m in policy_denies(`"my-bucket"`, [])
	contains(m, "参照がありません")
}

test_LY301_deny_wrong_parent_type if {
	src := `resource "aws_sqs_queue" "q" {}

resource "aws_s3_bucket_policy" "this" {
	bucket = aws_sqs_queue.q.id
}
`
	some m in attachment_denies(src, [policy_blk([ref("aws_sqs_queue.q.id")])])
	contains(m, "aws_sqs_queue.q")
}

test_LY301_deny_parent_not_declared_in_module if {
	some m in policy_denies("aws_s3_bucket.elsewhere.id", [ref("aws_s3_bucket.elsewhere.id")])
	contains(m, "aws_s3_bucket.elsewhere")
}

test_LY301_deny_parent_in_other_module if {
	files := [
		tf(main_tf, policy("aws_s3_bucket.this.id")),
		tf("modules/n/main.tf", bucket),
	]
	count(with_id(denies_go(files, [policy_blk([ref("aws_s3_bucket.this.id")])]), "LY301")) == 1
}

test_LY301_deny_module_through_two_locals if {
	src := `locals {
	a = local.b
	b = { x = module.m.id }
}

resource "aws_s3_bucket_policy" "this" {
	bucket = local.a.x
}
`
	blocks := [
		locals_blk(main_tf, [attr("a", [ref("local.b")]), attr("b", [ref("module.m.id")])]),
		policy_blk([ref("local.a.x")]),
	]
	some m in attachment_denies(src, blocks)
	contains(m, "module.m")
}

test_LY301_deny_module_through_each_value if {
	src := `resource "aws_s3_bucket_policy" "this" {
	for_each = module.m.ids
	bucket   = each.value
}
`
	blocks := [policy_blk_with([attr("for_each", [ref("module.m.ids")]), attr("bucket", [ref("each.value")])])]
	some m in attachment_denies(src, blocks)
	contains(m, "module.m")
}

test_LY301_deny_module_through_each_key if {
	src := `resource "aws_s3_bucket_policy" "this" {
	for_each = toset(module.m.names)
	bucket   = each.key
}
`
	blocks := [policy_blk_with([attr("for_each", [ref("module.m.names")]), attr("bucket", [ref("each.key")])])]
	some m in attachment_denies(src, blocks)
	contains(m, "module.m")
}

test_LY301_deny_nested_parent if {
	src := `resource "aws_guardduty_malware_protection_plan" "this" {
	protected_resource {
		s3_bucket {
			bucket_name = module.b.name
		}
	}
}
`
	blocks := [blk(main_tf, "resource", ["aws_guardduty_malware_protection_plan", "this"], [attr("protected_resource.s3_bucket.bucket_name", [ref("module.b.name")])])]
	msgs := attachment_denies(src, blocks)
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

dynamic_plan_blk(refs) := blk(main_tf, "resource", ["aws_guardduty_malware_protection_plan", "this"], [
	attr("dynamic.protected_resource.for_each", []),
	attr("protected_resource.s3_bucket.bucket_name", refs),
])

test_LY301_deny_parent_in_dynamic if {
	msgs := attachment_denies(dynamic_plan("module.b.name"), [dynamic_plan_blk([ref("module.b.name")])])
	count(msgs) == 1
	some m in msgs
	contains(m, "protected_resource.s3_bucket.bucket_name")
	contains(m, "module.b")
}

test_LY301_allow_parent_in_dynamic if {
	count(attachment_denies(dynamic_plan("aws_s3_bucket.this.bucket"), [dynamic_plan_blk([ref("aws_s3_bucket.this.bucket")])])) == 0
}

test_LY301_allow_count_index_outside_subscript if {
	count(policy_denies("element([aws_s3_bucket.this], count.index).id", [ref("aws_s3_bucket.this"), ref("count.index")])) == 0
}

test_LY301_deny_local_resolving_to_literal if {
	src := concat("\n", ["locals {", "\tx = \"my-bucket\"", "}", "", policy("local.x")])
	some m in attachment_denies(src, [locals_blk(main_tf, [attr("x", [])]), policy_blk([ref("local.x")])])
	contains(m, "参照がありません")
}

test_LY301_deny_local_of_other_module if {
	files := [
		tf(main_tf, concat("", [bucket, policy("local.x")])),
		tf("modules/n/main.tf", concat("\n", [bucket, "locals {", "\tx = aws_s3_bucket.this.id", "}"])),
	]
	blocks := [policy_blk([ref("local.x")]), locals_blk("modules/n/main.tf", [attr("x", [ref("aws_s3_bucket.this.id")])])]
	some m in with_id(denies_go(files, blocks), "LY301")
	contains(m, "local.x")
}

test_LY301_allow_local_in_sibling_file if {
	files := [
		tf(main_tf, concat("", [bucket, policy("local.x")])),
		tf("modules/m/locals.tf", concat("\n", ["locals {", "\tx = aws_s3_bucket.this.id", "}"])),
	]
	blocks := [policy_blk([ref("local.x")]), locals_blk("modules/m/locals.tf", [attr("x", [ref("aws_s3_bucket.this.id")])])]
	count(with_id(denies_go(files, blocks), "LY301")) == 0
}

test_LY301_allow_local_defined_twice if {
	files := [
		tf(main_tf, concat("", [bucket, policy("local.x")])),
		tf("modules/m/a.tf", concat("\n", ["locals {", "\tx = aws_s3_bucket.this.id", "}"])),
		tf("modules/m/b.tf", concat("\n", ["locals {", "\tx = aws_s3_bucket.this.arn", "}"])),
	]
	blocks := [
		policy_blk([ref("local.x")]),
		locals_blk("modules/m/a.tf", [attr("x", [ref("aws_s3_bucket.this.id")])]),
		locals_blk("modules/m/b.tf", [attr("x", [ref("aws_s3_bucket.this.arn")])]),
	]
	count(with_id(denies_go(files, blocks), "LY301")) == 0
}

test_LY301_deny_each_inside_local if {
	src := concat("\n", ["locals {", "\tx = each.value", "}", "", policy("local.x")])
	some m in attachment_denies(src, [locals_blk(main_tf, [attr("x", [ref("each.value")])]), policy_blk([ref("local.x")])])
	contains(m, "each.value")
}

test_LY301_allow_second_parent_type if {
	src := concat("\n", [
		`resource "aws_kms_replica_key" "this" {}`,
		"",
		`resource "aws_kms_alias" "this" {`,
		"\ttarget_key_id = aws_kms_replica_key.this.id",
		"}",
	])
	blocks := [blk(main_tf, "resource", ["aws_kms_alias", "this"], [attr("target_key_id", [ref("aws_kms_replica_key.this.id")])])]
	count(attachment_denies(src, blocks)) == 0
}

test_LY301_deny_any_type_without_resource_in_module if {
	files := [tf(main_tf, "resource \"aws_ec2_tag\" \"this\" {\n\tresource_id = aws_vpc.missing.id\n}\n")]
	blocks := [blk(main_tf, "resource", ["aws_ec2_tag", "this"], [attr("resource_id", [ref("aws_vpc.missing.id")])])]
	some m in with_id(denies_go(files, blocks), "LY301")
	contains(m, "aws_vpc.missing")
}

test_LY301_deny_one_of_several_parent_arguments if {
	src := `resource "aws_vpc" "this" {}

resource "aws_flow_log" "this" {
	vpc_id    = aws_vpc.this.id
	subnet_id = var.subnet
}
`
	blocks := [blk(main_tf, "resource", ["aws_flow_log", "this"], [attr("vpc_id", [ref("aws_vpc.this.id")]), attr("subnet_id", [ref("var.subnet")])])]
	some m in attachment_denies(src, blocks)
	contains(m, "subnet_id")
}

test_LY301_deny_any_type_from_other_module if {
	src := `resource "aws_ec2_tag" "this" {
	resource_id = module.vpc.id
}
`
	blocks := [blk(main_tf, "resource", ["aws_ec2_tag", "this"], [attr("resource_id", [ref("module.vpc.id")])])]
	count(attachment_denies(src, blocks)) == 1
}

test_LY302_allow_optional_without_parent if {
	count(with_id(denies(module_with(`resource "aws_flow_log" "this" {}`)), "LY302")) == 0
}

test_LY302_deny_required_parent_missing if {
	some m in with_id(denies(module_with(`resource "aws_s3_bucket_policy" "this" {}`)), "LY302")
	contains(m, "（bucket）")
}

test_LY302_allow_one_of_several_required_parents if {
	src := concat("\n", [
		`resource "aws_api_gateway_rest_api" "this" {}`,
		"",
		`resource "aws_api_gateway_method_settings" "this" {`,
		"\trest_api_id = aws_api_gateway_rest_api.this.id",
		"}",
	])
	blocks := [blk(main_tf, "resource", ["aws_api_gateway_method_settings", "this"], [attr("rest_api_id", [ref("aws_api_gateway_rest_api.this.id")])])]
	count(with_id(denies_go([tf(main_tf, src)], blocks), "LY302")) == 0
}

test_LY302_deny_none_of_several_required_parents if {
	some m in with_id(denies([tf("modules/m/main.tf", `resource "aws_api_gateway_method_settings" "this" {}`)]), "LY302")
	contains(m, "（rest_api_id, stage_name）")
}

test_LY303_allow_classified_types if {
	src := `resource "aws_lb_listener" "this" {}

resource "aws_vpc" "this" {}
`
	count(with_id(denies(module_with(src)), "LY303")) == 0
}

test_LY303_deny_parent_type_not_classified if {
	d := {"attachments": fixture_decl.attachments, "not_attachments": object.remove(fixture_decl.not_attachments, ["aws_s3_bucket"])}
	files := module_with(policy("aws_s3_bucket.this.id"))
	msgs := denies_with(files, [f.path | some f in files], d)
	some m in with_id(msgs, "LY303")
	contains(m, "aws_s3_bucket")
}

test_LY303_deny_unclassified_type if {
	some m in with_id(denies(module_with(`resource "aws_sqs_queue" "q" {}`)), "LY303")
	contains(m, "aws_sqs_queue")
}
