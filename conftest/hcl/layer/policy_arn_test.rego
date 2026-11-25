package layer

# LY309（ADR-0301 決定21）。参照の辿り方は LY301 と同じ leaves を使う（attachment_test.rego が固定する）。

ly309(name) := with_id(denies(fx(name)), "LY309")

ly309_msg(detail) := concat("", [sprintf("LY309 [policy の参照] modules/m/main.tf の aws_iam_role_policy_attachment.a の policy_arn が、同じ module の aws_iam_policy の参照と AWS managed policy の ARN だけに決まりません: %s（ADR-0301 決定21）→ 同じ module に aws_iam_policy を置いて参照するか、AWS managed policy の ARN をリテラルか partition だけ data.aws_partition から組むテンプレートで書く", [detail]), ly309_hint(detail)])

ly309_hint(detail) := "。関数の呼び出しや条件式は値を決められないので、policy を aws_iam_policy.x[0].arn のように添字で参照する" if contains(detail, "値を決められない式")

default ly309_hint(_) := ""

ly309_denied(name, detail) if ly309(name) == {ly309_msg(detail)}

ly309_allowed(name) if {
	msgs := denies(fx(name))
	count(with_id(msgs, "LY309")) == 0
	count(with_id(msgs, "LY004")) == 0
}

test_LY309_deny_literal_customer_managed if {
	ly309_denied("LY309_deny_literal_customer_managed", `リテラル "arn:aws:iam::123456789012:policy/x"`)
}

test_LY309_deny_variable if {
	ly309_denied("LY309_deny_variable", "参照 var.policy_arn")
}

test_LY309_deny_module_reference if {
	ly309_denied("LY309_deny_module_reference", "参照 module.p")
}

test_LY309_deny_data_source if {
	ly309_denied("LY309_deny_data_source", "参照 data.aws_iam_policy")
}

test_LY309_deny_local_resolving_to_literal if {
	ly309_denied("LY309_deny_local_resolving_to_literal", `リテラル "arn:aws:iam::123456789012:policy/x"`)
}

test_LY309_deny_policy_in_other_module if {
	ly309_denied("LY309_deny_policy_in_other_module", "参照 aws_iam_policy.p")
}

test_LY309_deny_template_customer_managed if {
	ly309_denied("LY309_deny_template_customer_managed", `参照 data.aws_caller_identity, data.aws_partition / リテラル ":iam::", ":policy/x", "arn:"`)
}

test_LY309_deny_template_partition_account_literal if {
	ly309_denied("LY309_deny_template_partition_account_literal", `参照 data.aws_partition / リテラル ":iam::123456789012:policy/x", "arn:"`)
}

test_LY309_deny_template_without_arn_prefix if {
	ly309_denied("LY309_deny_template_without_arn_prefix", `参照 data.aws_partition / リテラル ":iam::aws:policy/ReadOnlyAccess", "xarn:"`)
}

test_LY309_deny_template_partition_from_variable if {
	ly309_denied("LY309_deny_template_partition_from_variable", `参照 var.partition / リテラル ":iam::aws:policy/AdministratorAccess", "arn:"`)
}

test_LY309_deny_partition_local_from_variable if {
	ly309_denied("LY309_deny_partition_local_from_variable", `参照 var.partition / リテラル ":iam::aws:policy/ReadOnlyAccess", "arn:"`)
}

test_LY309_deny_partition_other_attribute if {
	ly309_denied("LY309_deny_partition_other_attribute", `参照 data.aws_partition / リテラル ":iam::aws:policy/ReadOnlyAccess", "arn:"`)
}

test_LY309_deny_partition_alias_cycle if {
	ly309_denied("LY309_deny_partition_alias_cycle", `リテラル ":iam::aws:policy/ReadOnlyAccess", "arn:"`)
}

test_LY309_deny_partition_undefined_local if {
	ly309_denied("LY309_deny_partition_undefined_local", `参照 local.missing / リテラル ":iam::aws:policy/ReadOnlyAccess", "arn:"`)
}

test_LY309_deny_template_name_interpolated if {
	ly309_denied("LY309_deny_template_name_interpolated", `参照 data.aws_partition, var.name / リテラル ":iam::aws:policy/", "arn:"`)
}

test_LY309_deny_template_suffix_on_reference if {
	ly309_denied("LY309_deny_template_suffix_on_reference", `リテラル "-v2"`)
}

test_LY309_deny_policy_and_variable if {
	ly309_denied("LY309_deny_policy_and_variable", "参照 var.arn / 値を決められない式 coalesce()")
}

test_LY309_deny_conditional_with_customer_literal if {
	ly309_denied("LY309_deny_conditional_with_customer_literal", "参照 var.on / 値を決められない式 conditional")
}

test_LY309_deny_local_conditional if {
	ly309_denied("LY309_deny_local_conditional", "参照 var.on / 値を決められない式 conditional")
}

test_LY309_deny_for_each_mixed_literal if {
	ly309_denied("LY309_deny_for_each_mixed_literal", `リテラル "arn:aws:iam::123456789012:policy/x"`)
}

test_LY309_deny_each_label_literal if {
	ly309_denied("LY309_deny_each_label_literal", `リテラル "arn:aws:iam::123456789012:policy/x"`)
}

test_LY309_deny_each_from_call if {
	ly309_denied("LY309_deny_each_from_call", "値を決められない式 toset()")
}

test_LY309_deny_empty_tuple if {
	ly309_denied("LY309_deny_empty_tuple", "参照もリテラルもありません")
}

test_LY309_deny_wrong_resource_type if {
	ly309_denied("LY309_deny_wrong_resource_type", "参照 aws_iam_role.other")
}

# policy_arn を書かない resource は、何を付けるかを判定できないので落とす。
test_LY309_deny_without_policy_arn if {
	ly309("LY309_deny_without_policy_arn") == {"LY309 [policy の参照] modules/m/main.tf の aws_iam_role_policy_attachment.a が policy_arn を書いていません（ADR-0301 決定21）→ policy_arn に、同じ module の aws_iam_policy の参照か AWS managed policy の ARN を書く"}
}

test_LY309_allow_direct_reference if {
	ly309_allowed("LY309_allow_direct_reference")
}

test_LY309_allow_reference_through_local if {
	ly309_allowed("LY309_allow_reference_through_local")
}

test_LY309_allow_indexed_reference if {
	ly309_allowed("LY309_allow_indexed_reference")
}

test_LY309_allow_reference_through_each if {
	ly309_allowed("LY309_allow_reference_through_each")
}

test_LY309_allow_each_value_with_literal_key if {
	ly309_allowed("LY309_allow_each_value_with_literal_key")
}

test_LY309_allow_aws_managed_literal if {
	ly309_allowed("LY309_allow_aws_managed_literal")
}

test_LY309_allow_aws_managed_literal_other_partition if {
	ly309_allowed("LY309_allow_aws_managed_literal_other_partition")
}

test_LY309_allow_aws_managed_literal_through_local if {
	ly309_allowed("LY309_allow_aws_managed_literal_through_local")
}

test_LY309_allow_aws_managed_partition_template if {
	ly309_allowed("LY309_allow_aws_managed_partition_template")
}

test_LY309_allow_aws_managed_partition_through_local if {
	ly309_allowed("LY309_allow_aws_managed_partition_through_local")
}

test_LY309_allow_aws_managed_template_through_local if {
	ly309_allowed("LY309_allow_aws_managed_template_through_local")
}

test_LY309_allow_other_attachment_type if {
	ly309_allowed("LY309_allow_other_attachment_type")
}
