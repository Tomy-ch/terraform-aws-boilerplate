package infra_code

lambda_decl(items) := object.union(base_decl, {
	"types": object.union(base_decl.types, {
		"resource.aws_lambda_function": {"executes_code": true, "path_args": ["filename"]},
		"resource.aws_cloudfront_function": {"executes_code": true, "body_args": ["code"]},
		"data.external": {"executes_code": true, "path_args": ["program"]},
	}),
	"allowlist": items,
})

repo_item(id, source) := {"id": id, "owner": "repository", "source": source, "language": "python", "adr": "x/fn", "reasons": ["type"]}

fn_id := "u/internal/fn#resource.aws_lambda_function.f"

test_IC020_deny_item_matching_nothing if {
	d := lambda_decl([repo_item("u/internal/fn#resource.aws_lambda_function.gone", "u/internal/fn/src")])
	msgs := denies(fx("IC020_deny_item_matching_nothing"), d)
	count(with_id(msgs, "IC020")) == 1
}

test_IC020_deny_item_matching_code_free_component if {
	d := lambda_decl([{"id": "m#resource.aws_s3_bucket.b", "owner": "external", "repository": "github.com/o/r", "reasons": ["type"]}])
	msgs := denies(fx("IC002_allow_hcl"), d)
	count(with_id(msgs, "IC020")) == 1
}

# 項目が挙げた理由のうち、その構成要素の検出に当たらないものは落とす。
test_IC020_deny_reason_matching_nothing if {
	item := object.union(repo_item(fn_id, "u/internal/fn/src"), {"reasons": ["type", "provisioner"]})
	with_id(denies(fx("IC020_deny_item_matching_nothing"), lambda_decl([item])), "IC020") == {"IC020 [当たらない理由] 許可リストの項目 u/internal/fn#resource.aws_lambda_function.f の理由 provisioner は、その構成要素のどの検出にも当たりません → reasons から外す"}
}

test_IC020_allow_item_matching_code if {
	d := lambda_decl([repo_item(fn_id, "u/internal/fn/src")])
	count(denies(fx("IC020_deny_item_matching_nothing"), d)) == 0
}

test_IC030_deny_non_static_path if {
	d := lambda_decl([repo_item(fn_id, "u/internal/fn/src")])
	every c in ["IC032_allow_external_item_without_repository_path", "IC030_deny_non_static_path_2", "IC030_deny_non_static_path_3", "IC030_deny_non_static_path_4"] {
		count(with_id(denies(fx(c), d), "IC030")) == 1
	}
}

test_IC030_deny_absolute_path if {
	d := lambda_decl([repo_item(fn_id, "u/internal/fn/src")])
	count(with_id(denies(fx("IC030_deny_absolute_path"), d), "IC030")) == 1
}

test_IC030_deny_path_escaping_repository if {
	d := lambda_decl([repo_item(fn_id, "u/internal/fn/src")])
	count(with_id(denies(fx("IC030_deny_path_escaping_repository"), d), "IC030")) == 1
}

test_IC030_deny_non_static_inline_code if {
	d := lambda_decl([repo_item("u/internal/fn#resource.aws_cloudfront_function.c", "u/internal/fn/main.tf")])
	count(with_id(denies(fx("IC030_deny_non_static_inline_code"), d), "IC030")) == 1
}

test_IC030_allow_static_paths if {
	d := lambda_decl([repo_item(fn_id, "u/internal/fn/src")])
	every c in ["IC020_deny_item_matching_nothing", "IC030_allow_static_paths_2", "IC030_allow_static_paths_3"] {
		count(denies(fx(c), d)) == 0
	}
}

test_IC031_deny_path_outside_source if {
	d := lambda_decl([repo_item(fn_id, "u/internal/fn/src")])
	every c in ["IC031_deny_path_outside_source_1", "IC031_deny_path_outside_source_2", "IC031_deny_path_outside_source_3"] {
		count(with_id(denies(fx(c), d), "IC031")) == 1
	}
}

test_IC031_deny_file_function_outside_source if {
	d := lambda_decl([repo_item("u/internal/fn#resource.aws_cloudfront_function.c", "u/internal/fn/src")])
	count(with_id(denies(fx("IC031_deny_file_function_outside_source"), d), "IC031")) == 1
}

test_IC031_allow_path_under_source if {
	d := lambda_decl([repo_item(fn_id, "u/internal/fn/src")])
	count(denies(fx("IC031_allow_path_under_source_1"), d)) == 0
	d2 := lambda_decl([repo_item("u/internal/fn#resource.aws_cloudfront_function.c", "u/internal/fn/src")])
	every n in numbers.range(2, 7) {
		count(denies(fx(sprintf("IC031_allow_path_under_source_%d", [n])), d2)) == 0
	}
}

test_IC032_deny_external_item_referencing_repository_path if {
	d := lambda_decl([{"id": fn_id, "owner": "external", "repository": "github.com/o/r", "reasons": ["type"]}])
	count(with_id(denies(fx("IC020_deny_item_matching_nothing"), d), "IC032")) == 1
}

test_IC032_allow_external_item_without_repository_path if {
	d := lambda_decl([{"id": fn_id, "owner": "external", "repository": "github.com/o/r", "reasons": ["type"]}])
	msgs := denies(fx("IC032_allow_external_item_without_repository_path"), d)
	count(with_id(msgs, "IC030")) == 1
	count(with_id(msgs, "IC032")) == 0
}

test_IC033_deny_inline_code_with_other_source if {
	d := lambda_decl([repo_item("u/internal/fn#resource.aws_cloudfront_function.c", "u/internal/fn/src")])
	count(with_id(denies(fx("IC033_deny_inline_code_with_other_source"), d), "IC033")) == 1
}

test_IC033_allow_inline_code_with_declaring_file_as_source if {
	d := lambda_decl([repo_item("u/internal/fn#resource.aws_cloudfront_function.c", "u/internal/fn/main.tf")])
	count(denies(fx("IC033_deny_inline_code_with_other_source"), d)) == 0
}

test_IC034_deny_list_valued_code if {
	d := lambda_decl([repo_item("u/internal/fn#data.external.e", "u/internal/fn/src")])
	denies(fx("IC034_deny_list_valued_code"), d) == {"IC034 [path] u/internal/fn/main.tf: u/internal/fn#data.external.e の program はリストなので path を検査できず、許可リストに載せられません"}
}

test_IC034_allow_string_valued_path if {
	d := lambda_decl([repo_item(fn_id, "u/internal/fn/src")])
	count(with_id(denies(fx("IC020_deny_item_matching_nothing"), d), "IC034")) == 0
}

test_IC031_deny_every_file_function_outside_source if {
	d := lambda_decl([repo_item("u/internal/fn#resource.aws_cloudfront_function.c", "u/internal/fn/src")])
	every c in array.concat(["IC031_deny_file_function_outside_source"], [sprintf("IC031_deny_every_file_function_outside_source_%d", [n]) | some n in numbers.range(2, 6)]) {
		count(with_id(denies(fx(c), d), "IC031")) == 1
	}
}

test_IC030_deny_non_string_non_list_value if {
	d := lambda_decl([repo_item("u/internal/fn#resource.aws_cloudfront_function.c", "u/internal/fn/main.tf")])
	every c in ["IC030_deny_non_string_non_list_value_1", "IC030_deny_non_string_non_list_value_2"] {
		count(with_id(denies(fx(c), d), "IC030")) == 1
	}
}

test_IC030_deny_file_function_followed_by_more_code if {
	d := lambda_decl([repo_item("u/internal/fn#resource.aws_cloudfront_function.c", "u/internal/fn/src")])
	count(with_id(denies(fx("IC030_deny_file_function_followed_by_more_code"), d), "IC030")) == 1
}

test_IC031_deny_path_in_dynamic_block if {
	d := object.union(lambda_decl([repo_item("u/internal/fn#resource.aws_spot_fleet_request.s", "u/internal/fn/src")]), {"types": object.union(lambda_decl([]).types, {"resource.aws_spot_fleet_request": {"executes_code": true, "body_args": ["launch_specification.user_data"]}})})
	count(with_id(denies(fx("IC031_deny_path_in_dynamic_block"), d), "IC031")) == 1
}

# 第1引数が静的な path なら、後の引数（呼び出しや `)` を含む文字列）に依らず path として判定する。
test_IC031_allow_file_function_with_calls_in_later_args if {
	d := lambda_decl([repo_item("u/internal/fn#resource.aws_cloudfront_function.c", "u/internal/fn/src")])
	count(denies(fx("IC031_allow_file_function_with_calls_in_later_args"), d)) == 0
}

test_IC031_deny_file_function_with_calls_in_later_args if {
	d := lambda_decl([repo_item("u/internal/fn#resource.aws_cloudfront_function.c", "u/internal/fn/src")])
	denies(fx("IC031_deny_file_function_with_calls_in_later_args"), d) == {"IC031 [path] u/internal/fn/main.tf: u/internal/fn#resource.aws_cloudfront_function.c の code が指す u/internal/fn/other/fn.js は、ソースの所在 u/internal/fn/src の下にありません"}
}

# path.module の直後が `/` でなければ、module の外を指しうるので静的な path として扱わない。
test_IC030_deny_path_module_prefix_without_slash if {
	d := lambda_decl([repo_item("u/internal/fn#resource.aws_cloudfront_function.c", "u/internal/fn/src")])
	some m in with_id(denies(fx("IC030_deny_path_module_prefix_without_slash"), d), "IC030")
	contains(m, "静的に確定するリポジトリ内のパスではありません")
}

test_IC031_allow_source_is_the_file_itself if {
	d := lambda_decl([repo_item("u/internal/fn#resource.aws_cloudfront_function.c", "u/internal/fn/src/fn.js")])
	count(denies(fx("IC031_allow_source_is_the_file_itself"), d)) == 0
}

# 入力の形が崩れていれば、許可リストの項目が当たらないことも判定しない。
test_IC005_deny_malformed_input_without_judging_allowlist if {
	d := lambda_decl([repo_item("u/internal/fn#resource.aws_cloudfront_function.c", "u/internal/fn/src")])
	msgs := deny with input as {"files": [], "blocks": null}
		with data.infra_code_decl as d
		with data.hcl_parsed_files as []
		with data.hcl_block_counts as {}
	count(with_id(msgs, "IC005")) == 1
	count(with_id(msgs, "IC020")) == 0
	msgs == with_id(msgs, "IC005")
}

# 許可リストの項目は id の一致する構成要素だけに当たる。
test_IC031_deny_only_the_matching_component if {
	d := lambda_decl([
		repo_item("u/internal/fn#resource.aws_lambda_function.f", "u/internal/fn/src1"),
		repo_item("u/internal/fn#resource.aws_lambda_function.g", "u/internal/fn/src1"),
	])
	with_id(denies(fx("IC031_two_items_two_components"), d), "IC031") == {"IC031 [path] u/internal/fn/main.tf: u/internal/fn#resource.aws_lambda_function.g の filename が指す u/internal/fn/src2/fn.zip は、ソースの所在 u/internal/fn/src1 の下にありません"}
}

test_IC032_deny_external_item_pointing_into_repository_exactly if {
	d := lambda_decl([{"id": fn_id, "owner": "external", "repository": "github.com/o/r", "reasons": ["type"]}])
	denies(fx("IC032_external_item_repository_path"), d) == {"IC032 [path] u/internal/fn/main.tf: 外部所有の項目 u/internal/fn#resource.aws_lambda_function.f の filename がリポジトリ内のパスを指しています"}
}

# 第1引数が参照の file() は、path として静的に確定しない。
test_IC030_deny_file_function_with_reference if {
	d := lambda_decl([repo_item("u/internal/fn#resource.aws_cloudfront_function.c", "u/internal/fn/src")])
	some m in with_id(denies(fx("IC030_deny_file_function_with_reference"), d), "IC030")
	contains(m, "パスでもインラインのコードでもありません（2 行目）")
}

# body_args が入れ子の block を指すとき、値はリストとして扱う。
test_IC034_deny_nested_block_body_arg if {
	d := object.union(lambda_decl([repo_item("u/internal/fn#resource.aws_spot_fleet_request.s", "u/internal/fn/src")]), {"types": object.union(lambda_decl([]).types, {"resource.aws_spot_fleet_request": {"executes_code": true, "body_args": ["launch_specification"]}})})
	some m in with_id(denies(fx("IC034_deny_nested_block_body_arg"), d), "IC034")
	contains(m, "launch_specification はリスト")
}

# 項目ごとの所在は、id の一致する構成要素にだけ当てる。取り違えれば f の path が g の所在の外になる。
test_IC031_allow_each_item_against_its_own_component if {
	d := lambda_decl([
		repo_item("u/internal/fn#resource.aws_lambda_function.f", "u/internal/fn/src1"),
		repo_item("u/internal/fn#resource.aws_lambda_function.g", "u/internal/fn/src2"),
	])
	count(with_id(denies(fx("IC031_two_items_matching_sources"), d), "IC031")) == 0
}
