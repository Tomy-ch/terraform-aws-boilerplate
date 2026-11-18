package layer

import data.lib.hcl

tf(path, src) := {"path": path, "contents": parse_config("hcl2", src)}

fixture_decl := {
	"attachments": {
		"aws_s3_bucket_policy": {"parents": {"bucket": ["aws_s3_bucket"]}, "optional": false},
		"aws_flow_log": {"parents": {"vpc_id": ["aws_vpc"], "subnet_id": ["aws_subnet"]}, "optional": true},
		"aws_ec2_tag": {"parents": {"resource_id": ["*"]}, "optional": false},
		"aws_guardduty_malware_protection_plan": {"parents": {"protected_resource.s3_bucket.bucket_name": ["aws_s3_bucket"]}, "optional": false},
		"aws_api_gateway_method_settings": {"parents": {"rest_api_id": ["aws_api_gateway_rest_api"], "stage_name": ["aws_api_gateway_stage"]}, "optional": false},
		"aws_kms_alias": {"parents": {"target_key_id": ["aws_kms_key", "aws_kms_replica_key"]}, "optional": false},
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
	},
}

denies_input(files, listed, d, h) := result if {
	result := deny with input as files
		with data.layer_decl as d
		with data.layer_files as listed
		with data.hcl_input as h
}

denies_with(files, listed, d) := denies_input(files, listed, d, go_input(files, []))

listed_of(files) := [f.path | some f in files; not endswith(f.path, ".tftest.hcl")]

denies(files) := denies_with(files, listed_of(files), fixture_decl)

# denies_go は、手で書いた block を hcl-input の出力として渡す。
denies_go(files, blocks) := denies_input(files, listed_of(files), fixture_decl, go_input(files, blocks))

# go_input は、files に対応する hcl-input の出力を作る。blocks に無い resource の block は、属性なしで足す。
go_input(files, blocks) := {
	"files": sort([f.path | some f in files; hcl.is_tf(f.path)]),
	"blocks": array.concat(blocks, [blk(c.path, "resource", [type_of(c), name_of(c)], []) |
		some c in hcl.components(files)
		startswith(c.key, "resource.")
		not written(blocks, c)
	]),
}

written(blocks, c) if {
	some b in blocks
	b.kind == "resource"
	b.file == c.path
	b.labels == [type_of(c), name_of(c)]
}

blk(file, kind, labels, attrs) := {"file": file, "kind": kind, "labels": labels, "attributes": attrs}

attr(path, refs) := {"path": path, "refs": refs}

# ref は `aws_s3_bucket.this[].id` の字面から参照を作る。`[]` は添字、`[*]` は splat の段。
ref(s) := {"root": parts[0], "steps": [step(p) | some p in array.slice(parts, 1, count(parts))], "within": []} if {
	parts := regex.find_n(`\[\*\]|\[\]|[^.\[\]]+`, s, -1)
}

step("[]") := {"kind": "index"}

step("[*]") := {"kind": "splat"}

step(p) := {"kind": "attr", "name": p} if not p in {"[]", "[*]"}

# ref_in は、refs[i] の段 j の添字の中に在る参照を作る。ws は外側からの祖先の [i, j] の並び。
ref_in(s, ws) := object.union(ref(s), {"within": [{"ref": w[0], "step": w[1]} | some w in ws]})

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

test_LY001_deny_decl_not_object if {
	msgs := denies_with([ok_module], ["modules/m/main.tf"], "x")
	some m in with_id(msgs, "LY001")
	contains(m, "attachments がオブジェクトではありません")
}

test_LY001_deny_entry_not_object if {
	d := object.union(fixture_decl, {"attachments": {"aws_x": "bucket"}})
	some m in with_id(denies_with([ok_module], ["modules/m/main.tf"], d), "LY001")
	contains(m, "attachments.aws_x")
}

test_LY001_deny_empty_argument_name if {
	d := object.union(fixture_decl, {"attachments": {"aws_x": {"parents": {"": ["aws_y"]}, "optional": false}}})
	some m in with_id(denies_with([ok_module], ["modules/m/main.tf"], d), "LY001")
	contains(m, "attachments.aws_x")
}

test_LY001_deny_empty_parent_type if {
	d := object.union(fixture_decl, {"attachments": {"aws_x": {"parents": {"a": [""]}, "optional": false}}})
	some m in with_id(denies_with([ok_module], ["modules/m/main.tf"], d), "LY001")
	contains(m, "attachments.aws_x")
}

test_LY001_deny_parent_types_as_string if {
	d := object.union(fixture_decl, {"attachments": {"aws_x": {"parents": {"a": "aws_y"}, "optional": false}}})
	some m in with_id(denies_with([ok_module], ["modules/m/main.tf"], d), "LY001")
	contains(m, "attachments.aws_x")
}

test_LY002_deny_files_list_missing if {
	msgs := deny with input as [ok_module] with data.layer_decl as fixture_decl
	some m in with_id(msgs, "LY002")
	contains(m, "modules/m/main.tf は独立の走査の一覧にありません")
}

test_LY002_deny_files_list_not_array if {
	msgs := deny with input as [] with data.layer_decl as fixture_decl with data.layer_files as "x"
	some m in with_id(msgs, "LY002")
	contains(m, "1件もありません")
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

test_LY002_allow_hcl_input_matches if {
	count(with_id(denies([ok_module]), "LY002")) == 0
}

test_LY002_deny_tf_not_in_hcl_input if {
	h := {"files": [], "blocks": [blk("modules/m/main.tf", "resource", ["aws_s3_bucket", "this"], [])]}
	some m in with_id(denies_input([ok_module], ["modules/m/main.tf"], fixture_decl, h), "LY002")
	contains(m, "modules/m/main.tf を hcl-input が読んでいません")
}

test_LY002_deny_hcl_input_file_not_read if {
	h := object.union(go_input([ok_module], []), {"files": ["modules/m/main.tf", "modules/m/extra.tf"]})
	some m in with_id(denies_input([ok_module], ["modules/m/main.tf"], fixture_decl, h), "LY002")
	contains(m, "modules/m/extra.tf は hcl-input が読みましたが、Conftest は読んでいません")
}

go_shape_denies(h) := with_id(denies_input([ok_module], ["modules/m/main.tf"], fixture_decl, h), "LY004")

# ok_blocks は、補助が作る形（blk・attr・ref・ref_in）をすべて使う。
ok_blocks := [
	blk("modules/m/main.tf", "resource", ["aws_s3_bucket", "this"], [attr("tags", [
		ref("local.all[].id"),
		ref_in("module.m.list[]", [[0, 1]]),
		ref_in("var.i", [[0, 1], [1, 2]]),
		ref("aws_s3_bucket.this[*]"),
	])]),
	blk("modules/m/main.tf", "locals", [], [attr("all", [])]),
]

test_LY004_allow_helper_shapes if {
	count(go_shape_denies(go_input([ok_module], ok_blocks))) == 0
}

test_LY004_deny_missing if {
	msgs := deny with input as [ok_module] with data.layer_decl as fixture_decl with data.layer_files as ["modules/m/main.tf"]
	some m in with_id(msgs, "LY004")
	contains(m, "data.hcl_input がオブジェクトではありません")
}

test_LY004_deny_not_object if {
	some m in go_shape_denies([])
	contains(m, "data.hcl_input がオブジェクトではありません")
}

test_LY004_deny_files_not_strings if {
	some m in go_shape_denies({"files": [1], "blocks": []})
	contains(m, "hcl_input.files")
}

test_LY004_deny_blocks_missing if {
	some m in go_shape_denies({"files": ["modules/m/main.tf"]})
	contains(m, "hcl_input.blocks が配列ではありません")
}

test_LY004_deny_block_extra_key if {
	b := object.union(blk("modules/m/main.tf", "resource", ["aws_s3_bucket", "this"], []), {"line": 1})
	some m in go_shape_denies({"files": ["modules/m/main.tf"], "blocks": [b]})
	contains(m, "hcl_input.blocks[0] が")
}

test_LY004_deny_labels_not_strings if {
	some m in go_shape_denies({"files": ["modules/m/main.tf"], "blocks": [blk("modules/m/main.tf", "resource", ["aws_s3_bucket", 1], [])]})
	contains(m, "hcl_input.blocks[0] が")
}

shape_of_attr(a) := go_shape_denies({"files": ["modules/m/main.tf"], "blocks": [blk("modules/m/main.tf", "resource", ["aws_s3_bucket", "this"], [a])]})

test_LY004_deny_refs_missing if {
	some m in shape_of_attr({"path": "tags"})
	contains(m, "hcl_input.blocks[0].attributes[0]")
}

test_LY004_deny_empty_path if {
	some m in shape_of_attr(attr("", []))
	contains(m, "hcl_input.blocks[0].attributes[0]")
}

test_LY004_deny_ref_without_within if {
	some m in shape_of_attr(attr("tags", [{"root": "var", "steps": []}]))
	contains(m, "hcl_input.blocks[0].attributes[0]")
}

test_LY004_deny_unknown_step if {
	some m in shape_of_attr(attr("tags", [{"root": "var", "steps": [{"kind": "call"}], "within": []}]))
	contains(m, "hcl_input.blocks[0].attributes[0]")
}

test_LY004_deny_attr_step_without_name if {
	some m in shape_of_attr(attr("tags", [{"root": "var", "steps": [{"kind": "attr"}], "within": []}]))
	contains(m, "hcl_input.blocks[0].attributes[0]")
}

test_LY004_deny_index_step_with_name if {
	some m in shape_of_attr(attr("tags", [{"root": "var", "steps": [{"kind": "index", "name": "x"}], "within": []}]))
	contains(m, "hcl_input.blocks[0].attributes[0]")
}

test_LY004_deny_within_out_of_range if {
	some m in shape_of_attr(attr("tags", [ref("aws_s3_bucket.this[]"), ref_in("var.i", [[0, 2]])]))
	contains(m, "hcl_input.blocks[0].attributes[0]")
}

test_LY004_deny_within_not_index if {
	some m in shape_of_attr(attr("tags", [ref("aws_s3_bucket.this[]"), ref_in("var.i", [[0, 0]])]))
	contains(m, "hcl_input.blocks[0].attributes[0]")
}

test_LY004_deny_within_unknown_ref if {
	some m in shape_of_attr(attr("tags", [ref_in("var.i", [[3, 1]])]))
	contains(m, "hcl_input.blocks[0].attributes[0]")
}

test_LY004_deny_alone_on_broken_input if {
	files := module_with(policy("aws_s3_bucket.this.id"))
	msgs := denies_input(files, listed_of(files), fixture_decl, {"files": "x", "blocks": []})
	count(with_id(msgs, "LY004")) == 1
	count(msgs) == 1
}

test_LY005_allow_matching_resources if {
	count(with_id(denies_go(module_with(policy("aws_s3_bucket.this.id")), [policy_blk([ref("aws_s3_bucket.this.id")])]), "LY005")) == 0
}

test_LY005_deny_resource_missing_from_hcl_input if {
	some m in with_id(denies_input([ok_module], ["modules/m/main.tf"], fixture_decl, {"files": ["modules/m/main.tf"], "blocks": []}), "LY005")
	contains(m, "modules/m/main.tf の resource aws_s3_bucket.this を hcl-input が出していません")
}

test_LY005_deny_resource_not_read_by_conftest if {
	h := go_input([ok_module], [blk("modules/m/main.tf", "resource", ["aws_s3_bucket", "extra"], [])])
	some m in with_id(denies_input([ok_module], ["modules/m/main.tf"], fixture_decl, h), "LY005")
	contains(m, "hcl-input が出した modules/m/main.tf の resource aws_s3_bucket.extra を Conftest が読んでいません")
}

test_LY005_deny_parent_argument_missing_from_hcl_input if {
	b := blk("modules/m/main.tf", "resource", ["aws_s3_bucket_policy", "this"], [])
	some m in with_id(denies_go(module_with(policy("aws_s3_bucket.this.id")), [b]), "LY005")
	contains(m, "aws_s3_bucket_policy.this の bucket が、Conftest では 1 個、hcl-input では 0 個です")
}

test_LY005_deny_for_each_count if {
	src := `resource "aws_s3_bucket_policy" "this" {
	for_each = local.keys
	bucket   = aws_s3_bucket.this.id
}
`
	some m in with_id(denies_go(module_with(src), [policy_blk([ref("aws_s3_bucket.this.id")])]), "LY005")
	contains(m, "aws_s3_bucket_policy.this の for_each が、Conftest では 1 個、hcl-input では 0 個です")
}

test_LY005_deny_extra_parent_argument_in_hcl_input if {
	b := blk("modules/m/main.tf", "resource", ["aws_s3_bucket_policy", "this"], [attr("bucket", []), attr("bucket", [])])
	some m in with_id(denies_go(module_with(policy("aws_s3_bucket.this.id")), [b]), "LY005")
	contains(m, "Conftest では 1 個、hcl-input では 2 個です")
}
