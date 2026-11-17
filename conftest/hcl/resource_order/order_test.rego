package resource_order

rule := {
	"module": "m",
	"resource": "aws_iam_role.this",
	"after": "aws_iam_role_policy.first",
	"adr": "docs/adr/x.md",
}

ok_src := `resource "aws_iam_role_policy" "first" {}

resource "aws_iam_role" "this" {
	depends_on = [aws_iam_role_policy.first]
}
`

tf(path, src) := {"path": path, "contents": parse_config("hcl2", src)}

denies(files, listed, rules) := result if {
	result := deny with input as files
		with data.resource_order_decl as {"rules": rules}
		with data.resource_order_files as listed
}

with_id(msgs, id) := {m | some m in msgs; startswith(m, concat("", [id, " "]))}

test_RO001_allow_declared_rule if {
	count(denies([tf("m/main.tf", ok_src)], ["m/main.tf"], [rule])) == 0
}

test_RO001_deny_no_rules if {
	count(with_id(denies([tf("m/main.tf", ok_src)], ["m/main.tf"], []), "RO001")) == 1
}

test_RO001_deny_rule_without_adr if {
	msgs := denies([tf("m/main.tf", ok_src)], ["m/main.tf"], [object.remove(rule, ["adr"])])
	count(with_id(msgs, "RO001")) == 1
}

test_RO001_deny_address_with_module_prefix if {
	msgs := denies([tf("m/main.tf", ok_src)], ["m/main.tf"], [object.union(rule, {"after": "module.x.aws_iam_role_policy.first"})])
	count(with_id(msgs, "RO001")) == 1
}

test_RO002_allow_tftest_not_listed if {
	files := [tf("m/main.tf", ok_src), tf("m/tests/a.tftest.hcl", `run "a" {}`)]
	count(denies(files, ["m/main.tf"], [rule])) == 0
}

test_RO002_deny_zero_files if {
	msgs := denies([], [], [rule])
	some m in with_id(msgs, "RO002")
	contains(m, "1件もありません")
}

test_RO002_deny_listed_but_not_read if {
	msgs := denies([tf("m/main.tf", ok_src)], ["m/main.tf", "m/extra.tf"], [rule])
	some m in with_id(msgs, "RO002")
	contains(m, "m/extra.tf を Conftest が読んでいません")
}

test_RO002_deny_read_but_not_listed if {
	msgs := denies([tf("m/main.tf", ok_src), tf("n/main.tf", "")], ["m/main.tf"], [rule])
	some m in with_id(msgs, "RO002")
	contains(m, "n/main.tf は独立の走査の一覧にありません")
}

test_RO003_allow_module_in_nested_dir if {
	r := object.union(rule, {"module": "a/b/m"})
	count(denies([tf("a/b/m/main.tf", ok_src)], ["a/b/m/main.tf"], [r])) == 0
}

test_RO003_deny_module_missing if {
	msgs := denies([tf("other/main.tf", ok_src)], ["other/main.tf"], [rule])
	count(with_id(msgs, "RO003")) == 1
	count(with_id(msgs, "RO004")) == 0
}

test_RO004_allow_resources_in_separate_files if {
	files := [
		tf("m/policy.tf", `resource "aws_iam_role_policy" "first" {}`),
		tf("m/main.tf", "resource \"aws_iam_role\" \"this\" {\n  depends_on = [aws_iam_role_policy.first]\n}\n"),
	]
	count(denies(files, ["m/main.tf", "m/policy.tf"], [rule])) == 0
}

test_RO004_deny_resource_missing if {
	msgs := denies([tf("m/main.tf", `resource "aws_iam_role_policy" "first" {}`)], ["m/main.tf"], [rule])
	some m in with_id(msgs, "RO004")
	contains(m, "aws_iam_role.this が 0 件")
}

test_RO004_deny_after_missing if {
	msgs := denies([tf("m/main.tf", "resource \"aws_iam_role\" \"this\" {\n  depends_on = [aws_iam_role_policy.first]\n}\n")], ["m/main.tf"], [rule])
	some m in with_id(msgs, "RO004")
	contains(m, "aws_iam_role_policy.first が 0 件")
}

test_RO004_deny_resource_in_other_module if {
	files := [tf("m/main.tf", `resource "aws_iam_role_policy" "first" {}`), tf("n/main.tf", ok_src)]
	msgs := denies(files, ["m/main.tf", "n/main.tf"], [rule])
	count(with_id(msgs, "RO004")) == 1
}

test_RO005_allow_among_other_dependencies if {
	src := "resource \"aws_iam_role_policy\" \"first\" {}\nresource \"aws_s3_bucket\" \"b\" {}\nresource \"aws_iam_role\" \"this\" {\n  depends_on = [aws_s3_bucket.b, aws_iam_role_policy.first]\n}\n"
	count(denies([tf("m/main.tf", src)], ["m/main.tf"], [rule])) == 0
}

test_RO005_deny_without_depends_on if {
	src := "resource \"aws_iam_role_policy\" \"first\" {}\nresource \"aws_iam_role\" \"this\" {}\n"
	msgs := denies([tf("m/main.tf", src)], ["m/main.tf"], [rule])
	some m in with_id(msgs, "RO005")
	contains(m, "m/main.tf の aws_iam_role.this が depends_on に aws_iam_role_policy.first を持ちません")
}

test_RO005_deny_depends_on_other_resource if {
	src := "resource \"aws_iam_role_policy\" \"first\" {}\nresource \"aws_iam_role_policy\" \"other\" {}\nresource \"aws_iam_role\" \"this\" {\n  depends_on = [aws_iam_role_policy.other]\n}\n"
	msgs := denies([tf("m/main.tf", src)], ["m/main.tf"], [rule])
	count(with_id(msgs, "RO005")) == 1
}

test_RO001_deny_rules_not_array if {
	msgs := deny with input as [tf("m/main.tf", ok_src)]
		with data.resource_order_decl as {"rules": rule}
		with data.resource_order_files as ["m/main.tf"]
	count(with_id(msgs, "RO001")) == 1
}

test_RO001_deny_empty_value if {
	msgs := denies([tf("m/main.tf", ok_src)], ["m/main.tf"], [object.union(rule, {"module": ""})])
	count(with_id(msgs, "RO001")) == 1
}

test_RO001_deny_rule_as_string if {
	msgs := denies([tf("m/main.tf", ok_src)], ["m/main.tf"], ["m#aws_iam_role.this"])
	count(with_id(msgs, "RO001")) == 1
}

test_RO001_deny_extra_key if {
	msgs := denies([tf("m/main.tf", ok_src)], ["m/main.tf"], [object.union(rule, {"note": "x"})])
	count(with_id(msgs, "RO001")) == 1
}

test_RO006_allow_hcl_only if {
	count(with_id(denies([tf("m/main.tf", ok_src)], ["m/main.tf"], [rule]), "RO006")) == 0
}

test_RO006_deny_json_config if {
	files := [tf("m/main.tf", ok_src), {"path": "m/override.tf.json", "contents": {"resource": {}}}]
	msgs := denies(files, ["m/main.tf", "m/override.tf.json"], [rule])
	some m in with_id(msgs, "RO006")
	contains(m, "m/override.tf.json")
	count(with_id(msgs, "RO002")) == 0
}
