package root

# --- RT101 ---

test_RT101_allow_identity_calls_bootstrap_and_control if {
	count(with_id(denies(ok_tree), "RT101")) == 0
}

test_RT101_deny_identity_calls_undeclared_usecase if {
	files := replaced(ok_tree, "env/mem/identity/main.tf", call("w", "../../../usecases/w"))
	some m in with_id(denies(files), "RT101")
	contains(m, "env/mem/identity/main.tf の module.w が usecases/w を呼びます")
}

test_RT101_deny_identity_calls_general_module if {
	files := array.concat(
		replaced(ok_tree, "env/mem/identity/main.tf", call("m", "../../../modules/m")),
		[tf("modules/m/main.tf", "resource \"aws_s3_bucket\" \"this\" {}\n")],
	)
	some m in with_id(denies(files), "RT101")
	contains(m, "modules/m")
}

# --- RT102 ---

test_RT102_allow_management_calls_control if {
	count(with_id(denies(ok_tree), "RT102")) == 0
}

test_RT102_deny_member_calls_control if {
	files := replaced(ok_tree, "env/mem/identity/main.tf", concat("\n", [call("b", "../../../usecases/b"), call("c", "../../../usecases/c")]))
	some m in with_id(denies(files), "RT102")
	contains(m, "env/mem/identity/main.tf の module.c")
}

# --- RT103 ---

test_RT103_allow_workload_calls_other_usecase if {
	count(with_id(denies(ok_tree), "RT103")) == 0
}

test_RT103_deny_workload_calls_bootstrap if {
	files := replaced(ok_tree, "env/mem/main.tf", concat("\n", [call("w", "../../usecases/w"), call("b", "../../usecases/b")]))
	some m in with_id(denies(files), "RT103")
	contains(m, "bootstrap の構成物を持つ usecases/b")
}

test_RT103_deny_workload_calls_control if {
	files := replaced(ok_tree, "env/mgmt/main.tf", call("c", "../../usecases/c"))
	files2 := array.concat(files, [tf("env/mgmt/backend.tf", workload_backend)])
	some m in with_id(denies(files2), "RT103")
	contains(m, "control の構成物を持つ usecases/c")
}

# --- RT104 ---

test_RT104_allow_relative_sources if {
	count(with_id(denies(ok_tree), "RT104")) == 0
}

test_RT104_deny_remote_source if {
	files := replaced(ok_tree, "env/mem/main.tf", call("w", "git::https://example.com/m.git"))
	some m in with_id(denies(files), "RT104")
	contains(m, "env/mem/main.tf の module.w")
}

test_RT104_deny_interpolated_source if {
	files := replaced(ok_tree, "env/mem/main.tf", "module \"w\" {\n  source = \"../../usecases/${var.u}\"\n}\n")
	some m in with_id(denies(files), "RT104")
	contains(m, "env/mem/main.tf の module.w")
}

test_RT104_deny_escaping_source if {
	files := replaced(ok_tree, "env/mem/main.tf", call("w", "../../../../outside"))
	some m in with_id(denies(files), "RT104")
	contains(m, "env/mem/main.tf の module.w")
}

test_RT104_deny_missing_source if {
	files := replaced(ok_tree, "env/mem/main.tf", "module \"w\" {}\n")
	some m in with_id(denies(files), "RT104")
	contains(m, "env/mem/main.tf の module.w")
}

# root の外（usecase の中）の module の source は、この検査では判定しない（層の検査の LY201・LY202 が持つ）。
test_RT104_allow_non_root_remote_source if {
	files := replaced(ok_tree, "usecases/w/main.tf", call("x", "git::https://example.com/m.git"))
	count(with_id(denies(files), "RT104")) == 0
}

test_RT102_deny_control_call_when_management_env_missing if {
	msgs := denies_decl({"usecases": fixture_decl.usecases})
	some m in with_id(msgs, "RT102")
	contains(m, "env/mgmt/identity/main.tf の module.c")
}

# --- RT105 ---

test_RT105_allow_usecase_directories if {
	count(with_id(denies(ok_tree), "RT105")) == 0
}

test_RT105_deny_usecase_subdirectory if {
	files := array.concat(
		replaced(ok_tree, "env/mem/main.tf", call("x", "../../usecases/b/internal/x")),
		[tf("usecases/b/internal/x/main.tf", "resource \"aws_s3_bucket\" \"x\" {}\n")],
	)
	some m in with_id(denies(files), "RT105")
	contains(m, "env/mem/main.tf の module.x が usecases/b/internal/x を指します")
}

test_RT105_deny_other_root if {
	files := replaced(ok_tree, "env/mem/main.tf", call("x", "../mgmt/identity"))
	some m in with_id(denies(files), "RT105")
	contains(m, "env/mem/main.tf の module.x が env/mgmt/identity を指します")
}

test_RT105_deny_local_module if {
	files := array.concat(
		replaced(ok_tree, "env/mem/main.tf", call("x", "./sub")),
		[tf("env/mem/sub/main.tf", call("b", "../../../usecases/b"))],
	)
	some m in with_id(denies(files), "RT105")
	contains(m, "env/mem/main.tf の module.x が env/mem/sub を指します")
}

test_RT105_deny_repository_root if {
	files := replaced(ok_tree, "env/mem/main.tf", call("x", "../.."))
	some m in with_id(denies(files), "RT105")
	contains(m, "env/mem/main.tf の module.x")
}

test_RT105_deny_missing_usecase if {
	files := replaced(ok_tree, "env/mem/main.tf", call("x", "../../usecases/nonexist"))
	some m in with_id(denies(files), "RT105")
	contains(m, "usecases/nonexist")
}
