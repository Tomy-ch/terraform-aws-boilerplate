package layer

call(name, source) := sprintf("module %q {\n  source = %q\n}\n", [name, source])

# tree は、ADR-0102 決定17 の許可をすべて使う最小の木である。
tree := [
	tf("env/dev/main.tf", call("u", "../../usecases/u")),
	tf("env/dev/identity/main.tf", call("u", "../../../usecases/u")),
	tf("usecases/u/main.tf", concat("", [call("a", "../../modules/a"), call("i", "./internal/i")])),
	tf("usecases/u/internal/i/main.tf", concat("", [call("a", "../../../../modules/a"), call("j", "../j")])),
	tf("usecases/u/internal/j/main.tf", ""),
	tf("usecases/u/reference/main.tf", call("u", "../")),
	tf("usecases/v/main.tf", ""),
	tf("usecases/v/internal/k/main.tf", ""),
	tf("modules/a/main.tf", call("b", "../b")),
	tf("modules/b/main.tf", ""),
]

with_file(path, src) := array.concat([f | some f in tree; f.path != path], [tf(path, src)])

source_denies(path, src) := denies(with_file(path, src))

test_LY201_allow_relative_paths if {
	count(with_id(denies(tree), "LY201")) == 0
}

test_LY201_deny_variable_source if {
	msgs := source_denies("modules/a/main.tf", `module "b" {
	source = var.src
}
`)
	count(with_id(msgs, "LY201")) == 1
}

test_LY201_deny_registry_source if {
	count(with_id(source_denies("modules/a/main.tf", call("b", "hashicorp/consul/aws")), "LY201")) == 1
}

test_LY201_deny_git_source if {
	count(with_id(source_denies("modules/a/main.tf", call("b", "git::https://example.com/m.git")), "LY201")) == 1
}

test_LY201_deny_url_source if {
	count(with_id(source_denies("modules/a/main.tf", call("b", "https://example.com/m.zip")), "LY201")) == 1
}

test_LY201_deny_s3_source if {
	count(with_id(source_denies("modules/a/main.tf", call("b", "s3::https://s3.amazonaws.com/b/m.zip")), "LY201")) == 1
}

test_LY201_deny_github_shorthand if {
	count(with_id(source_denies("modules/a/main.tf", call("b", "github.com/o/r")), "LY201")) == 1
}

test_LY201_deny_missing_source if {
	count(with_id(source_denies("modules/a/main.tf", `module "b" {}`), "LY201")) == 1
}

test_LY202_allow_parent_reference if {
	count(with_id(denies(tree), "LY202")) == 0
}

test_LY202_deny_outside_root if {
	msgs := source_denies("modules/a/main.tf", call("b", "../../../outside"))
	some m in with_id(msgs, "LY202")
	contains(m, "../../../outside")
}

test_LY203_allow_every_permitted_direction if {
	count(with_id(denies(tree), "LY203")) == 0
}

test_LY203_deny_env_to_module if {
	some m in with_id(source_denies("env/dev/main.tf", call("a", "../../modules/a")), "LY203")
	contains(m, "modules/a")
}

test_LY203_deny_env_to_internal if {
	count(with_id(source_denies("env/dev/main.tf", call("i", "../../usecases/u/internal/i")), "LY203")) == 1
}

test_LY203_deny_usecase_to_other_usecase if {
	count(with_id(source_denies("usecases/u/main.tf", call("v", "../v")), "LY203")) == 1
}

test_LY203_deny_usecase_to_other_internal if {
	count(with_id(source_denies("usecases/u/main.tf", call("k", "../v/internal/k")), "LY203")) == 1
}

test_LY203_deny_usecase_to_env if {
	count(with_id(source_denies("usecases/u/main.tf", call("e", "../../env/dev")), "LY203")) == 1
}

test_LY203_deny_internal_to_other_usecase_internal if {
	count(with_id(source_denies("usecases/u/internal/i/main.tf", call("k", "../../../v/internal/k")), "LY203")) == 1
}

test_LY203_deny_module_to_usecase if {
	count(with_id(source_denies("modules/a/main.tf", call("u", "../../usecases/u")), "LY203")) == 1
}

test_LY203_deny_module_to_internal if {
	count(with_id(source_denies("modules/a/main.tf", call("i", "../../usecases/u/internal/i")), "LY203")) == 1
}

test_LY203_deny_reference_to_other_usecase if {
	count(with_id(source_denies("usecases/u/reference/main.tf", call("v", "../../v")), "LY203")) == 1
}

test_LY203_deny_reference_to_module if {
	count(with_id(source_denies("usecases/u/reference/main.tf", call("a", "../../../modules/a")), "LY203")) == 1
}

test_LY204_allow_existing_target if {
	count(with_id(denies(tree), "LY204")) == 0
}

test_LY204_deny_missing_target if {
	some m in with_id(source_denies("modules/a/main.tf", call("z", "../zzz")), "LY204")
	contains(m, "modules/zzz")
}

test_LY205_allow_resource_only_in_unplaced_dir if {
	count(with_id(denies(array.concat(tree, [tf("modules/a/sub/main.tf", `resource "aws_s3_bucket" "this" {}`)])), "LY205")) == 0
}

test_LY205_deny_call_from_nested_module_dir if {
	some m in with_id(denies(array.concat(tree, [tf("modules/a/sub/main.tf", call("b", "../../b"))])), "LY205")
	contains(m, "modules/a/sub/main.tf")
}

test_LY205_deny_call_from_deep_internal if {
	count(with_id(denies(array.concat(tree, [tf("usecases/u/internal/i/j/main.tf", call("a", "../../../../../modules/a"))])), "LY205")) == 1
}

test_LY205_deny_call_from_env_dir_itself if {
	count(with_id(denies(array.concat(tree, [tf("env/main.tf", call("u", "../usecases/u"))])), "LY205")) == 1
}

test_LY206_allow_chain if {
	count(with_id(denies(tree), "LY206")) == 0
}

test_LY206_deny_module_cycle if {
	msgs := source_denies("modules/b/main.tf", call("a", "../a"))
	count(with_id(msgs, "LY206")) == 2
}

test_LY206_deny_internal_cycle if {
	msgs := source_denies("usecases/u/internal/j/main.tf", call("i", "../i"))
	count(with_id(msgs, "LY206")) == 2
}
