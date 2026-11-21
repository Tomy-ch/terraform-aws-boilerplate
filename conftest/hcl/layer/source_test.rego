package layer

call(name, source) := sprintf("module %q {\n  source = %q\n}\n", [name, source])

# tree は、ADR-0102 決定17 の許可をすべて使う最小の木である。
test_LY201_allow_relative_paths if {
	count(with_id(denies(fx("LY206_allow_chain")), "LY201")) == 0
}

test_LY201_deny_variable_source if {
	msgs := denies(fx("LY201_deny_variable_source"))
	count(with_id(msgs, "LY201")) == 1
}

test_LY201_deny_registry_source if {
	count(with_id(denies(fx("LY201_deny_registry_source")), "LY201")) == 1
}

test_LY201_deny_git_source if {
	count(with_id(denies(fx("LY201_deny_git_source")), "LY201")) == 1
}

test_LY201_deny_url_source if {
	count(with_id(denies(fx("LY201_deny_url_source")), "LY201")) == 1
}

test_LY201_deny_s3_source if {
	count(with_id(denies(fx("LY201_deny_s3_source")), "LY201")) == 1
}

test_LY201_deny_github_shorthand if {
	count(with_id(denies(fx("LY201_deny_github_shorthand")), "LY201")) == 1
}

test_LY201_deny_missing_source if {
	count(with_id(denies(fx("LY201_deny_missing_source")), "LY201")) == 1
}

test_LY202_allow_parent_reference if {
	count(with_id(denies(fx("LY206_allow_chain")), "LY202")) == 0
}

test_LY202_deny_outside_root if {
	msgs := denies(fx("LY202_deny_outside_root"))
	some m in with_id(msgs, "LY202")
	contains(m, "../../../outside")
}

test_LY203_allow_every_permitted_direction if {
	count(with_id(denies(fx("LY206_allow_chain")), "LY203")) == 0
}

test_LY203_deny_env_to_module if {
	some m in with_id(denies(fx("LY203_deny_env_to_module")), "LY203")
	contains(m, "modules/a")
}

test_LY203_deny_env_to_internal if {
	count(with_id(denies(fx("LY203_deny_env_to_internal")), "LY203")) == 1
}

test_LY203_deny_usecase_to_other_usecase if {
	count(with_id(denies(fx("LY203_deny_usecase_to_other_usecase")), "LY203")) == 1
}

test_LY203_deny_usecase_to_other_internal if {
	count(with_id(denies(fx("LY203_deny_usecase_to_other_internal")), "LY203")) == 1
}

test_LY203_deny_usecase_to_env if {
	count(with_id(denies(fx("LY203_deny_usecase_to_env")), "LY203")) == 1
}

test_LY203_deny_internal_to_other_usecase_internal if {
	count(with_id(denies(fx("LY203_deny_internal_to_other_usecase_internal")), "LY203")) == 1
}

test_LY203_deny_module_to_usecase if {
	count(with_id(denies(fx("LY203_deny_module_to_usecase")), "LY203")) == 1
}

test_LY203_deny_module_to_internal if {
	count(with_id(denies(fx("LY203_deny_module_to_internal")), "LY203")) == 1
}

test_LY203_deny_reference_to_other_usecase if {
	count(with_id(denies(fx("LY203_deny_reference_to_other_usecase")), "LY203")) == 1
}

test_LY203_deny_reference_to_module if {
	count(with_id(denies(fx("LY203_deny_reference_to_module")), "LY203")) == 1
}

test_LY204_allow_existing_target if {
	count(with_id(denies(fx("LY206_allow_chain")), "LY204")) == 0
}

test_LY204_deny_missing_target if {
	some m in with_id(denies(fx("LY204_deny_missing_target")), "LY204")
	contains(m, "modules/zzz")
}

test_LY205_allow_resource_only_in_unplaced_dir if {
	count(with_id(denies(fx("LY205_allow_resource_only_in_unplaced_dir")), "LY205")) == 0
}

test_LY205_deny_call_from_nested_module_dir if {
	some m in with_id(denies(fx("LY205_deny_call_from_nested_module_dir")), "LY205")
	contains(m, "modules/a/sub/main.tf")
}

test_LY205_deny_call_from_deep_internal if {
	count(with_id(denies(fx("LY205_deny_call_from_deep_internal")), "LY205")) == 1
}

test_LY205_deny_call_from_env_dir_itself if {
	count(with_id(denies(fx("LY205_deny_call_from_env_dir_itself")), "LY205")) == 1
}

test_LY206_allow_chain if {
	count(with_id(denies(fx("LY206_allow_chain")), "LY206")) == 0
}

test_LY206_deny_module_cycle if {
	msgs := with_id(denies(fx("LY206_deny_module_cycle")), "LY206")
	count(msgs) == 2
	some ma in msgs
	contains(ma, "LY206 [循環] modules/a の")
	some mb in msgs
	contains(mb, "LY206 [循環] modules/b の")
}

test_LY206_deny_internal_cycle if {
	msgs := with_id(denies(fx("LY206_deny_internal_cycle")), "LY206")
	count(msgs) == 2
	some mi in msgs
	contains(mi, "usecases/u/internal/i の")
	some mj in msgs
	contains(mj, "usecases/u/internal/j の")
}

test_LY206_deny_self_call if {
	msgs := with_id(denies(fx("LY206_deny_self_call")), "LY206")
	count(msgs) == 1
	some m in msgs
	contains(m, "modules/b の")
}

test_LY206_deny_three_node_cycle if {
	count(with_id(denies(fx("LY206_deny_three_node_cycle")), "LY206")) == 3
}

test_LY201_deny_interpolated_relative_source if {
	count(with_id(denies(fx("LY201_deny_interpolated_relative_source_1")), "LY201")) == 1
	count(with_id(denies(fx("LY201_deny_interpolated_relative_source_2")), "LY201")) == 1
}

test_LY201_deny_non_string_source if {
	count(with_id(denies(fx("LY201_deny_non_string_source_1")), "LY201")) == 1
	count(with_id(denies(fx("LY201_deny_non_string_source_2")), "LY201")) == 1
}

test_LY203_deny_module_to_unplaced_dir if {
	msgs := with_id(denies(fx("LY203_deny_module_to_unplaced_dir")), "LY203")
	some m in msgs
	contains(m, "modules/a/sub")
}

test_LY203_deny_env_to_env if {
	count(with_id(denies(fx("LY203_deny_env_to_env")), "LY203")) == 1
}

test_LY203_deny_env_to_reference if {
	count(with_id(denies(fx("LY203_deny_env_to_reference")), "LY203")) == 1
}

test_LY203_deny_internal_to_usecase if {
	count(with_id(denies(fx("LY203_deny_internal_to_usecase")), "LY203")) == 1
}

test_LY203_deny_usecase_to_reference if {
	count(with_id(denies(fx("LY203_deny_usecase_to_reference")), "LY203")) == 1
}

test_LY203_deny_reference_to_own_internal if {
	count(with_id(denies(fx("LY203_deny_reference_to_own_internal")), "LY203")) == 1
}

# root の検査（conftest/hcl/root）は、env の root が usecases/<名前> だけを呼ぶことをこの規則に任せる。
test_LY203_deny_env_to_local_module if {
	msgs := with_id(denies(fx("LY203_deny_env_to_local_module")), "LY203")
	some m in msgs
	contains(m, "env/dev/sub")
}

test_LY203_deny_env_to_identity_root if {
	msgs := with_id(denies(fx("LY203_deny_env_to_identity_root")), "LY203")
	some m in msgs
	contains(m, "env/dev/identity")
}

test_LY203_deny_env_to_repository_root if {
	count(with_id(denies(fx("LY203_deny_env_to_repository_root")), "LY203")) == 1
}

# 構文として読んだ .tf があれば、block が無くても module のディレクトリである。
test_LY204_allow_module_dir_with_comment_only_tf if {
	count(with_id(denies(fx("LY204_allow_module_dir_with_comment_only_tf")), "LY204")) == 0
}

# .tftest.hcl だけを持つディレクトリは、構成の .tf を持たないので module の先ではない。
test_LY204_deny_source_dir_with_only_tftest if {
	only_with(with_id(denies(fx("LY204_deny_source_dir_with_only_tftest")), "LY204"), "usecases/u/internal/i")
}
