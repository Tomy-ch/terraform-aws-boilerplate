package layer

# LY401（ADR-0102 決定16）と、その宣言 singletons の形（LY001）。

ly401(name) := with_id(denies(fx(name)), "LY401")

ly401_msg(path) := sprintf("LY401 [単一の型の持ち主] %s の aws_s3_account_public_access_block.this は、アカウントかリージョンに1つしか置けない型で、持ち主のユースケース（account-baseline）の外で宣言されています", [path])

test_LY401_deny_env_root if {
	only_with(ly401("LY401_deny_env_root"), ly401_msg("env/dev/main.tf"))
}

test_LY401_deny_general_module if {
	only_with(ly401("LY401_deny_general_module"), ly401_msg("modules/m/main.tf"))
}

test_LY401_deny_other_usecase if {
	only_with(ly401("LY401_deny_other_usecase"), ly401_msg("usecases/finops/internal/x/main.tf"))
}

# 持ち主の名前を接頭辞に持つ別のユースケースは、持ち主ではない。
test_LY401_deny_usecase_name_prefix if {
	only_with(ly401("LY401_deny_usecase_name_prefix"), ly401_msg("usecases/account-baseline-extra/internal/x/main.tf"))
}

test_LY401_allow_owner_internal if {
	count(ly401("LY401_allow_owner_internal")) == 0
}

two_owners := object.union(fixture_decl, {"singletons": {"aws_s3_account_public_access_block": ["account-baseline", "organization-baseline"]}})

test_LY401_allow_second_owner if {
	doc := fx("LY401_allow_second_owner")
	count(with_id(denies_with(doc, listed_of(doc), two_owners), "LY401")) == 0
}

test_LY401_deny_without_second_owner if {
	msgs := ly401("LY401_allow_second_owner")
	only_with(msgs, "usecases/organization-baseline/internal/y/main.tf")
}

# 読むだけの data source は、設定を書き換えない。
test_LY401_allow_data_source_outside_owner if {
	count(ly401("LY401_allow_data_source_outside_owner")) == 0
}

ly001_with(d) := with_id(denies_with(fx("ok_module"), listed_of(fx("ok_module")), d), "LY001")

test_LY001_deny_singletons_missing if {
	only_with(ly001_with(object.remove(fixture_decl, ["singletons"])), "data.layer_decl.singletons がオブジェクトではありません")
}

test_LY001_deny_singletons_empty if {
	only_with(ly001_with(json.patch(fixture_decl, [{"op": "replace", "path": "/singletons", "value": {}}])), "data.layer_decl.singletons が空です")
}

test_LY001_deny_singletons_array if {
	only_with(ly001_with(object.union(fixture_decl, {"singletons": ["aws_s3_account_public_access_block"]})), "data.layer_decl.singletons がオブジェクトではありません")
}

test_LY001_deny_malformed_owners if {
	every owners in [[], "account-baseline", [1], [""], ["account-baseline", "account-baseline"], ["usecases/account-baseline"], ["Account-Baseline"]] {
		d := json.patch(fixture_decl, [{"op": "replace", "path": "/singletons/aws_s3_account_public_access_block", "value": owners}])
		only_with(ly001_with(d), "singletons.aws_s3_account_public_access_block が、持ち主のユースケースの名前")
	}
}

test_LY001_deny_singleton_not_classified_as_not_attachment if {
	d := object.union(fixture_decl, {"singletons": {"aws_s3_account_public_access_block": ["account-baseline"], "aws_ebs_encryption_by_default": ["account-baseline"]}})
	only_with(ly001_with(d), "singletons.aws_ebs_encryption_by_default は not_attachments にありません")
}

test_LY001_allow_singletons if {
	count(ly001_with(fixture_decl)) == 0
}
