package layer

tf(path, src) := {"path": path, "contents": parse_config("hcl2", src)}

fixture_decl := {
	"attachments": {
		"aws_s3_bucket_policy": {"parents": {"bucket": ["aws_s3_bucket"]}, "optional": false},
		"aws_flow_log": {"parents": {"vpc_id": ["aws_vpc"], "subnet_id": ["aws_subnet"]}, "optional": true},
		"aws_ec2_tag": {"parents": {"resource_id": ["*"]}, "optional": false},
		"aws_guardduty_malware_protection_plan": {"parents": {"protected_resource.s3_bucket.bucket_name": ["aws_s3_bucket"]}, "optional": false},
	},
	"not_attachments": {
		"aws_lb_listener": "子 resource",
		"aws_s3_bucket": "親になる resource",
		"aws_vpc": "親になる resource",
		"aws_subnet": "子 resource",
	},
}

denies_with(files, listed, d) := result if {
	result := deny with input as files
		with data.layer_decl as d
		with data.layer_files as listed
}

denies(files) := denies_with(files, [f.path | some f in files; not endswith(f.path, ".tftest.hcl")], fixture_decl)

with_id(msgs, id) := {m | some m in msgs; startswith(m, concat("", [id, " "]))}

ok_module := tf("modules/m/main.tf", `resource "aws_s3_bucket" "this" {}
`)

test_LY001_allow_declared if {
	count(denies([ok_module])) == 0
}

test_LY001_deny_attachments_missing if {
	msgs := denies_with([ok_module], ["modules/m/main.tf"], {"not_attachments": {}})
	count(with_id(msgs, "LY001")) == 1
}

test_LY001_deny_attachments_empty if {
	msgs := denies_with([ok_module], ["modules/m/main.tf"], {"attachments": {}, "not_attachments": {}})
	some m in with_id(msgs, "LY001")
	contains(m, "空です")
}

test_LY001_deny_attachments_array if {
	msgs := denies_with([ok_module], ["modules/m/main.tf"], {"attachments": [], "not_attachments": {}})
	count(with_id(msgs, "LY001")) == 1
}

test_LY001_deny_parents_empty if {
	d := object.union(fixture_decl, {"attachments": {"aws_x": {"parents": {}, "optional": false}}})
	msgs := denies_with([ok_module], ["modules/m/main.tf"], d)
	some m in with_id(msgs, "LY001")
	contains(m, "attachments.aws_x")
}

test_LY001_deny_parent_types_empty if {
	d := object.union(fixture_decl, {"attachments": {"aws_x": {"parents": {"a": []}, "optional": false}}})
	msgs := denies_with([ok_module], ["modules/m/main.tf"], d)
	some m in with_id(msgs, "LY001")
	contains(m, "attachments.aws_x")
}

test_LY001_deny_parents_as_list if {
	d := object.union(fixture_decl, {"attachments": {"aws_x": {"parents": ["a"], "optional": false}}})
	msgs := denies_with([ok_module], ["modules/m/main.tf"], d)
	some m in with_id(msgs, "LY001")
	contains(m, "attachments.aws_x")
}

test_LY001_deny_optional_not_boolean if {
	d := object.union(fixture_decl, {"attachments": {"aws_x": {"parents": {"a": ["aws_y"]}, "optional": "false"}}})
	msgs := denies_with([ok_module], ["modules/m/main.tf"], d)
	some m in with_id(msgs, "LY001")
	contains(m, "attachments.aws_x")
}

test_LY001_deny_extra_key if {
	d := object.union(fixture_decl, {"attachments": {"aws_x": {"parents": {"a": ["aws_y"]}, "optional": false, "skip": true}}})
	msgs := denies_with([ok_module], ["modules/m/main.tf"], d)
	some m in with_id(msgs, "LY001")
	contains(m, "attachments.aws_x")
}

test_LY001_deny_not_attachments_missing if {
	msgs := denies_with([ok_module], ["modules/m/main.tf"], {"attachments": fixture_decl.attachments})
	some m in with_id(msgs, "LY001")
	contains(m, "not_attachments がオブジェクトではありません")
}

test_LY001_deny_empty_reason if {
	d := object.union(fixture_decl, {"not_attachments": {"aws_lb_listener": ""}})
	msgs := denies_with([ok_module], ["modules/m/main.tf"], d)
	some m in with_id(msgs, "LY001")
	contains(m, "not_attachments.aws_lb_listener")
}

test_LY001_deny_type_in_both if {
	d := object.union(fixture_decl, {"not_attachments": {"aws_s3_bucket_policy": "子 resource"}})
	msgs := denies_with([ok_module], ["modules/m/main.tf"], d)
	some m in with_id(msgs, "LY001")
	contains(m, "両方")
}

test_LY002_allow_tftest_not_listed if {
	files := [ok_module, tf("modules/m/tests/a.tftest.hcl", `run "a" {}`)]
	count(denies_with(files, ["modules/m/main.tf"], fixture_decl)) == 0
}

test_LY002_deny_zero_files if {
	some m in with_id(denies_with([], [], fixture_decl), "LY002")
	contains(m, "1件もありません")
}

test_LY002_deny_listed_but_not_read if {
	msgs := denies_with([ok_module], ["modules/m/main.tf", "modules/m/extra.tf"], fixture_decl)
	some m in with_id(msgs, "LY002")
	contains(m, "modules/m/extra.tf を Conftest が読んでいません")
}

test_LY002_deny_read_but_not_listed if {
	msgs := denies_with([ok_module, tf("modules/n/main.tf", "")], ["modules/m/main.tf"], fixture_decl)
	some m in with_id(msgs, "LY002")
	contains(m, "modules/n/main.tf は独立の走査の一覧にありません")
}

test_LY003_allow_hcl_only if {
	count(with_id(denies([ok_module]), "LY003")) == 0
}

test_LY003_deny_tf_json if {
	files := [ok_module, {"path": "modules/m/override.tf.json", "contents": {"resource": {}}}]
	some m in with_id(denies(files), "LY003")
	contains(m, "modules/m/override.tf.json")
}
