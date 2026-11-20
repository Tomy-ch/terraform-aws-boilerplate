package lib.hcl_input

# fixture は conftest/hcl/lib/fixtures の txtar を hcl-input fixtures が JSON にしたもの（data.fixtures）。
fx(name) := data.fixtures[name]

lib_cases := {"lib_components", "lib_bodies", "lib_sources", "lib_unresolved"}

test_fixtures_present if {
	every name in lib_cases {
		count(fx(name).files) > 0
	}
	{k | some k, _ in data.fixtures; startswith(k, "lib_")} == lib_cases
}

block(doc, type, labels) := b if {
	some b in doc.blocks
	b.type == type
	b.labels == labels
}

test_components_reads_every_kind_and_check_data if {
	{[c.id, c.key, c.path] | some c in components(fx("lib_components"))} == {
		["m#resource.aws_s3_bucket.b", "resource.aws_s3_bucket", "m/main.tf"],
		["m#data.aws_region.r", "data.aws_region", "m/main.tf"],
		["m#ephemeral.aws_x.e", "ephemeral.aws_x", "m/main.tf"],
		["m#action.aws_lambda_invoke.a", "action.aws_lambda_invoke", "m/main.tf"],
		["m#data.aws_caller_identity.me", "data.aws_caller_identity", "m/main.tf"],
	}
}

test_component_scope_of_check_data_is_the_nested_block if {
	some c in components(fx("lib_components"))
	c.id == "m#data.aws_caller_identity.me"
	c.scope.block.type == "check"
	c.scope.block.blocks[c.scope.at].type == "data"
}

test_has_provisioner if {
	doc := fx("lib_bodies")
	has_provisioner(scope_of(block(doc, "resource", ["aws_instance", "i"])))
	has_provisioner(scope_of(block(doc, "resource", ["aws_spot_fleet_request", "f"])))
	not has_provisioner(scope_of(block(doc, "resource", ["aws_s3_bucket", "plain"])))
}

test_values_at_reads_attributes_nested_blocks_and_folded_dynamic if {
	doc := fx("lib_bodies")
	[v.expr | some v in values_at(scope_of(block(doc, "resource", ["aws_instance", "i"])), "user_data")] == [{"kind": "literal", "value": "a"}]
	[v.expr | some v in values_at(scope_of(block(doc, "resource", ["aws_spot_fleet_request", "f"])), "launch_specification.user_data")] == [{"kind": "literal", "value": "d"}]
	[v.type | some v in values_at(scope_of(block(doc, "provider", ["kubernetes"])), "exec")] == ["exec"]
	values_at(scope_of(block(doc, "resource", ["aws_s3_bucket", "plain"])), "tags.exec") == []
}

test_has_key_anywhere if {
	doc := fx("lib_bodies")
	has_key_anywhere(scope_of(block(doc, "provider", ["kubernetes"])), "exec")
	has_key_anywhere(scope_of(block(doc, "provider", ["kubernetes"])), "api_version")
	has_key_anywhere(scope_of(block(doc, "resource", ["aws_spot_fleet_request", "f"])), "provisioner")
	has_key_anywhere(scope_of(block(doc, "resource", ["aws_s3_bucket", "plain"])), "exec")
	has_key_anywhere(scope_of(block(doc, "provider", ["helm"])), "exec")
	not has_key_anywhere(scope_of(block(doc, "resource", ["aws_s3_bucket", "plain"])), "nothere")
	not has_key_anywhere(scope_of(block(doc, "provider", ["helm"])), "nothere")
}

test_removed_blocks if {
	{[r.id, has_provisioner(r.scope)] | some r in removed_blocks(fx("lib_bodies")); has_provisioner(r.scope)} == {["m#removed.aws_s3_bucket.old", true]}
	{r.id | some r in removed_blocks(fx("lib_bodies"))} == {"m#removed.aws_s3_bucket.old", "m#removed.<解けない from>"}
}

test_providers_identity if {
	{[p.id, p.module] | some p in providers(fx("lib_bodies"))} == {
		["m#provider.kubernetes", "m"],
		["m#provider.helm", "m"],
		["m#provider.aws.use1", "m"],
		["m#provider.aws.<解けない alias>", "m"],
		["m/tests/x.tftest.hcl#provider.aws.use1", "m"],
		["m/tests/x.tftest.hcl#provider.aws", "m"],
	}
}

test_required_sources if {
	doc := fx("lib_sources")
	required_sources(doc) == {
		["m", "aws", "hashicorp/aws"],
		["m", "k8s", "hashicorp/k8s"],
		["m", "gh", "integrations/github"],
		["m", "dyn", null],
		["m", "mixed", "Example/Mixed"],
	}
	provider_source(doc, "m", "gh") == "registry.terraform.io/integrations/github"
	provider_source(doc, "m", "dyn") == null
	provider_source(doc, "m", "other") == "registry.terraform.io/hashicorp/other"
	declared_source(doc, "m", "aws")
	not declared_source(doc, "m", "other")
}

test_module_calls if {
	module_calls(fx("lib_sources")) == {
		{"path": "m/main.tf", "dir": "m", "name": "lit", "source": "../n"},
		{"path": "m/main.tf", "dir": "m", "name": "expr", "source": null},
		{"path": "m/main.tf", "dir": "m", "name": "none", "source": null},
	}
	module_count(fx("lib_sources"), "m/main.tf") == 3
	module_count(fx("lib_sources"), "m/versions.tf") == 0
}

test_backends_and_cloud_blocks if {
	[b] := [b | some b in backends(fx("lib_sources"))]
	b.path == "m/versions.tf"
	b.type == "s3"
	object.keys(b.attrs) == {"key", "use_lockfile", "encrypt"}
	b.attrs.key == {"kind": "literal", "value": "k"}
	b.attrs.encrypt.kind == "traversal"
	{c.path | some c in cloud_blocks(fx("lib_sources"))} == {"m/versions.tf"}
}

test_relative_source_and_module_target_reject_interpolated_literals if {
	not relative_source("./${var.x}")
	not relative_source("../${var.x}/m")
	not relative_source("modules/a")
	relative_source("./a")
	not module_target("env/dev", "./${var.x}")
	module_target("env/dev", "./a/../b") == "env/dev/b"
	not module_target("a", "../../b")
}

test_address_of if {
	address_of({"kind": "traversal", "root": "aws_x", "steps": [{"kind": "attr", "name": "y"}]}) == "aws_x.y"
	not address_of({"kind": "traversal", "root": "aws_x", "steps": [{"kind": "attr", "name": "y"}, {"kind": "index", "index": {"form": "other"}}]})
	not address_of({"kind": "literal", "value": "aws_x.y"})
}

test_literal_string if {
	literal_string({"kind": "literal", "value": "a"}) == "a"
	not literal_string({"kind": "literal", "value": 1})
	not literal_string({"kind": "template", "parts": []})
}

test_in_subtree if {
	b := {"blocks": [{"parent": -1}, {"parent": 0}, {"parent": 1}, {"parent": -1}, {"parent": 3}]}
	in_subtree(b, 0, 1)
	in_subtree(b, 0, 2)
	not in_subtree(b, 0, 3)
	not in_subtree(b, 0, 4)
	in_subtree(b, 3, 4)
	not in_subtree(b, 1, 0)
}

# --- 形の検査 ---

ok_doc := {"files": [{"path": "m/a.tf", "parsed": true}], "blocks": [{
	"file": "m/a.tf", "line": 1, "type": "resource", "labels": ["a", "b"],
	"blocks": [{"path": "n", "parent": -1, "line": 2, "type": "n", "labels": [], "dynamic": false}],
	"attributes": [
		{"path": "n.x", "parent": 0, "line": 3, "refs": [], "expr": {"kind": "call", "name": "file", "expand": false, "args": [
			{"kind": "template", "parts": [{"kind": "traversal", "root": "path", "steps": [{"kind": "attr", "name": "module"}]}, {"kind": "literal", "value": "/x"}]},
		]}},
		{"path": "y", "parent": -1, "line": 4, "refs": [], "expr": {"kind": "object", "items": [
			{"key": {"kind": "literal", "value": "k"}, "value": {"kind": "traversal", "root": "var", "steps": [{"kind": "splat"}, {"kind": "index", "index": {"form": "literal", "value": 0}}]}},
		]}},
	],
}]}

# shape_one は、ok_doc に1つの書き換えを掛けた入力の形の崩れの数を返す。
shape_one(op) := count(shape_errors(json.patch(ok_doc, [op])))

test_shape_errors_deny_each_broken_field if {
	ops := [
		{"op": "replace", "path": "/files/0/path", "value": 1},
		{"op": "add", "path": "/files/0/extra", "value": 1},
		{"op": "replace", "path": "/blocks/0/file", "value": ""},
		{"op": "replace", "path": "/blocks/0/line", "value": "1"},
		{"op": "replace", "path": "/blocks/0/type", "value": ""},
		{"op": "replace", "path": "/blocks/0/labels", "value": "a"},
		{"op": "replace", "path": "/blocks/0/labels/0", "value": 1},
		{"op": "replace", "path": "/blocks/0/blocks", "value": {}},
		{"op": "replace", "path": "/blocks/0/attributes", "value": {}},
		{"op": "replace", "path": "/blocks/0/blocks/0/line", "value": null},
		{"op": "replace", "path": "/blocks/0/blocks/0/labels", "value": null},
		{"op": "replace", "path": "/blocks/0/blocks/0/dynamic", "value": "no"},
		{"op": "replace", "path": "/blocks/0/blocks/0/path", "value": ""},
		{"op": "replace", "path": "/blocks/0/blocks/0/type", "value": ""},
		{"op": "replace", "path": "/blocks/0/blocks/0/parent", "value": -2},
		{"op": "add", "path": "/blocks/0/blocks/0/extra", "value": 1},
		{"op": "replace", "path": "/blocks/0/attributes/0/line", "value": "3"},
		{"op": "replace", "path": "/blocks/0/attributes/0/path", "value": ""},
		{"op": "replace", "path": "/blocks/0/attributes/0/refs", "value": {}},
		{"op": "replace", "path": "/blocks/0/attributes/0/parent", "value": -2},
		{"op": "add", "path": "/blocks/0/attributes/0/extra", "value": 1},
		{"op": "replace", "path": "/blocks/0/attributes/0/expr/args/0/kind", "value": "lambda"},
		{"op": "replace", "path": "/blocks/0/attributes/0/expr/name", "value": ""},
		{"op": "replace", "path": "/blocks/0/attributes/0/expr/expand", "value": "no"},
		{"op": "replace", "path": "/blocks/0/attributes/0/expr/args", "value": {}},
		{"op": "replace", "path": "/blocks/0/attributes/0/expr/args/0/parts", "value": {}},
		{"op": "replace", "path": "/blocks/0/attributes/0/expr/args/0/parts/0/root", "value": ""},
		{"op": "replace", "path": "/blocks/0/attributes/0/expr/args/0/parts/0/steps", "value": {}},
		{"op": "replace", "path": "/blocks/0/attributes/1/expr/items", "value": {}},
		{"op": "add", "path": "/blocks/0/attributes/1/expr/items/0/extra", "value": 1},
		{"op": "add", "path": "/blocks/0/attributes/1/expr/items/0/value/steps/0/name", "value": "x"},
		{"op": "replace", "path": "/blocks/0/attributes/1/expr/items/0/value/steps/1/kind", "value": "call"},
		{"op": "replace", "path": "/blocks/0/attributes/1/expr/items/0/value/steps/1", "value": {"kind": "attr"}},
		{"op": "remove", "path": "/blocks/0/attributes/1/expr/items/0/value/steps/1/index"},
		{"op": "replace", "path": "/blocks/0/attributes/1/expr/items/0/value/steps/1/index/form", "value": "each"},
		{"op": "remove", "path": "/blocks/0/attributes/1/expr/items/0/value/steps/1/index/value"},
		{"op": "replace", "path": "/blocks/0/attributes/1/expr/items/0/value/steps/1/index/value", "value": true},
		{"op": "replace", "path": "/blocks/0/attributes/1/expr/items/0/value/steps/1/index", "value": {"form": "other", "value": 0}},
		{"op": "remove", "path": "/blocks/0/attributes/1/expr/items/0/key/value"},
	]
	every op in ops {
		shape_one(op) == 1
	}
}

test_shape_errors_allow_well_formed if {
	shape_errors(ok_doc) == set()
	shape_errors(fx("lib_bodies")) == set()
	shape_errors(fx("lib_sources")) == set()
}

test_shape_errors_deny_malformed if {
	shape_errors([]) == {"入力がオブジェクトではありません"}
	shape_errors({"files": {}, "blocks": []}) == {"files が配列ではありません"}
	shape_errors({"files": [], "blocks": null}) == {"blocks が配列ではありません"}
	shape_errors({"files": [], "blocks": [], "extra": 1}) == {"未知のキー extra があります"}
	count(shape_errors({"files": [{"path": "", "parsed": true}], "blocks": []})) == 1
	count(shape_errors({"files": [{"path": "a", "parsed": "yes"}], "blocks": []})) == 1
	count(shape_errors(json.patch(ok_doc, [{"op": "add", "path": "/blocks/0/extra", "value": 1}]))) == 1
	count(shape_errors(json.patch(ok_doc, [{"op": "replace", "path": "/blocks/0/blocks/0/parent", "value": 0}]))) == 1
	count(shape_errors(json.patch(ok_doc, [{"op": "replace", "path": "/blocks/0/attributes/0/parent", "value": 1}]))) == 1
	count(shape_errors(json.patch(ok_doc, [{"op": "replace", "path": "/blocks/0/attributes/0/expr/kind", "value": "lambda"}]))) == 1
	count(shape_errors(json.patch(ok_doc, [{"op": "replace", "path": "/blocks/0/attributes/0/expr/args/0/parts/1/kind", "value": "lambda"}]))) == 1
	count(shape_errors(json.patch(ok_doc, [{"op": "remove", "path": "/blocks/0/attributes/0/expr/args/0/parts/0/steps/0/name"}]))) == 1
	count(shape_errors(json.patch(ok_doc, [{"op": "remove", "path": "/blocks/0/attributes/0/expr/expand"}]))) == 1
}

# --- 独立の数との突き合わせ ---

test_count_errors_allow_matching if {
	count_errors(ok_doc, ["m/a.tf"], {"m/a.tf": 1}) == set()
}

test_count_errors_deny_block_of_unparsed_file_and_duplicate_path if {
	doc := json.patch(ok_doc, [{"op": "add", "path": "/files/-", "value": {"path": "m/b.md", "parsed": false}}, {"op": "replace", "path": "/blocks/0/file", "value": "m/b.md"}])
	count_errors(doc, ["m/a.tf"], {"m/a.tf": 0}) == {"m/b.md の block を読みましたが、構文として読んだファイルの一覧にありません"}
	dup := json.patch(ok_doc, [{"op": "add", "path": "/files/-", "value": {"path": "m/a.tf", "parsed": true}}])
	count_errors(dup, ["m/a.tf"], {"m/a.tf": 1}) == {"files に m/a.tf が 2 回あります"}
}

test_count_errors_deny_mismatch if {
	count_errors(ok_doc, ["m/a.tf", "m/b.tf"], {"m/a.tf": 1}) == {"m/b.tf を構文として読んでいません（find は列挙しています）"}
	count_errors(ok_doc, [], {"m/a.tf": 1}) == {"m/a.tf を構文として読みましたが、find の一覧にありません"}
	count_errors(ok_doc, ["m/a.tf"], {"m/a.tf": 2}) == {"m/a.tf のトップレベルの block を 1 個読みましたが、行頭の数えは 2 個です"}
	count_errors(ok_doc, ["m/a.tf"], {}) == {"m/a.tf のトップレベルの block を 1 個読みましたが、行頭の数えは 数えていない 個です"}
	count_errors(ok_doc, null, {"m/a.tf": 1}) == {"hcl_parsed_files（find の一覧）が配列ではありません"}
	count_errors(ok_doc, ["m/a.tf"], null) == {"hcl_block_counts（行頭の数え）がオブジェクトではありません"}
}

test_dir if {
	dir("modules/a/main.tf") == "modules/a"
	dir("main.tf") == "."
}

test_module_dir if {
	module_dir("modules/a/tests/x.tftest.hcl") == "modules/a"
	module_dir("modules/a/x.tftest.hcl") == "modules/a"
	module_dir("tests/x.tftest.hcl") == "."
	module_dir("x.tftest.hcl") == "."
	module_dir("modules/a/main.tf") == "modules/a"
}

test_clean if {
	clean("m/./src/../src/a.py") == "m/src/a.py"
	clean("m/a/../../b") == "b"
	clean("m//a") == "m/a"
	clean("a/b/..") == "a"
	clean("a/../b") == "b"
	clean("a/..") == ""
	clean(".") == ""
	not clean("m/../../x")
	not clean("../x")
	not clean("a/../..")
}

test_normalize_source if {
	normalize_source("HashiCorp/AWS") == "registry.terraform.io/hashicorp/aws"
	normalize_source("registry.terraform.io/HashiCorp/AWS") == "registry.terraform.io/hashicorp/aws"
	normalize_source("example.com/o/p") == "example.com/o/p"
}

test_module_target_resolves_relative_literals if {
	module_target("env/dev", "../../usecases/u") == "usecases/u"
	module_target("usecases/u", "./internal/x") == "usecases/u/internal/x"
}

test_module_target_rejects_non_relative_and_escaping_sources if {
	not module_target("env/dev", "git::https://example.com/m.git")
	not module_target("env/dev", "${var.src}/m")
	not module_target("env/dev", "../${var.x}")
	not module_target("env/dev", "../../../outside")
	not module_target("env/dev", null)
}

test_module_target_rejects_sources_that_are_not_relative_paths if {
	not module_target("env/dev", ".")
	not module_target("env/dev", "..")
	not module_target("env/dev", "/abs/path")
	not module_target("env/dev", 1)
}

test_unresolved_expr if {
	doc := fx("lib_unresolved")
	unresolved_expr(scope_of(block(doc, "provider", ["helm"])))
	unresolved_expr(scope_of(block(doc, "provider", ["kubectl"])))
	every name in ["nested", "objval", "callarg", "tuplekey", "secondkey"] {
		unresolved_expr(scope_of(block(doc, "provider", [name])))
	}
	not unresolved_expr(scope_of(block(doc, "provider", ["aws"])))
}
