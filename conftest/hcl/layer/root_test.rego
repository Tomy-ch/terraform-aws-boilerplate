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

test_LY101_allow_resource_below_usecase if {
	count(with_id(denies(fx("LY101_allow_resource_below_usecase")), "LY101")) == 0
}

test_LY101_deny_resource_in_usecase if {
	msgs := denies(fx("LY101_deny_resource_in_usecase"))
	some m in with_id(msgs, "LY101")
	contains(m, "usecases/u/extra.tf が resource aws_s3_bucket.x")
}

test_LY102_allow_provider_in_tests_and_reference if {
	count(with_id(denies(fx("LY102_allow_provider_in_tests_and_reference")), "LY102")) == 0
}

test_LY102_deny_provider_in_usecase if {
	msgs := denies(fx("LY102_deny_provider_in_usecase"))
	some m in with_id(msgs, "LY102")
	contains(m, "provider aws")
}

test_LY103_allow_required_providers_moved_removed if {
	count(with_id(denies(fx("LY103_allow_required_providers_moved_removed")), "LY103")) == 0
}

test_LY103_deny_import if {
	msgs := denies(fx("LY103_deny_import"))
	some m in with_id(msgs, "LY103")
	contains(m, "が import を持ちます")
}

test_LY103_deny_backend if {
	msgs := denies(fx("LY103_deny_backend"))
	some m in with_id(msgs, "LY103")
	contains(m, "が backend を持ちます")
}

test_LY103_deny_cloud if {
	msgs := denies(fx("LY103_deny_cloud"))
	some m in with_id(msgs, "LY103")
	contains(m, "が cloud を持ちます")
}

test_LY103_allow_env_root_only_blocks if {
	count(with_id(denies(fx("LY103_allow_env_root_only_blocks")), "LY103")) == 0
}
