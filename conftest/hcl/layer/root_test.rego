package layer

usecase_ok := `terraform {
	required_providers {
		aws = {
			source = "hashicorp/aws"
		}
	}
}

module "m" {
	source = "../../modules/m"
}

data "aws_caller_identity" "this" {}

moved {
	from = module.old
	to   = module.m
}

removed {
	from = module.gone
	lifecycle {
		destroy = false
	}
}
`

usecase_files_with(path, src) := [ok_module, tf("usecases/u/main.tf", usecase_ok), tf(path, src)]

test_LY101_allow_resource_below_usecase if {
	files := [
		ok_module,
		tf("usecases/u/main.tf", usecase_ok),
		tf("usecases/u/internal/i/main.tf", `resource "aws_s3_bucket" "this" {}`),
		tf("usecases/u/reference/main.tf", `resource "aws_s3_bucket" "this" {}`),
	]
	count(with_id(denies(files), "LY101")) == 0
}

test_LY101_deny_resource_in_usecase if {
	msgs := denies(usecase_files_with("usecases/u/extra.tf", `resource "aws_s3_bucket" "x" {}`))
	some m in with_id(msgs, "LY101")
	contains(m, "usecases/u/extra.tf が resource aws_s3_bucket.x")
}

test_LY102_allow_provider_in_tests_and_reference if {
	files := [
		ok_module,
		tf("usecases/u/main.tf", usecase_ok),
		tf("usecases/u/tests/a.tftest.hcl", `provider "aws" {}`),
		tf("usecases/u/main.tftest.hcl", `provider "aws" {}`),
		tf("usecases/u/reference/providers.tf", `provider "aws" {}`),
	]
	count(with_id(denies(files), "LY102")) == 0
}

test_LY102_deny_provider_in_usecase if {
	msgs := denies(usecase_files_with("usecases/u/providers.tf", `provider "aws" {}`))
	some m in with_id(msgs, "LY102")
	contains(m, "provider aws")
}

test_LY103_allow_required_providers_moved_removed if {
	count(with_id(denies([ok_module, tf("usecases/u/main.tf", usecase_ok)]), "LY103")) == 0
}

test_LY103_deny_import if {
	msgs := denies(usecase_files_with("usecases/u/extra.tf", `import {
	to = module.m.aws_s3_bucket.this
	id = "x"
}
`))
	some m in with_id(msgs, "LY103")
	contains(m, "が import を持ちます")
}

test_LY103_deny_backend if {
	msgs := denies(usecase_files_with("usecases/u/extra.tf", `terraform {
	backend "s3" {}
}
`))
	some m in with_id(msgs, "LY103")
	contains(m, "が backend を持ちます")
}

test_LY103_deny_cloud if {
	msgs := denies(usecase_files_with("usecases/u/extra.tf", `terraform {
	cloud {}
}
`))
	some m in with_id(msgs, "LY103")
	contains(m, "が cloud を持ちます")
}
