package plan.lib_test

import data.plan.lib

test_iam_match_literal_equal if lib.iam_match("s3:getobject", "s3:getobject")

test_iam_match_literal_differs if not lib.iam_match("s3:getobject", "s3:getobjects")

test_iam_match_literal_dot_is_not_regex if not lib.iam_match("s3:get.bject", "s3:getobject")

test_iam_match_literal_bracket if lib.iam_match("arn:aws:s3:::b[1]", "arn:aws:s3:::b[1]")

test_iam_match_star_crosses_separators if lib.iam_match("arn:aws:s3:::b/*", "arn:aws:s3:::b/x/y:z")

test_iam_match_question_is_one_character if {
	lib.iam_match("s3:get?bject", "s3:getobject")
	not lib.iam_match("s3:get?bject", "s3:getbject")
}

test_iam_match_wildcard_with_regex_characters if {
	lib.iam_match("arn:aws:s3:::b.c/*", "arn:aws:s3:::b.c/k")
	not lib.iam_match("arn:aws:s3:::b.c/*", "arn:aws:s3:::bxc/k")
}

test_derived_merges_parent_arguments_across_attachments if {
	d := {
		"attachments": {
			"a": {"parent_arguments": {"aws_s3_bucket": {"policy": null}}},
			"b": {"parent_arguments": {"aws_s3_bucket": {"website": []}, "aws_iam_role": {"inline_policy": []}}},
		},
		"unrelated_deprecated_arguments": {},
		"inline_owned": {},
	}
	got := lib.derived_inline_arguments with data.layer_decl as d with data.policy_plan.stateful_protection.protected_types as ["aws_s3_bucket"]
	got == {"aws_s3_bucket": {"policy": null, "website": []}}
}
