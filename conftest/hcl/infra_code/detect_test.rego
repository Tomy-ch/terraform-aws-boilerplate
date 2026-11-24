package infra_code

base_decl := {
	"types": {
		"resource.aws_s3_bucket": {"executes_code": false},
		"resource.aws_instance": {"executes_code": true, "body_args": ["user_data"]},
		"resource.terraform_data": {"executes_code": false},
		"data.external": {"executes_code": true, "path_args": ["program"]},
		"data.aws_region": {"executes_code": false},
	},
	"providers": {
		"registry.terraform.io/hashicorp/aws": {"command_blocks": []},
		"registry.terraform.io/hashicorp/kubernetes": {"command_blocks": []},
		"registry.terraform.io/example/runner": {"command_blocks": ["runner.command"]},
	},
	"languages": {},
	"allowlist": [],
}

# fixture は conftest/hcl/infra_code/fixtures の txtar を hcl-input fixtures が JSON にしたもの（data.fixtures）。
# 独立の数は fixture そのものから作り、count_errors を素通しにする。突き合わせの規則は input_test.rego が見る。
fx(name) := data.fixtures[name]

parsed_of(doc) := [f.path | some f in doc.files; f.parsed]

empty_doc := {"files": [], "blocks": []}

counts_of(doc) := {p: count([b | some b in doc.blocks; b.file == p]) | some p in parsed_of(doc)}

denies(doc, d) := result if {
	result := deny with input as doc
		with data.infra_code_decl as d
		with data.hcl_parsed_files as parsed_of(doc)
		with data.hcl_block_counts as counts_of(doc)
}

with_id(msgs, id) := {m | some m in msgs; startswith(m, concat("", [id, " "]))}

test_IC010_deny_unclassified_type if {
	msgs := denies(fx("IC010_deny_unclassified_type"), base_decl)
	count(with_id(msgs, "IC010")) == 1
}

test_IC010_deny_unclassified_data_inside_check if {
	msgs := denies(fx("IC010_deny_unclassified_data_inside_check"), base_decl)
	count(with_id(msgs, "IC010")) == 1
}

test_IC010_allow_classified_types if {
	msgs := denies(fx("IC010_allow_classified_types"), base_decl)
	count(msgs) == 0
}

test_IC011_deny_code_executing_type if {
	msgs := denies(fx("IC011_allow_listed_code"), base_decl)
	some m in with_id(msgs, "IC011")
	contains(m, "m#resource.aws_instance.i")
}

test_IC011_deny_provisioner_on_any_type if {
	msgs := denies(fx("IC011_deny_provisioner_on_any_type"), base_decl)
	some m in with_id(msgs, "IC011")
	contains(m, "provisioner")
}

test_IC011_deny_dynamic_provisioner if {
	msgs := denies(fx("IC011_deny_dynamic_provisioner"), base_decl)
	count(with_id(msgs, "IC011")) == 1
}

test_IC011_deny_external_inside_check if {
	msgs := denies(fx("IC011_deny_external_inside_check"), base_decl)
	some m in with_id(msgs, "IC011")
	contains(m, "m#data.external.e")
}

test_IC011_deny_provider_exec_at_any_depth if {
	msgs := denies(fx("IC011_deny_provider_exec_at_any_depth"), base_decl)
	some m in with_id(msgs, "IC011")
	contains(m, "m#provider.kubernetes")
}

test_IC011_deny_provider_exec_in_tftest if {
	msgs := denies(fx("IC011_deny_provider_exec_in_tftest"), base_decl)
	some m in with_id(msgs, "IC011")
	contains(m, "m/tests/a.tftest.hcl#provider.kubernetes")
}

test_IC011_deny_declared_command_block if {
	msgs := denies(fx("IC011_deny_declared_command_block"), base_decl)
	some m in with_id(msgs, "IC011")
	contains(m, "provider の runner.command")
}

test_IC011_deny_override_file_adding_provisioner if {
	count(with_id(denies(fx("IC011_deny_override_file_adding_provisioner"), base_decl), "IC011")) == 1
}

test_IC011_deny_provisioner_in_removed_block if {
	msgs := denies(fx("IC011_deny_provisioner_in_removed_block"), base_decl)
	some m in with_id(msgs, "IC011")
	contains(m, "m#removed.aws_s3_bucket.old")
}

test_IC011_allow_removed_block_without_provisioner if {
	msgs := denies(fx("IC011_allow_removed_block_without_provisioner"), base_decl)
	count(msgs) == 0
}

test_IC011_allow_listed_code if {
	d := object.union(base_decl, {"allowlist": [{"id": "m#resource.aws_instance.i", "owner": "external", "repository": "github.com/o/r", "reasons": ["type"]}]})
	msgs := denies(fx("IC011_allow_listed_code"), d)
	count(msgs) == 0
}

test_IC004_deny_missing_or_non_boolean_executes_code if {
	d := object.union(base_decl, {"types": object.union(base_decl.types, {
		"resource.aws_lambda_invocation": {},
		"resource.aws_lambda_function": {"executes_code": "yes"},
	})})
	count(with_id(denies(empty_doc, d), "IC004")) == 2
}

test_IC012_deny_misclassified_must_execute_type if {
	d := object.union(base_decl, {"types": object.union(base_decl.types, {"resource.aws_launch_template": {"executes_code": false}})})
	msgs := denies(fx("IC002_allow_hcl"), d)
	count(with_id(msgs, "IC012")) == 1
}

test_IC012_allow_must_execute_types_on_the_executing_side if {
	msgs := denies(fx("IC002_allow_hcl"), base_decl)
	count(with_id(msgs, "IC012")) == 0
}

test_IC013_deny_unclassified_provider if {
	msgs := denies(fx("IC013_deny_unclassified_provider"), base_decl)
	some m in with_id(msgs, "IC013")
	contains(m, "registry.terraform.io/hashicorp/github")
}

test_IC013_deny_required_source_without_provider_block if {
	msgs := denies(fx("IC013_deny_required_source_without_provider_block"), base_decl)
	some m in with_id(msgs, "IC013")
	contains(m, "registry.terraform.io/evil/aws")
}

test_IC013_allow_classified_provider_with_required_source if {
	count(denies(fx("IC013_allow_classified_provider_with_required_source"), base_decl)) == 0
}

test_IC014_deny_non_literal_source if {
	msgs := denies(fx("IC014_deny_non_literal_source"), base_decl)
	count(with_id(msgs, "IC014")) == 1
	count(with_id(msgs, "IC013")) == 0
}

test_IC014_deny_non_literal_required_source_without_provider_block if {
	count(with_id(denies(fx("IC014_deny_non_literal_required_source_without_provider_block"), base_decl), "IC014")) == 1
}

test_IC014_allow_literal_source if {
	count(denies(fx("IC014_allow_literal_source"), base_decl)) == 0
}

test_IC004_deny_unknown_keys if {
	d := object.union(base_decl, {
		"extra": true,
		"types": {"resource.aws_s3_bucket": {"executes_code": false, "x": 1}},
		"providers": {"registry.terraform.io/hashicorp/aws": {"y": 1}},
		"allowlist": [{"id": "a", "owner": "external", "reasons": ["type"], "z": 1}],
	})
	with_id(denies(empty_doc, d), "IC004") == {
		"IC004 [宣言の形] infra_code_decl のキー extra を解釈できません",
		"IC004 [宣言の形] types.resource.aws_s3_bucket のキー x を解釈できません",
		"IC004 [宣言の形] providers.registry.terraform.io/hashicorp/aws のキー y を解釈できません",
		"IC004 [宣言の形] allowlist の項目 a のキー z を解釈できません",
	}
}

test_IC004_deny_allowlist_item_without_id_or_owner if {
	d := object.union(base_decl, {"allowlist": [{"repository": "github.com/o/r", "reasons": ["type"]}]})
	with_id(denies(empty_doc, d), "IC004") == {
		"IC004 [宣言の形] allowlist の 0 番目の項目に id がありません",
		"IC004 [宣言の形] allowlist の 0 番目の項目に owner がありません",
	}
}

test_IC004_deny_missing_declaration if {
	msgs := denies(empty_doc, {})
	"IC004 [宣言の形] infra_code_decl の types がありません" in msgs
}

test_IC004_allow_known_keys if {
	d := object.union(base_decl, {
		"languages": {"python": {"contexts": ["python-test"]}},
		"allowlist": [
			{"id": "m#resource.aws_instance.i", "owner": "repository", "source": "m/src", "language": "python", "adr": "x/y", "reasons": ["type"]},
			{"id": "n#resource.aws_instance.j", "owner": "external", "repository": "github.com/o/r", "reasons": ["type", "provisioner", "provider_exec", "unresolved", "command_block:runner.command"]},
		],
	})
	count(with_id(denies(empty_doc, d), "IC004")) == 0
}

test_IC011_deny_declared_command_block_in_tftest if {
	some msg in with_id(denies(fx("IC011_deny_declared_command_block_in_tftest"), base_decl), "IC011")
	contains(msg, "m/tests/a.tftest.hcl#provider.runner（provider の runner.command）")
}

test_IC011_allow_listed_provider_exec_and_removed_provisioner if {
	d := object.union(base_decl, {"allowlist": [
		{"id": "m#provider.kubernetes", "owner": "external", "repository": "github.com/o/r", "reasons": ["provider_exec"]},
		{"id": "m#removed.aws_s3_bucket.old", "owner": "external", "repository": "github.com/o/r", "reasons": ["provisioner"]},
	]})
	count(denies(fx("IC011_allow_listed_provider_exec_and_removed_provisioner"), d)) == 0
}

test_IC010_deny_when_declaration_is_empty if {
	some msg in denies(fx("IC002_allow_hcl"), {})
	startswith(msg, "IC010 ")
}

test_IC013_deny_required_provider_without_source if {
	msgs := denies(fx("IC013_deny_required_provider_without_source"), base_decl)
	some m in with_id(msgs, "IC013")
	contains(m, "registry.terraform.io/hashicorp/evil")
}

test_IC013_deny_once_when_provider_block_and_required_source_agree if {
	count(with_id(denies(fx("IC013_deny_once_when_provider_block_and_required_source_agree"), base_decl), "IC013")) == 1
}

# fixture_cases は fixtures の txtar のケース名すべて。ケースが欠けるか空なら、それを使うテストが空振りしないよう、ここで落とす。
fixture_cases := {
	"IC001_allow_terraform_files_2",
	"IC001_allow_usecase_tree",
	"IC001_deny_other_extension",
	"IC001_deny_unparsed_extension",
	"IC002_allow_hcl",
	"IC002_deny_json_and_query",
	"IC003_allow_every_known_block_1",
	"IC003_allow_every_known_block_2",
	"IC003_deny_unknown_block_1",
	"IC003_deny_unknown_block_2",
	"IC010_allow_classified_types",
	"IC010_deny_unclassified_data_inside_check",
	"IC010_deny_unclassified_type",
	"IC011_allow_listed_code",
	"IC011_allow_listed_provider_exec_and_removed_provisioner",
	"IC011_allow_removed_block_without_provisioner",
	"IC011_deny_declared_command_block",
	"IC011_deny_declared_command_block_in_tftest",
	"IC011_deny_dynamic_provisioner",
	"IC011_deny_every_must_execute_type",
	"IC011_deny_external_inside_check",
	"IC011_deny_override_file_adding_provisioner",
	"IC011_deny_provider_exec_at_any_depth",
	"IC011_deny_provider_exec_in_attribute_form",
	"IC011_deny_provider_exec_in_tftest",
	"IC011_deny_provider_unresolved_conditional",
	"IC011_deny_provider_unresolved_object_key",
	"IC011_deny_provisioner_in_removed_block",
	"IC011_deny_provisioner_on_any_type",
	"IC011_deny_command_provider_reference",
	"IC011_deny_command_provider_nested_reference",
	"IC011_deny_command_provider_call",
	"IC011_deny_command_provider_template",
	"IC011_allow_reference_in_provider_without_command_blocks",
	"IC011_allow_literal_in_command_provider",
	"IC011_deny_reason_not_listed",
	"IC013_allow_classified_provider_with_required_source",
	"IC013_deny_once_when_provider_block_and_required_source_agree",
	"IC013_deny_required_provider_without_source",
	"IC013_deny_required_source_without_provider_block",
	"IC013_deny_unclassified_provider",
	"IC014_allow_literal_source",
	"IC014_deny_non_literal_required_source_without_provider_block",
	"IC014_deny_non_literal_source",
	"IC020_deny_item_matching_nothing",
	"IC030_allow_static_paths_2",
	"IC030_allow_static_paths_3",
	"IC030_deny_absolute_path",
	"IC030_deny_file_function_followed_by_more_code",
	"IC030_deny_file_function_with_reference",
	"IC030_deny_non_static_inline_code",
	"IC030_deny_non_static_path_2",
	"IC030_deny_non_static_path_3",
	"IC030_deny_non_static_path_4",
	"IC030_deny_non_string_non_list_value_1",
	"IC030_deny_non_string_non_list_value_2",
	"IC030_deny_path_escaping_repository",
	"IC030_deny_path_module_prefix_without_slash",
	"IC031_allow_file_function_with_calls_in_later_args",
	"IC031_allow_path_under_source_1",
	"IC031_allow_path_under_source_2",
	"IC031_allow_path_under_source_3",
	"IC031_allow_path_under_source_4",
	"IC031_allow_path_under_source_5",
	"IC031_allow_path_under_source_6",
	"IC031_allow_path_under_source_7",
	"IC031_allow_source_is_the_file_itself",
	"IC031_deny_every_file_function_outside_source_2",
	"IC031_deny_every_file_function_outside_source_3",
	"IC031_deny_every_file_function_outside_source_4",
	"IC031_deny_every_file_function_outside_source_5",
	"IC031_deny_every_file_function_outside_source_6",
	"IC031_deny_file_function_outside_source",
	"IC031_deny_file_function_with_calls_in_later_args",
	"IC031_deny_path_in_dynamic_block",
	"IC031_deny_path_outside_source_1",
	"IC031_deny_path_outside_source_2",
	"IC031_deny_path_outside_source_3",
	"IC031_two_items_matching_sources",
	"IC031_two_items_two_components",
	"IC032_allow_external_item_without_repository_path",
	"IC032_external_item_repository_path",
	"IC033_deny_inline_code_with_other_source",
	"IC034_deny_list_valued_code",
	"IC034_deny_nested_block_body_arg",
}

test_fixtures_present if {
	every name in fixture_cases {
		count(fx(name).files) > 0
	}
	{k | some k, _ in data.fixtures; not startswith(k, "lib_")} == fixture_cases
}

# provider の設定は block を属性の形でも書ける（helm の kubernetes = { exec = {...} }）。
test_IC011_deny_provider_exec_in_attribute_form if {
	some m in with_id(denies(fx("IC011_deny_provider_exec_in_attribute_form"), base_decl), "IC011")
	contains(m, "m#provider.helm（provider の exec）")
}

# provider の中の、中を読めない式（条件など）と、キーがリテラルでないオブジェクトは、exec を隠せるので検出する。
test_IC011_deny_provider_unresolved_expression if {
	every c in ["IC011_deny_provider_unresolved_conditional", "IC011_deny_provider_unresolved_object_key"] {
		with_id(denies(fx(c), base_decl), "IC011") == {"IC011 [許可リストに無い] m/main.tf: m#provider.helm（解けない式）はインフラ内コードです → allowlist に項目を足し、本リポジトリ所有なら ADR-0103 決定13 の ADR を書く"}
	}
}

test_IC011_allow_listed_provider_unresolved_expression if {
	d := object.union(base_decl, {"allowlist": [{"id": "m#provider.helm", "owner": "external", "repository": "github.com/o/r", "reasons": ["unresolved"]}]})
	every c in ["IC011_deny_provider_unresolved_conditional", "IC011_deny_provider_unresolved_object_key"] {
		count(with_id(denies(fx(c), d), "IC011")) == 0
	}
}

must_execute_decl(v) := object.union(base_decl, {"types": object.union(base_decl.types, {k: {"executes_code": v} | some k in must_execute})})

# ADR-0103 決定9 が名指す型の集合を、規則の must_execute とは別に書いて突き合わせる。
must_execute_expected := {
		"resource.aws_instance",
		"resource.aws_launch_configuration",
		"resource.aws_launch_template",
		"resource.aws_lightsail_instance",
		"resource.aws_spot_fleet_request",
		"resource.aws_spot_instance_request",
		"resource.aws_lambda_invocation",
		"data.aws_lambda_invocation",
		"action.aws_lambda_invoke",
		"data.external",
	}

test_IC012_deny_every_must_execute_type_on_the_wrong_side if {
	must_execute == must_execute_expected
	msgs := with_id(denies(fx("IC002_allow_hcl"), must_execute_decl(false)), "IC012")
	every k in must_execute {
		some m in msgs
		contains(m, concat("", [k, " はコードを実行させる型です"]))
	}
	count(msgs) == count(must_execute)
}

test_IC011_deny_every_must_execute_type if {
	must_execute == must_execute_expected
	msgs := with_id(denies(fx("IC011_deny_every_must_execute_type"), must_execute_decl(true)), "IC011")
	every k in must_execute {
		some m in msgs
		contains(m, sprintf("コードを実行させる型 %s", [k]))
	}
}

command_provider_non_literal := ["IC011_deny_command_provider_reference", "IC011_deny_command_provider_call", "IC011_deny_command_provider_template"]

test_IC011_deny_command_provider_non_literal_value if {
	every c in command_provider_non_literal {
		with_id(denies(fx(c), base_decl), "IC011") == {"IC011 [許可リストに無い] m/providers.tf: m#provider.runner（解けない式）はインフラ内コードです → allowlist に項目を足し、本リポジトリ所有なら ADR-0103 決定13 の ADR を書く"}
	}
}

test_IC011_allow_listed_command_provider_non_literal_value if {
	d := object.union(base_decl, {"allowlist": [{"id": "m#provider.runner", "owner": "external", "repository": "github.com/o/r", "reasons": ["unresolved"]}]})
	every c in command_provider_non_literal {
		count(denies(fx(c), d)) == 0
	}
}

test_IC011_allow_reference_in_provider_without_command_blocks if {
	count(denies(fx("IC011_allow_reference_in_provider_without_command_blocks"), base_decl)) == 0
}

test_IC011_allow_literal_in_command_provider if {
	count(denies(fx("IC011_allow_literal_in_command_provider"), base_decl)) == 0
}

instance_item(reasons) := {"id": "m#resource.aws_instance.i", "owner": "external", "repository": "github.com/o/r", "reasons": reasons}

test_IC011_deny_reason_not_listed if {
	d := object.union(base_decl, {"allowlist": [instance_item(["type"])]})
	denies(fx("IC011_deny_reason_not_listed"), d) == {"IC011 [理由が許可リストに無い] m/main.tf: m#resource.aws_instance.i の項目は、この検出の理由 provisioner（provisioner）を reasons に挙げていません → 意図したコードなら reasons に足し、そうでなければ取り除く"}
}

test_IC011_allow_every_reason_listed if {
	d := object.union(base_decl, {"allowlist": [instance_item(["type", "provisioner"])]})
	count(denies(fx("IC011_deny_reason_not_listed"), d)) == 0
}

test_IC004_deny_malformed_reasons if {
	every rs in [null, [], "type", [1], ["exec"], ["type", "type"], ["command_block:"], ["command_block:Runner.Command"]] {
		item := object.union(object.remove(instance_item([]), ["reasons"]), {"reasons": rs})
		d := object.union(base_decl, {"allowlist": [item]})
		with_id(denies(empty_doc, d), "IC004") == {"IC004 [宣言の形] allowlist の項目 m#resource.aws_instance.i の reasons が、理由の符号（type・provisioner・provider_exec・unresolved・command_block:<block>）の、重複の無い空でない配列ではありません"}
	}
}

test_IC004_deny_missing_reasons if {
	d := object.union(base_decl, {"allowlist": [object.remove(instance_item([]), ["reasons"])]})
	with_id(denies(empty_doc, d), "IC004") == {"IC004 [宣言の形] allowlist の項目 m#resource.aws_instance.i の reasons が、理由の符号（type・provisioner・provider_exec・unresolved・command_block:<block>）の、重複の無い空でない配列ではありません"}
}

test_IC004_deny_command_blocks_not_expressible_as_reason if {
	every bs in [null, "runner.command", [1], [""], ["Runner.Command"], ["runner command"], ["runner..command"], [".runner"], ["runner.command", "runner.command"]] {
		d := json.patch(base_decl, [{"op": "replace", "path": "/providers/registry.terraform.io~1example~1runner/command_blocks", "value": bs}])
		with_id(denies(empty_doc, d), "IC004") == {"IC004 [宣言の形] providers.registry.terraform.io/example/runner の command_blocks が、理由の符号 command_block:<block> に書ける位置（小文字・数字・_ の名前を . でつないだもの）の、重複の無い配列ではありません"}
	}
}

test_IC004_deny_command_blocks_missing if {
	d := json.patch(base_decl, [{"op": "remove", "path": "/providers/registry.terraform.io~1example~1runner/command_blocks"}])
	with_id(denies(empty_doc, d), "IC004") == {"IC004 [宣言の形] providers.registry.terraform.io/example/runner の command_blocks が、理由の符号 command_block:<block> に書ける位置（小文字・数字・_ の名前を . でつないだもの）の、重複の無い配列ではありません"}
}

runner_item(reasons) := {"id": "m#provider.runner", "owner": "external", "repository": "github.com/o/r", "reasons": reasons}

test_IC011_allow_listed_command_block if {
	d := object.union(base_decl, {"allowlist": [runner_item(["command_block:runner.command"])]})
	count(denies(fx("IC011_deny_declared_command_block"), d)) == 0
}

test_IC011_deny_command_block_not_in_reasons if {
	d := object.union(base_decl, {"allowlist": [runner_item(["unresolved"])]})
	with_id(denies(fx("IC011_deny_declared_command_block"), d), "IC011") == {"IC011 [理由が許可リストに無い] m/providers.tf: m#provider.runner の項目は、この検出の理由 command_block:runner.command（provider の runner.command）を reasons に挙げていません → 意図したコードなら reasons に足し、そうでなければ取り除く"}
}

test_IC011_deny_command_provider_nested_non_literal if {
	with_id(denies(fx("IC011_deny_command_provider_nested_reference"), base_decl), "IC011") == {"IC011 [許可リストに無い] m/providers.tf: m#provider.runner（解けない式）はインフラ内コードです → allowlist に項目を足し、本リポジトリ所有なら ADR-0103 決定13 の ADR を書く"}
}
