package layer

bucket := `resource "aws_s3_bucket" "this" {}
`

policy(expr) := sprintf("resource \"aws_s3_bucket_policy\" \"this\" {\n  bucket = %s\n}\n", [expr])

module_with(src) := [tf("modules/m/main.tf", concat("", [bucket, src]))]

attachment_denies(src) := with_id(denies(module_with(src)), "LY301")

test_LY301_allow_direct_parent if {
	count(attachment_denies(policy("aws_s3_bucket.this.id"))) == 0
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
	count(attachment_denies(src)) == 0
}

test_LY301_allow_parent_through_each_value if {
	src := `resource "aws_s3_bucket_policy" "this" {
	for_each = { a = aws_s3_bucket.this.id }
	bucket   = each.value
}
`
	count(attachment_denies(src)) == 0
}

test_LY301_allow_template_with_parent if {
	count(attachment_denies(policy(`"${aws_s3_bucket.this.id}"`))) == 0
}

test_LY301_allow_any_parent_type_in_same_module if {
	src := `resource "aws_ec2_tag" "this" {
	resource_id = aws_s3_bucket.this.id
}
`
	count(attachment_denies(src)) == 0
}

test_LY301_allow_for_expression_iterator if {
	count(attachment_denies(policy("one([for b in [aws_s3_bucket.this] : b.id])"))) == 0
}

test_LY301_allow_literal_after_interpolation if {
	count(attachment_denies(policy(`"${aws_s3_bucket.this.id}-foo.bar"`))) == 0
}

test_LY301_allow_quoted_literal_in_expression if {
	count(attachment_denies(policy(`try(aws_s3_bucket.this.id, "a.b")`))) == 0
}

test_LY301_deny_interpolation_after_literal_bracket if {
	some m in attachment_denies(policy(`"${aws_s3_bucket.this.id}-x[${module.x.id}]"`))
	contains(m, "module.x")
}

test_LY301_deny_escaped_interpolation if {
	some m in attachment_denies(policy(`"$${aws_s3_bucket.this.id}"`))
	contains(m, "参照がありません")
}

test_LY301_deny_undefined_local if {
	some m in attachment_denies(policy(`"${local.nope}${aws_s3_bucket.this.id}"`))
	contains(m, "local.nope")
}

test_LY301_deny_unreadable_nesting if {
	some m in attachment_denies(policy(`"${aws_s3_bucket.this.id}${ {a = {b = {c = {d = module.x.id}}}} }"`))
	contains(m, unreadable)
}

test_LY301_deny_module_inside_quoted_template if {
	some m in attachment_denies(policy(`"${aws_s3_bucket.this.id}${"${module.x.id}"}"`))
	contains(m, "module.x")
}

test_LY301_deny_module_reference if {
	some m in attachment_denies(policy("module.b.id"))
	contains(m, "module.b")
}

test_LY301_deny_variable if {
	some m in attachment_denies(policy("var.bucket"))
	contains(m, "var.bucket")
}

test_LY301_deny_data_source if {
	some m in attachment_denies(policy("data.aws_s3_bucket.x.id"))
	contains(m, "data.aws_s3_bucket")
}

test_LY301_deny_literal if {
	some m in attachment_denies(policy(`"my-bucket"`))
	contains(m, "参照がありません")
}

test_LY301_deny_wrong_parent_type if {
	src := `resource "aws_sqs_queue" "q" {}

resource "aws_s3_bucket_policy" "this" {
	bucket = aws_sqs_queue.q.id
}
`
	some m in attachment_denies(src)
	contains(m, "aws_sqs_queue.q")
}

test_LY301_deny_parent_not_declared_in_module if {
	some m in attachment_denies(policy("aws_s3_bucket.elsewhere.id"))
	contains(m, "aws_s3_bucket.elsewhere")
}

test_LY301_deny_parent_in_other_module if {
	files := [
		tf("modules/m/main.tf", policy("aws_s3_bucket.this.id")),
		tf("modules/n/main.tf", bucket),
	]
	count(with_id(denies(files), "LY301")) == 1
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
	some m in attachment_denies(src)
	contains(m, "module.m")
}

test_LY301_deny_module_through_each_value if {
	src := `resource "aws_s3_bucket_policy" "this" {
	for_each = module.m.ids
	bucket   = each.value
}
`
	some m in attachment_denies(src)
	contains(m, "module.m")
}

test_LY301_deny_module_through_each_key if {
	src := `resource "aws_s3_bucket_policy" "this" {
	for_each = toset(module.m.names)
	bucket   = each.key
}
`
	some m in attachment_denies(src)
	contains(m, "module.m")
}

test_LY301_deny_module_in_template if {
	some m in attachment_denies(policy(`"${aws_s3_bucket.this.id}${module.x.suffix}"`))
	contains(m, "module.x")
}

test_LY301_deny_module_in_list_literal_indexed if {
	some m in attachment_denies(policy("[module.x.id][0]"))
	contains(m, "module.x")
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
	some m in attachment_denies(src)
	contains(m, "protected_resource.s3_bucket.bucket_name")
}

test_LY301_deny_parent_in_dynamic if {
	src := `resource "aws_guardduty_malware_protection_plan" "this" {
	dynamic "protected_resource" {
		for_each = [1]
		content {
			s3_bucket {
				bucket_name = module.b.name
			}
		}
	}
}
`
	count(attachment_denies(src)) == 1
}

test_LY301_deny_one_of_several_parent_arguments if {
	src := `resource "aws_vpc" "this" {}

resource "aws_flow_log" "this" {
	vpc_id    = aws_vpc.this.id
	subnet_id = var.subnet
}
`
	some m in attachment_denies(src)
	contains(m, "subnet_id")
}

test_LY301_deny_any_type_from_other_module if {
	src := `resource "aws_ec2_tag" "this" {
	resource_id = module.vpc.id
}
`
	count(attachment_denies(src)) == 1
}

test_LY302_allow_optional_without_parent if {
	count(with_id(denies(module_with(`resource "aws_flow_log" "this" {}`)), "LY302")) == 0
}

test_LY302_deny_required_parent_missing if {
	some m in with_id(denies(module_with(`resource "aws_s3_bucket_policy" "this" {}`)), "LY302")
	contains(m, "bucket")
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
