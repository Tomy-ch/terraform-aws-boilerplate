package root

# --- RT101 ---

test_RT101_allow_identity_calls_bootstrap_and_control if {
	count(with_id(denies(ok_tree), "RT101")) == 0
}

test_RT101_deny_identity_calls_undeclared_usecase if {
	some m in with_id(denies(fx("RT101_deny_identity_calls_undeclared_usecase")), "RT101")
	contains(m, "env/mem/identity/main.tf の module.w が usecases/w を呼びます")
}

test_RT101_deny_identity_calls_general_module if {
	some m in with_id(denies(fx("RT101_deny_identity_calls_general_module")), "RT101")
	contains(m, "modules/m")
}

# --- RT102 ---

test_RT102_allow_management_calls_control if {
	count(with_id(denies(ok_tree), "RT102")) == 0
}

test_RT102_deny_member_calls_control if {
	some m in with_id(denies(fx("RT102_deny_member_calls_control")), "RT102")
	contains(m, "env/mem/identity/main.tf の module.c")
}

# --- RT103 ---

test_RT103_allow_workload_calls_other_usecase if {
	count(with_id(denies(ok_tree), "RT103")) == 0
}

test_RT103_deny_workload_calls_bootstrap if {
	some m in with_id(denies(fx("RT103_deny_workload_calls_bootstrap")), "RT103")
	contains(m, "bootstrap の構成物を持つ usecases/b")
}

test_RT103_deny_workload_calls_control if {
	some m in with_id(denies(fx("RT103_deny_workload_calls_control")), "RT103")
	contains(m, "control の構成物を持つ usecases/c")
}

# --- RT104 ---

test_RT104_allow_relative_sources if {
	count(with_id(denies(ok_tree), "RT104")) == 0
}

test_RT104_deny_remote_source if {
	some m in with_id(denies(fx("RT104_deny_remote_source")), "RT104")
	contains(m, "env/mem/main.tf の module.w")
}

test_RT104_deny_interpolated_source if {
	some m in with_id(denies(fx("RT104_deny_interpolated_source")), "RT104")
	contains(m, "env/mem/main.tf の module.w")
}

test_RT104_deny_escaping_source if {
	some m in with_id(denies(fx("RT104_deny_escaping_source")), "RT104")
	contains(m, "env/mem/main.tf の module.w")
}

test_RT104_deny_missing_source if {
	some m in with_id(denies(fx("RT104_deny_missing_source")), "RT104")
	contains(m, "env/mem/main.tf の module.w")
}

# root の外（usecase の中）の module の source は、この検査では判定しない（層の検査の LY201・LY202 が持つ）。
test_RT104_allow_non_root_remote_source if {
	count(with_id(denies(fx("RT104_allow_non_root_remote_source")), "RT104")) == 0
}

test_RT102_deny_control_call_when_management_env_missing if {
	msgs := denies_decl({"usecases": fixture_decl.usecases})
	some m in with_id(msgs, "RT102")
	contains(m, "env/mgmt/identity/main.tf の module.c")
	contains(m, "管理アカウント（null）")
}
