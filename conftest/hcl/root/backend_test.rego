package root

# --- RT201 ---

test_RT201_allow_one_s3_backend_per_root if {
	count(with_id(denies(ok_tree), "RT201")) == 0
}

test_RT201_deny_backend_missing if {
	some m in with_id(denies(fx("RT201_deny_backend_missing")), "RT201")
	contains(m, "root env/mem の backend が 0 件です")
}

test_RT201_deny_two_backends if {
	msgs := with_id(denies(fx("RT201_deny_two_backends")), "RT201")
	some m in msgs
	contains(m, "root env/mem の backend が 2 件です")
}

test_RT201_deny_backend_outside_backend_tf if {
	some m in with_id(denies(fx("RT201_deny_backend_outside_backend_tf")), "RT201")
	contains(m, "env/mem/versions.tf が backend を持ちます")
}

test_RT201_deny_non_s3_backend if {
	some m in with_id(denies(fx("RT201_deny_non_s3_backend")), "RT201")
	contains(m, "backend が local です")
}

test_RT201_deny_cloud_block if {
	some m in with_id(denies(fx("RT201_deny_cloud_block")), "RT201")
	contains(m, "cloud block")
}

# --- RT202 ---

test_RT202_allow_expected_keys if {
	count(with_id(denies(ok_tree), "RT202")) == 0
}

test_RT202_deny_wrong_state_path if {
	some m in with_id(denies(fx("RT202_deny_wrong_state_path")), "RT202")
	contains(m, "env/mem/backend.tf の key が workload/terraform.tfstate")
}

test_RT202_deny_state_path_null if {
	some m in with_id(denies(fx("RT202_deny_state_path_null")), "RT202")
	contains(m, "env/mem/identity/backend.tf")
}

test_RT202_deny_state_path_expression if {
	some m in with_id(denies(fx("RT202_deny_state_path_expression")), "RT202")
	contains(m, "env/mem/backend.tf")
}

# --- RT203 ---

test_RT203_allow_lockfile_true if {
	count(with_id(denies(ok_tree), "RT203")) == 0
}

test_RT203_deny_lockfile_missing if {
	some m in with_id(denies(fx("RT203_deny_lockfile_missing")), "RT203")
	contains(m, "env/mem/backend.tf")
}

test_RT203_deny_lockfile_false if {
	some m in with_id(denies(fx("RT203_deny_lockfile_false")), "RT203")
	contains(m, "env/mem/backend.tf")
}

test_RT203_deny_lockfile_string if {
	some m in with_id(denies(fx("RT203_deny_lockfile_string")), "RT203")
	contains(m, "env/mem/backend.tf")
}

# --- RT204 ---

test_RT204_allow_encrypt_true if {
	count(with_id(denies(ok_tree), "RT204")) == 0
}

test_RT204_deny_encrypt_false if {
	some m in with_id(denies(fx("RT204_deny_encrypt_false")), "RT204")
	contains(m, "env/mem/backend.tf")
}

test_RT204_deny_encrypt_expression if {
	some m in with_id(denies(fx("RT204_deny_encrypt_expression")), "RT204")
	contains(m, "env/mem/backend.tf")
}

# --- RT205 ---

test_RT205_allow_no_dynamodb_table if {
	count(with_id(denies(ok_tree), "RT205")) == 0
}

test_RT205_deny_dynamodb_table if {
	some m in with_id(denies(fx("RT205_deny_dynamodb_table")), "RT205")
	contains(m, "env/mem/backend.tf")
}

test_RT205_deny_dynamodb_table_null if {
	some m in with_id(denies(fx("RT205_deny_dynamodb_table_null")), "RT205")
	contains(m, "env/mem/backend.tf")
}

# --- RT206 ---

test_RT206_allow_no_kms_key_id if {
	count(with_id(denies(ok_tree), "RT206")) == 0
}

test_RT206_deny_kms_key_id if {
	some m in with_id(denies(fx("RT206_deny_kms_key_id")), "RT206")
	contains(m, "env/mem/backend.tf")
}

test_RT206_deny_empty_kms_key_id if {
	some m in with_id(denies(fx("RT206_deny_empty_kms_key_id")), "RT206")
	contains(m, "env/mem/backend.tf")
}

# root の外の backend（usecase の中）は、この検査では見ない（層の検査の LY103 が落とす）。
test_RT206_allow_backend_outside_root if {
	count(with_id(denies(fx("RT206_allow_backend_outside_root")), "RT206")) == 0
	count(with_id(denies(fx("RT206_allow_backend_outside_root")), "RT201")) == 0
}

test_RT201_deny_two_identical_backends_in_one_file if {
	some m in with_id(denies(fx("RT201_deny_two_terraform_blocks_each_with_backend")), "RT201")
	contains(m, "root env/mem の backend が 2 件です")
}

test_RT201_deny_non_s3_backend_in_identity_root if {
	msgs := denies(fx("RT201_deny_non_s3_backend_in_identity_root"))
	some m in with_id(msgs, "RT201")
	contains(m, "env/mem/identity/backend.tf の backend が local です")
	count(with_id(msgs, "RT202")) == 0
	count(with_id(msgs, "RT203")) == 0
}

test_RT201_allow_terraform_block_without_backend if {
	count(with_id(denies(fx("RT201_allow_terraform_block_without_backend")), "RT201")) == 0
}

test_RT202_deny_state_path_omitted if {
	some m in with_id(denies(fx("RT202_deny_state_path_omitted")), "RT202")
	contains(m, "env/mem/backend.tf の key が workload/terraform.tfstate")
}

test_RT202_deny_workload_state_path_in_identity_root if {
	some m in with_id(denies(fx("RT202_deny_workload_state_path_in_identity_root")), "RT202")
	contains(m, "env/mem/identity/backend.tf の key が identity/terraform.tfstate")
}

test_RT202_deny_state_path_number if {
	some m in with_id(denies(fx("RT202_deny_state_path_number")), "RT202")
	contains(m, "env/mem/backend.tf の key が workload/terraform.tfstate のリテラルではありません")
}

test_RT203_deny_lockfile_expression if {
	some m in with_id(denies(fx("RT203_deny_lockfile_expression")), "RT203")
	contains(m, "env/mem/backend.tf の use_lockfile が真偽値のリテラル true ではありません")
}

test_RT204_deny_encrypt_missing if {
	some m in with_id(denies(fx("RT204_deny_encrypt_missing")), "RT204")
	contains(m, "env/mem/backend.tf の encrypt が真偽値のリテラル true ではありません")
}

test_RT204_deny_encrypt_string if {
	some m in with_id(denies(fx("RT204_deny_encrypt_string")), "RT204")
	contains(m, "env/mem/backend.tf の encrypt が真偽値のリテラル true ではありません")
}

test_RT201_deny_two_terraform_blocks_each_with_backend if {
	some m in with_id(denies(fx("RT201_deny_two_terraform_blocks_each_with_backend")), "RT201")
	contains(m, "root env/mem の backend が 2 件です")
}

# root は env/<env>/ と env/<env>/identity/ の位置だけで、その下や別の木の同じ並びは root ではない。
test_RT201_allow_backend_outside_root_positions if {
	count(with_id(denies(fx("RT201_allow_backend_outside_root_positions")), "RT201")) == 0
}
