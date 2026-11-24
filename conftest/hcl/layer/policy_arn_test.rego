package layer

# LY309（ADR-0301 決定21）。参照の辿り方は LY301 と同じ leaves を使う（attachment_test.rego が固定する）。

ly309(name) := with_id(denies(fx(name)), "LY309")

test_LY309_deny_literal_customer_managed if {
	only_with(ly309("LY309_deny_literal_customer_managed"), ": 参照がありません（ADR-0301 決定21）")
}

test_LY309_deny_variable if {
	only_with(ly309("LY309_deny_variable"), ": var.policy_arn（ADR-0301 決定21）")
}

test_LY309_deny_module_reference if {
	only_with(ly309("LY309_deny_module_reference"), ": module.p（ADR-0301 決定21）")
}

test_LY309_deny_data_source if {
	only_with(ly309("LY309_deny_data_source"), ": data.aws_iam_policy（ADR-0301 決定21）")
}

test_LY309_deny_local_resolving_to_literal if {
	only_with(ly309("LY309_deny_local_resolving_to_literal"), ": 参照がありません（ADR-0301 決定21）")
}

test_LY309_deny_policy_in_other_module if {
	only_with(ly309("LY309_deny_policy_in_other_module"), ": aws_iam_policy.p（ADR-0301 決定21）")
}

test_LY309_deny_template_customer_managed if {
	only_with(ly309("LY309_deny_template_customer_managed"), ": data.aws_caller_identity, data.aws_partition（ADR-0301 決定21）")
}

test_LY309_deny_template_partition_from_variable if {
	only_with(ly309("LY309_deny_template_partition_from_variable"), ": var.partition（ADR-0301 決定21）")
}

# partition のほかを補間する ARN は AWS managed と読まない。
test_LY309_deny_template_name_interpolated if {
	only_with(ly309("LY309_deny_template_name_interpolated"), ": data.aws_partition, var.name（ADR-0301 決定21）")
}

# 参照の1つでも aws_iam_policy でなければ落とす。
test_LY309_deny_policy_and_variable if {
	only_with(ly309("LY309_deny_policy_and_variable"), ": aws_iam_policy.p, var.arn（ADR-0301 決定21）")
}

test_LY309_deny_wrong_resource_type if {
	only_with(ly309("LY309_deny_wrong_resource_type"), ": aws_iam_role.other（ADR-0301 決定21）")
}

test_LY309_allow_direct_reference if {
	count(ly309("LY309_allow_direct_reference")) == 0
}

test_LY309_allow_reference_through_local if {
	count(ly309("LY309_allow_reference_through_local")) == 0
}

test_LY309_allow_indexed_reference if {
	count(ly309("LY309_allow_indexed_reference")) == 0
}

test_LY309_allow_reference_through_each if {
	count(ly309("LY309_allow_reference_through_each")) == 0
}

test_LY309_allow_aws_managed_literal if {
	count(ly309("LY309_allow_aws_managed_literal")) == 0
}

test_LY309_allow_aws_managed_literal_other_partition if {
	count(ly309("LY309_allow_aws_managed_literal_other_partition")) == 0
}

test_LY309_allow_aws_managed_partition_template if {
	count(ly309("LY309_allow_aws_managed_partition_template")) == 0
}

test_LY309_allow_aws_managed_partition_through_local if {
	count(ly309("LY309_allow_aws_managed_partition_through_local")) == 0
}

# policy_arn は Provider の schema で必須なので、書かない resource は terraform validate が落とす。
test_LY309_allow_without_policy_arn if {
	count(ly309("LY309_allow_without_policy_arn")) == 0
}

# 決定21 は role への付与だけを定める。
test_LY309_allow_other_attachment_type if {
	count(ly309("LY309_allow_other_attachment_type")) == 0
}
