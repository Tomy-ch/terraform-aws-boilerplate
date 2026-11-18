package root

with_backend(path, src) := replaced(ok_tree, path, src)

backend_attrs(key, extra) := sprintf(`terraform {
	backend "s3" {
		key    = %s
		region = "ap-northeast-1"
%s
	}
}
`, [key, extra])

# --- RT201 ---

test_RT201_allow_one_s3_backend_per_root if {
	count(with_id(denies(ok_tree), "RT201")) == 0
}

test_RT201_deny_backend_missing if {
	files := without(ok_tree, "env/mem/backend.tf")
	some m in with_id(denies(files), "RT201")
	contains(m, "root env/mem の backend が 0 件です")
}

test_RT201_deny_two_backends if {
	files := array.concat(ok_tree, [tf("env/mem/backend2.tf", workload_backend)])
	msgs := with_id(denies(files), "RT201")
	some m in msgs
	contains(m, "root env/mem の backend が 2 件です")
}

test_RT201_deny_backend_outside_backend_tf if {
	files := array.concat(without(ok_tree, "env/mem/backend.tf"), [tf("env/mem/versions.tf", workload_backend)])
	some m in with_id(denies(files), "RT201")
	contains(m, "env/mem/versions.tf が backend を持ちます")
}

test_RT201_deny_non_s3_backend if {
	files := with_backend("env/mem/backend.tf", "terraform {\n  backend \"local\" {\n    path = \"x\"\n  }\n}\n")
	some m in with_id(denies(files), "RT201")
	contains(m, "backend が local です")
}

test_RT201_deny_cloud_block if {
	files := array.concat(ok_tree, [tf("env/mem/cloud.tf", "terraform {\n  cloud {\n    organization = \"x\"\n  }\n}\n")])
	some m in with_id(denies(files), "RT201")
	contains(m, "cloud block")
}

# --- RT202 ---

test_RT202_allow_expected_keys if {
	count(with_id(denies(ok_tree), "RT202")) == 0
}

test_RT202_deny_wrong_key if {
	files := with_backend("env/mem/backend.tf", backend_src("identity/terraform.tfstate"))
	some m in with_id(denies(files), "RT202")
	contains(m, "env/mem/backend.tf の key が workload/terraform.tfstate")
}

test_RT202_deny_key_null if {
	files := with_backend("env/mem/identity/backend.tf", backend_attrs("null", "    use_lockfile = true\n    encrypt = true"))
	some m in with_id(denies(files), "RT202")
	contains(m, "env/mem/identity/backend.tf")
}

test_RT202_deny_key_expression if {
	files := with_backend("env/mem/backend.tf", backend_attrs(`"${var.k}"`, "    use_lockfile = true\n    encrypt = true"))
	some m in with_id(denies(files), "RT202")
	contains(m, "env/mem/backend.tf")
}

# --- RT203 ---

test_RT203_allow_lockfile_true if {
	count(with_id(denies(ok_tree), "RT203")) == 0
}

test_RT203_deny_lockfile_missing if {
	files := with_backend("env/mem/backend.tf", backend_attrs(`"workload/terraform.tfstate"`, "    encrypt = true"))
	some m in with_id(denies(files), "RT203")
	contains(m, "env/mem/backend.tf")
}

test_RT203_deny_lockfile_false if {
	files := with_backend("env/mem/backend.tf", backend_attrs(`"workload/terraform.tfstate"`, "    use_lockfile = false\n    encrypt = true"))
	some m in with_id(denies(files), "RT203")
	contains(m, "env/mem/backend.tf")
}

test_RT203_deny_lockfile_string if {
	files := with_backend("env/mem/backend.tf", backend_attrs(`"workload/terraform.tfstate"`, "    use_lockfile = \"true\"\n    encrypt = true"))
	some m in with_id(denies(files), "RT203")
	contains(m, "env/mem/backend.tf")
}

# --- RT204 ---

test_RT204_allow_encrypt_true if {
	count(with_id(denies(ok_tree), "RT204")) == 0
}

test_RT204_deny_encrypt_false if {
	files := with_backend("env/mem/backend.tf", backend_attrs(`"workload/terraform.tfstate"`, "    use_lockfile = true\n    encrypt = false"))
	some m in with_id(denies(files), "RT204")
	contains(m, "env/mem/backend.tf")
}

test_RT204_deny_encrypt_expression if {
	files := with_backend("env/mem/backend.tf", backend_attrs(`"workload/terraform.tfstate"`, "    use_lockfile = true\n    encrypt = var.e"))
	some m in with_id(denies(files), "RT204")
	contains(m, "env/mem/backend.tf")
}

# --- RT205 ---

test_RT205_allow_no_dynamodb_table if {
	count(with_id(denies(ok_tree), "RT205")) == 0
}

test_RT205_deny_dynamodb_table if {
	files := with_backend("env/mem/backend.tf", backend_attrs(`"workload/terraform.tfstate"`, "    use_lockfile = true\n    encrypt = true\n    dynamodb_table = \"locks\""))
	some m in with_id(denies(files), "RT205")
	contains(m, "env/mem/backend.tf")
}

test_RT205_deny_dynamodb_table_null if {
	files := with_backend("env/mem/backend.tf", backend_attrs(`"workload/terraform.tfstate"`, "    use_lockfile = true\n    encrypt = true\n    dynamodb_table = null"))
	some m in with_id(denies(files), "RT205")
	contains(m, "env/mem/backend.tf")
}

# --- RT206 ---

test_RT206_allow_no_kms_key_id if {
	count(with_id(denies(ok_tree), "RT206")) == 0
}

test_RT206_deny_kms_key_id if {
	files := with_backend("env/mem/backend.tf", backend_attrs(`"workload/terraform.tfstate"`, "    use_lockfile = true\n    encrypt = true\n    kms_key_id = \"arn:aws:kms:ap-northeast-1:111122223333:key/x\""))
	some m in with_id(denies(files), "RT206")
	contains(m, "env/mem/backend.tf")
}

test_RT206_deny_empty_kms_key_id if {
	files := with_backend("env/mem/backend.tf", backend_attrs(`"workload/terraform.tfstate"`, "    use_lockfile = true\n    encrypt = true\n    kms_key_id = \"\""))
	some m in with_id(denies(files), "RT206")
	contains(m, "env/mem/backend.tf")
}

# root の外の backend（usecase の中）は、この検査では見ない（層の検査の LY103 が落とす）。
test_RT206_allow_backend_outside_root if {
	files := array.concat(ok_tree, [tf("usecases/w/backend.tf", "terraform {\n  backend \"s3\" {\n    kms_key_id = \"x\"\n  }\n}\n")])
	count(with_id(denies(files), "RT206")) == 0
	count(with_id(denies(files), "RT201")) == 0
}

backend_without_key := `terraform {
	backend "s3" {
		region       = "ap-northeast-1"
		use_lockfile = true
		encrypt      = true
	}
}
`

test_RT201_deny_two_identical_backends_in_one_file if {
	files := with_backend("env/mem/backend.tf", concat("\n", [workload_backend, workload_backend]))
	some m in with_id(denies(files), "RT201")
	contains(m, "root env/mem の backend が 2 件です")
}

test_RT201_deny_non_s3_backend_in_identity_root if {
	files := with_backend("env/mem/identity/backend.tf", "terraform {\n  backend \"local\" {\n    path = \"x\"\n  }\n}\n")
	msgs := denies(files)
	some m in with_id(msgs, "RT201")
	contains(m, "env/mem/identity/backend.tf の backend が local です")
	count(with_id(msgs, "RT202")) == 0
	count(with_id(msgs, "RT203")) == 0
}

test_RT201_allow_terraform_block_without_backend if {
	files := array.concat(ok_tree, [tf("env/mem/versions.tf", "terraform {\n  required_version = \">= 1.0\"\n}\n")])
	count(with_id(denies(files), "RT201")) == 0
}

test_RT202_deny_key_omitted if {
	files := with_backend("env/mem/backend.tf", backend_without_key)
	some m in with_id(denies(files), "RT202")
	contains(m, "env/mem/backend.tf の key が workload/terraform.tfstate")
}

test_RT202_deny_workload_key_in_identity_root if {
	files := with_backend("env/mem/identity/backend.tf", workload_backend)
	some m in with_id(denies(files), "RT202")
	contains(m, "env/mem/identity/backend.tf の key が identity/terraform.tfstate")
}

test_RT202_deny_key_number if {
	files := with_backend("env/mem/backend.tf", backend_attrs("1", "    use_lockfile = true\n    encrypt = true"))
	some m in with_id(denies(files), "RT202")
	contains(m, "env/mem/backend.tf")
}

test_RT203_deny_lockfile_expression if {
	files := with_backend("env/mem/backend.tf", backend_attrs(`"workload/terraform.tfstate"`, "    use_lockfile = var.l\n    encrypt = true"))
	some m in with_id(denies(files), "RT203")
	contains(m, "env/mem/backend.tf")
}

test_RT204_deny_encrypt_missing if {
	files := with_backend("env/mem/backend.tf", backend_attrs(`"workload/terraform.tfstate"`, "    use_lockfile = true"))
	some m in with_id(denies(files), "RT204")
	contains(m, "env/mem/backend.tf")
}

test_RT204_deny_encrypt_string if {
	files := with_backend("env/mem/backend.tf", backend_attrs(`"workload/terraform.tfstate"`, "    use_lockfile = true\n    encrypt = \"true\""))
	some m in with_id(denies(files), "RT204")
	contains(m, "env/mem/backend.tf")
}

# --- RT106 ---

test_RT106_allow_backends_in_roots_and_outside_env if {
	files := array.concat(ok_tree, [tf("usecases/w/backend.tf", workload_backend)])
	count(with_id(denies(files), "RT106")) == 0
}

test_RT106_deny_backend_in_hidden_directory if {
	files := array.concat(ok_tree, [tf("env/mem/.alt/backend.tf", workload_backend)])
	some m in with_id(denies(files), "RT106")
	contains(m, "env/mem/.alt/backend.tf が backend を持ちます")
}

test_RT106_deny_backend_in_tests_directory if {
	files := array.concat(ok_tree, [tf("env/mem/identity/tests/backend.tf", identity_backend)])
	some m in with_id(denies(files), "RT106")
	contains(m, "env/mem/identity/tests/backend.tf が backend を持ちます")
}

test_RT106_deny_cloud_block_outside_root if {
	files := array.concat(ok_tree, [tf("env/mem/sub/cloud.tf", "terraform {\n  cloud {\n    organization = \"x\"\n  }\n}\n")])
	some m in with_id(denies(files), "RT106")
	contains(m, "env/mem/sub/cloud.tf が terraform の cloud block を持ちます")
}
