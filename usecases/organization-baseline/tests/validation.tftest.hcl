mock_provider "aws" {
  mock_data "aws_ssm_parameter" {
    defaults = { value = "{\"security\":\"security@example.com\",\"log-archive\":\"log-archive@example.com\"}" }
  }
  mock_data "aws_caller_identity" {
    defaults = { account_id = "111111111111" }
  }
  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }
  mock_data "aws_region" {
    defaults = { region = "ap-northeast-1" }
  }
  mock_resource "aws_organizations_organization" {
    defaults = {
      id                = "o-exampleorg1"
      master_account_id = "111111111111"
      roots             = [{ id = "r-exam", arn = "arn:aws:organizations::111111111111:root/o-exampleorg1/r-exam", name = "Root", policy_types = [] }]
    }
  }
  # OU の ID は子の OU の parent_id の検査を通る形にする。
  mock_resource "aws_organizations_organizational_unit" {
    defaults = { id = "ou-exam-abcdefgh" }
  }
}

variables {
  project                      = "tabp"
  bootstrap_read_principal_arn = "arn:aws:iam::111111111111:role/tabp-admin-ci-plan"
  state_bucket_arn             = "arn:aws:s3:::tabp-admin-tfstate-111111111111-ap-northeast-1-an"
  plan_output_bucket_arn       = "arn:aws:s3:::tabp-admin-tfplan-111111111111-ap-northeast-1-an"
  apply_output_bucket_arn      = "arn:aws:s3:::tabp-admin-tfapply-111111111111-ap-northeast-1-an"
  state_key_arn                = "arn:aws:kms:ap-northeast-1:111111111111:key/00000000-0000-0000-0000-000000000000"
}

override_data {
  target = module.access[0].data.aws_ssoadmin_instances.this
  values = { arns = ["arn:aws:sso:::instance/ssoins-0000000000000000"], identity_store_ids = ["d-0000000000"] }
}

override_data {
  target = module.access[0].data.aws_ssm_parameter.principals
  values = { value = "{\"users\":{\"owner\":\"00000000-0000-0000-0000-000000000001\"},\"groups\":{\"operators\":\"00000000-0000-0000-0000-000000000002\"}}" }
}

run "accepts_13_character_project" {
  command = plan

  variables {
    project = "abcdefghijklm"
  }
}

run "rejects_empty_project" {
  command = plan

  variables {
    project = ""
  }

  expect_failures = [var.project]
}

run "rejects_too_long_project" {
  command = plan

  variables {
    project = "abcdefghijklmn"
  }

  expect_failures = [var.project]
}

run "rejects_hyphen_in_project" {
  command = plan

  variables {
    project = "tabp-x"
  }

  expect_failures = [var.project]
}

run "rejects_uppercase_project" {
  command = plan

  variables {
    project = "Tabp"
  }

  expect_failures = [var.project]
}

run "accepts_member_envs_in_bootstrapping" {
  command = plan

  variables {
    bootstrapping = ["security", "log-archive"]
  }
}

run "rejects_unknown_env_in_bootstrapping" {
  command = plan

  variables {
    bootstrapping = ["security", "dev"]
  }

  expect_failures = [var.bootstrapping]
}

run "rejects_wildcard_bootstrap_read_principal" {
  command = plan

  variables {
    bootstrap_read_principal_arn = "arn:aws:iam::111111111111:role/tabp-*"
  }

  expect_failures = [var.bootstrap_read_principal_arn]
}

run "rejects_non_role_bootstrap_read_principal" {
  command = plan

  variables {
    bootstrap_read_principal_arn = "arn:aws:iam::111111111111:user/tabp-admin"
  }

  expect_failures = [var.bootstrap_read_principal_arn]
}

run "accepts_identity_center" {
  command = plan

  variables {
    identity_center = {
      management_assignments = [
        { permission_set = "organization_admin", user = "owner" },
        { permission_set = "read_only", user = "owner" },
      ]
      member_assignments = [{ account = "security", permission_set = "read_only", group = "operators" }]
    }
  }

  assert {
    condition     = length(module.access) == 1
    error_message = "identity_center を渡したとき、permission set と割当を作っていません。"
  }
}

run "accepts_identity_center_without_member_assignments" {
  command = plan

  variables {
    identity_center = {
      management_assignments = [{ permission_set = "organization_admin", user = "owner" }]
    }
  }

  assert {
    condition     = length(module.access) == 1 && length(var.identity_center.member_assignments) == 0
    error_message = "member_assignments を書かないとき、メンバーへの割当を空にしてください。"
  }
}

run "accepts_null_member_assignments" {
  command = plan

  variables {
    identity_center = {
      management_assignments = [{ permission_set = "organization_admin", user = "owner" }]
      member_assignments     = null
    }
  }

  assert {
    condition     = length(module.access) == 1 && length(var.identity_center.member_assignments) == 0
    error_message = "member_assignments が null のとき、メンバーへの割当を空にしてください。"
  }
}

run "rejects_null_management_assignments" {
  command = plan

  variables {
    identity_center = {
      management_assignments = null
    }
  }

  expect_failures = [var.identity_center]
}

run "accepts_32_character_labels" {
  command = plan

  variables {
    identity_center = {
      management_assignments = [{ permission_set = "organization_admin", user = "abcdefghijklmnopqrstuvwxyz012345" }]
      member_assignments     = [{ account = "security", permission_set = "read_only", group = "bcdefghijklmnopqrstuvwxyz0123456" }]
    }
  }

  override_data {
    target = module.access[0].data.aws_ssm_parameter.principals
    values = { value = "{\"users\":{\"abcdefghijklmnopqrstuvwxyz012345\":\"00000000-0000-0000-0000-000000000001\"},\"groups\":{\"bcdefghijklmnopqrstuvwxyz0123456\":\"00000000-0000-0000-0000-000000000002\"}}" }
  }
}

run "rejects_33_character_user_label" {
  command = plan

  variables {
    identity_center = {
      management_assignments = [{ permission_set = "organization_admin", user = "abcdefghijklmnopqrstuvwxyz0123456" }]
    }
  }

  expect_failures = [var.identity_center]
}

run "rejects_33_character_group_label" {
  command = plan

  variables {
    identity_center = {
      management_assignments = [{ permission_set = "organization_admin", user = "owner" }]
      member_assignments     = [{ account = "security", permission_set = "read_only", group = "bcdefghijklmnopqrstuvwxyz01234567" }]
    }
  }

  expect_failures = [var.identity_center]
}

run "rejects_malformed_group_label" {
  command = plan

  variables {
    identity_center = {
      management_assignments = [{ permission_set = "organization_admin", user = "owner" }]
      member_assignments     = [{ account = "security", permission_set = "read_only", group = "Operators" }]
    }
  }

  expect_failures = [var.identity_center]
}

run "null_identity_center_creates_nothing" {
  command = plan

  assert {
    condition     = length(module.access) == 0
    error_message = "identity_center が null のとき、permission set を作らないでください（Identity Center が無い段4 の plan が落ちる）。"
  }
}

run "rejects_unknown_permission_set" {
  command = plan

  variables {
    identity_center = {
      management_assignments = [
        { permission_set = "organization_admin", user = "owner" },
        { permission_set = "power_user", user = "owner" },
      ]
    }
  }

  expect_failures = [var.identity_center]
}

run "rejects_organization_admin_to_member" {
  command = plan

  variables {
    identity_center = {
      management_assignments = [{ permission_set = "organization_admin", user = "owner" }]
      member_assignments     = [{ account = "security", permission_set = "organization_admin", group = "operators" }]
    }
  }

  expect_failures = [var.identity_center]
}

run "rejects_no_organization_admin" {
  command = plan

  variables {
    identity_center = {
      management_assignments = [{ permission_set = "read_only", user = "owner" }]
    }
  }

  expect_failures = [var.identity_center]
}

run "rejects_empty_management_assignments" {
  command = plan

  variables {
    identity_center = {
      management_assignments = []
    }
  }

  expect_failures = [var.identity_center]
}

run "rejects_email_as_label" {
  command = plan

  variables {
    identity_center = {
      management_assignments = [{ permission_set = "organization_admin", user = "owner@example.com" }]
    }
  }

  expect_failures = [var.identity_center]
}

run "accepts_1_character_labels" {
  command = plan

  variables {
    identity_center = {
      management_assignments = [{ permission_set = "organization_admin", user = "o" }]
      member_assignments     = [{ account = "security", permission_set = "read_only", group = "g" }]
    }
  }

  override_data {
    target = module.access[0].data.aws_ssm_parameter.principals
    values = { value = "{\"users\":{\"o\":\"00000000-0000-0000-0000-000000000001\"},\"groups\":{\"g\":\"00000000-0000-0000-0000-000000000002\"}}" }
  }
}

run "rejects_label_with_leading_digit" {
  command = plan

  variables {
    identity_center = {
      management_assignments = [{ permission_set = "organization_admin", user = "1owner" }]
    }
  }

  expect_failures = [var.identity_center]
}

run "rejects_label_with_leading_hyphen" {
  command = plan

  variables {
    identity_center = {
      management_assignments = [{ permission_set = "organization_admin", user = "owner" }]
      member_assignments     = [{ account = "security", permission_set = "read_only", group = "-operators" }]
    }
  }

  expect_failures = [var.identity_center]
}

run "rejects_duplicate_assignment" {
  command = plan

  variables {
    identity_center = {
      management_assignments = [
        { permission_set = "organization_admin", user = "owner" },
        { permission_set = "organization_admin", user = "owner" },
      ]
    }
  }

  expect_failures = [var.identity_center]
}

run "rejects_duplicate_member_assignment" {
  command = plan

  variables {
    identity_center = {
      management_assignments = [{ permission_set = "organization_admin", user = "owner" }]
      member_assignments = [
        { account = "security", permission_set = "read_only", group = "operators" },
        { account = "security", permission_set = "read_only", group = "operators" },
      ]
    }
  }

  expect_failures = [var.identity_center]
}

run "rejects_key_alias_as_state_key" {
  command = plan

  variables {
    state_key_arn = "arn:aws:kms:ap-northeast-1:111111111111:alias/tabp-admin-tfstate"
  }

  expect_failures = [var.state_key_arn]
}

run "rejects_non_bucket_state_bucket" {
  command = plan

  variables {
    state_bucket_arn = "arn:aws:s3:::tabp-admin-tfstate/key"
  }

  expect_failures = [var.state_bucket_arn]
}

run "rejects_non_bucket_plan_output_bucket" {
  command = plan

  variables {
    plan_output_bucket_arn = "arn:aws:iam::111111111111:role/tabp"
  }

  expect_failures = [var.plan_output_bucket_arn]
}

run "rejects_non_bucket_apply_output_bucket" {
  command = plan

  variables {
    apply_output_bucket_arn = "tabp-admin-tfapply"
  }

  expect_failures = [var.apply_output_bucket_arn]
}

run "accepts_63_character_bucket_names" {
  command = plan

  variables {
    state_bucket_arn        = "arn:aws:s3:::${join("", [for i in range(63) : "a"])}"
    plan_output_bucket_arn  = "arn:aws:s3:::${join("", [for i in range(63) : "b"])}"
    apply_output_bucket_arn = "arn:aws:s3:::${join("", [for i in range(63) : "c"])}"
  }
}

run "rejects_64_character_state_bucket" {
  command = plan

  variables {
    state_bucket_arn = "arn:aws:s3:::${join("", [for i in range(64) : "a"])}"
  }

  expect_failures = [var.state_bucket_arn]
}

run "rejects_64_character_plan_output_bucket" {
  command = plan

  variables {
    plan_output_bucket_arn = "arn:aws:s3:::${join("", [for i in range(64) : "b"])}"
  }

  expect_failures = [var.plan_output_bucket_arn]
}

run "rejects_64_character_apply_output_bucket" {
  command = plan

  variables {
    apply_output_bucket_arn = "arn:aws:s3:::${join("", [for i in range(64) : "c"])}"
  }

  expect_failures = [var.apply_output_bucket_arn]
}

run "accepts_3_character_bucket_name" {
  command = plan

  variables {
    state_bucket_arn = "arn:aws:s3:::abc"
  }
}

run "rejects_2_character_bucket_name" {
  command = plan

  variables {
    state_bucket_arn = "arn:aws:s3:::ab"
  }

  expect_failures = [var.state_bucket_arn]
}

run "rejects_empty_bucket_name" {
  command = plan

  variables {
    state_bucket_arn = "arn:aws:s3:::"
  }

  expect_failures = [var.state_bucket_arn]
}
