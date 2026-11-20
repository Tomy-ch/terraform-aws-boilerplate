package infra_code


test_IC001_deny_other_extension if {
	some m in with_id(denies(fx("IC001_deny_other_extension"), base_decl), "IC001")
	contains(m, "m/terraform.tfvars")
}

test_IC001_deny_unparsed_extension if {
	with_id(denies(fx("IC001_deny_unparsed_extension"), base_decl), "IC001") == {"IC001 [対象外の入力] m/src/handler.py は Terraform の構成ファイルではありません → 走査の root から外すか、conftest/hcl/infra_code/input.rego の除外に足す"}
}

test_IC001_allow_terraform_files if {
	count(with_id(denies(fx("IC003_allow_every_known_block_1"), base_decl), "IC001")) == 0
	count(with_id(denies(fx("IC001_allow_terraform_files_2"), base_decl), "IC001")) == 0
}

# 除外の宣言（.md と .terraform.lock.hcl）に当たるファイルは、usecase の木に在っても IC001 にならない。
test_IC001_allow_usecase_tree if {
	fx("IC001_allow_usecase_tree").files == [
		{"path": "usecases/u/README.md", "parsed": false},
		{"path": "usecases/u/docs/adr/0001-x.md", "parsed": false},
		{"path": "usecases/u/main.tf", "parsed": true},
		{"path": "usecases/u/reference/.terraform.lock.hcl", "parsed": false},
		{"path": "usecases/u/reference/main.tf", "parsed": true},
		{"path": "usecases/u/tests/a.tftest.hcl", "parsed": true},
	]
	count(with_id(denies(fx("IC001_allow_usecase_tree"), base_decl), "IC001")) == 0
}

test_IC002_deny_json_and_query if {
	{m | some m in with_id(denies(fx("IC002_deny_json_and_query"), base_decl), "IC002")} == {
		"IC002 [未対応の入力] m/x.tf.json は検出が解釈しない形式です → HCL（.tf / .tftest.hcl）で書く",
		"IC002 [未対応の入力] m/x.tftest.json は検出が解釈しない形式です → HCL（.tf / .tftest.hcl）で書く",
		"IC002 [未対応の入力] m/x.tfquery.hcl は検出が解釈しない形式です → HCL（.tf / .tftest.hcl）で書く",
	}
}

test_IC002_allow_hcl if {
	count(with_id(denies(fx("IC002_allow_hcl"), base_decl), "IC002")) == 0
}

test_IC003_deny_unknown_block if {
	some m in with_id(denies(fx("IC003_deny_unknown_block_1"), base_decl), "IC003")
	contains(m, "future_block")
	some m2 in with_id(denies(fx("IC003_deny_unknown_block_2"), base_decl), "IC003")
	contains(m2, "m/x.tftest.hcl のブロック resource")
}

test_IC003_allow_every_known_block if {
	count(with_id(denies(fx("IC003_allow_every_known_block_1"), base_decl), "IC003")) == 0
	count(with_id(denies(fx("IC003_allow_every_known_block_2"), base_decl), "IC003")) == 0
}

# --- IC005（入力の形と独立の数） ---

denies_with(doc, parsed, counts) := result if {
	result := deny with input as doc
		with data.infra_code_decl as base_decl
		with data.hcl_parsed_files as parsed
		with data.hcl_block_counts as counts
}

test_IC005_allow_matching_input if {
	doc := fx("IC002_allow_hcl")
	count(with_id(denies_with(doc, parsed_of(doc), counts_of(doc)), "IC005")) == 0
}

test_IC005_deny_malformed_input_and_skip_other_rules if {
	msgs := denies_with([], [], {})
	count(with_id(msgs, "IC005")) == 1
	msgs == with_id(msgs, "IC005")
	msgs2 := denies_with({"files": [{"path": "m/x.tfvars", "parsed": false}], "blocks": [{"file": "m/a.tf"}]}, [], {})
	some m in with_id(msgs2, "IC005")
	contains(m, "blocks[0] の形が違います")
	count(with_id(msgs2, "IC001")) == 0
}

test_IC005_deny_count_mismatch if {
	doc := fx("IC002_allow_hcl")
	msgs := denies_with(doc, [], counts_of(doc))
	some m in with_id(msgs, "IC005")
	contains(m, "find の一覧にありません")
	msgs2 := denies_with(doc, parsed_of(doc), {})
	some m2 in with_id(msgs2, "IC005")
	contains(m2, "行頭の数え")
}

test_IC005_deny_missing_independent_counts if {
	doc := fx("IC002_allow_hcl")
	msgs := deny with input as doc with data.infra_code_decl as base_decl
	with_id(msgs, "IC005") == {
		"IC005 [入力の形] hcl_parsed_files（find の一覧）が配列ではありません → make infra-code-gate から起動する。hcl-input の出力と conftest/hcl/lib/hcl_input.rego の読む形を揃える",
		"IC005 [入力の形] hcl_block_counts（行頭の数え）がオブジェクトではありません → make infra-code-gate から起動する。hcl-input の出力と conftest/hcl/lib/hcl_input.rego の読む形を揃える",
	}
}
