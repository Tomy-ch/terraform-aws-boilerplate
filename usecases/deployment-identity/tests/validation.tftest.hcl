mock_provider "aws" {
  # state_key_arn と access_log_* の precondition は、ARN を data source の partition・region・account と比べる。
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
  mock_data "aws_region" {
    defaults = { region = "ap-northeast-1" }
  }
  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }
}

variables {
  project                = "tabp"
  env                    = "admin"
  github                 = { owner = "octo-org", owner_id = "123456", repository = "octo-repo", repository_id = "456789" }
  state_bucket_arn       = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an"
  state_key_arn          = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  plan_output_bucket_arn = "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an"

  access_log_group_arn       = "arn:aws:logs:ap-northeast-1:123456789012:log-group:tabp-admin-state-backend-access"
  access_log_destination_arn = "arn:aws:logs:ap-northeast-1:123456789012:delivery-destination:tabp-admin-state-backend-access"
  access_log_source_arns = [
    "arn:aws:logs:ap-northeast-1:123456789012:delivery-source:tabp-admin-tfstate",
    "arn:aws:logs:ap-northeast-1:123456789012:delivery-source:tabp-admin-tfplan",
  ]
}

run "accepts_boundary_inputs" {
  command = plan

  variables {
    project                = "abcdefghijklmnopqrstuvwxyz01234567890"
    env                    = "sandbox"
    github                 = { owner = "abbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbc", owner_id = "1", repository = "rrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrr", repository_id = "1" }
    state_bucket_arn       = "arn:aws-cn:s3:::tabp-sandbox-tfstate-123456789012-cn-north-1-an"
    plan_output_bucket_arn = "arn:aws-cn:s3:::tabp-sandbox-tfplan-123456789012-cn-north-1-an"
  }

  assert {
    condition     = module.github_oidc.roles.unlock_identity.name == "abcdefghijklmnopqrstuvwxyz01234567890-sandbox-ci-unlock-identity"
    error_message = "project が37文字・env が sandbox・owner が39文字・repository が100文字の入力で、64文字の role 名が組まれていません。"
  }
}

run "accepts_dev" {
  command = plan

  variables {
    env = "dev"
  }
}

run "accepts_stg" {
  command = plan

  variables {
    env = "stg"
  }
}

run "accepts_prd" {
  command = plan

  variables {
    env = "prd"
  }
}

run "rejects_project_over_37_chars" {
  command = plan

  variables {
    project = "abcdefghijklmnopqrstuvwxyz012345678901"
  }

  expect_failures = [var.project]
}

run "rejects_owner_over_39_chars" {
  command = plan

  variables {
    github = { owner = "abbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbcd", owner_id = "123456", repository = "octo-repo", repository_id = "456789" }
  }

  expect_failures = [var.github]
}

run "rejects_repository_over_100_chars" {
  command = plan

  variables {
    github = { owner = "octo-org", owner_id = "123456", repository = "rrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrr", repository_id = "456789" }
  }

  expect_failures = [var.github]
}

run "rejects_wildcard_in_repository" {
  command = plan

  variables {
    github = { owner = "octo-org", owner_id = "123456", repository = "octo-*", repository_id = "456789" }
  }

  expect_failures = [var.github]
}

run "rejects_colon_in_owner" {
  command = plan

  variables {
    github = { owner = "octo:org", owner_id = "123456", repository = "octo-repo", repository_id = "456789" }
  }

  expect_failures = [var.github]
}

run "rejects_slash_in_repository" {
  command = plan

  variables {
    github = { owner = "octo-org", owner_id = "123456", repository = "octo/repo", repository_id = "456789" }
  }

  expect_failures = [var.github]
}

run "rejects_at_sign_in_owner" {
  command = plan

  variables {
    github = { owner = "octo@org", owner_id = "123456", repository = "octo-repo", repository_id = "456789" }
  }

  expect_failures = [var.github]
}

run "rejects_trailing_hyphen_in_owner" {
  command = plan

  variables {
    github = { owner = "octo-", owner_id = "123456", repository = "octo-repo", repository_id = "456789" }
  }

  expect_failures = [var.github]
}

run "rejects_empty_owner" {
  command = plan

  variables {
    github = { owner = "", owner_id = "123456", repository = "octo-repo", repository_id = "456789" }
  }

  expect_failures = [var.github]
}

run "rejects_leading_zero_repository_id" {
  command = plan

  variables {
    github = { owner = "octo-org", owner_id = "123456", repository = "octo-repo", repository_id = "0456789" }
  }

  expect_failures = [var.github]
}

run "rejects_non_s3_arn" {
  command = plan

  variables {
    state_bucket_arn = "arn:aws:iam::123456789012:role/x"
  }

  expect_failures = [var.state_bucket_arn]
}

run "rejects_trailing_slash_in_bucket_arn" {
  command = plan

  variables {
    state_bucket_arn = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/"
  }

  expect_failures = [var.state_bucket_arn]
}

run "rejects_non_s3_plan_output_arn" {
  command = plan

  variables {
    plan_output_bucket_arn = "arn:aws:iam::123456789012:role/x"
  }

  expect_failures = [var.plan_output_bucket_arn]
}

run "rejects_object_arn_as_plan_output_bucket" {
  command = plan

  variables {
    plan_output_bucket_arn = "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an/pr/*"
  }

  expect_failures = [var.plan_output_bucket_arn]
}

run "rejects_wildcard_in_owner" {
  command = plan

  variables {
    github = { owner = "octo-*", owner_id = "123456", repository = "octo-repo", repository_id = "456789" }
  }

  expect_failures = [var.github]
}

run "rejects_colon_in_repository" {
  command = plan

  variables {
    github = { owner = "octo-org", owner_id = "123456", repository = "octo:repo", repository_id = "456789" }
  }

  expect_failures = [var.github]
}

run "rejects_question_mark_in_repository" {
  command = plan

  variables {
    github = { owner = "octo-org", owner_id = "123456", repository = "octo?repo", repository_id = "456789" }
  }

  expect_failures = [var.github]
}

run "rejects_non_numeric_owner_id" {
  command = plan

  variables {
    github = { owner = "octo-org", owner_id = "octo-org", repository = "octo-repo", repository_id = "456789" }
  }

  expect_failures = [var.github]
}

run "rejects_empty_repository_id" {
  command = plan

  variables {
    github = { owner = "octo-org", owner_id = "123456", repository = "octo-repo", repository_id = "" }
  }

  expect_failures = [var.github]
}

run "rejects_leading_zero_id" {
  command = plan

  variables {
    github = { owner = "octo-org", owner_id = "0123456", repository = "octo-repo", repository_id = "456789" }
  }

  expect_failures = [var.github]
}

run "rejects_bucket_object_arn" {
  command = plan

  variables {
    state_bucket_arn = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/*"
  }

  expect_failures = [var.state_bucket_arn]
}

run "rejects_unknown_env" {
  command = plan

  variables {
    env = "production"
  }

  expect_failures = [var.env]
}

run "rejects_hyphen_in_project" {
  command = plan

  variables {
    project = "tabp-x"
  }

  expect_failures = [var.project]
}

run "accepts_key_arn_in_other_partition" {
  command = plan

  variables {
    state_key_arn              = "arn:aws-us-gov:kms:us-gov-west-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
    access_log_group_arn       = "arn:aws-us-gov:logs:us-gov-west-1:123456789012:log-group:tabp-admin-state-backend-access"
    access_log_destination_arn = "arn:aws-us-gov:logs:us-gov-west-1:123456789012:delivery-destination:tabp-admin-state-backend-access"
    access_log_source_arns = [
      "arn:aws-us-gov:logs:us-gov-west-1:123456789012:delivery-source:tabp-admin-tfstate",
      "arn:aws-us-gov:logs:us-gov-west-1:123456789012:delivery-source:tabp-admin-tfplan",
    ]
  }

  override_data {
    target = module.github_oidc.data.aws_partition.current
    values = { partition = "aws-us-gov" }
  }
  override_data {
    target = module.github_oidc.data.aws_region.current
    values = { region = "us-gov-west-1" }
  }
}

run "rejects_alias_arn" {
  command = plan

  variables {
    state_key_arn = "arn:aws:kms:ap-northeast-1:123456789012:alias/tabp-admin-tfstate"
  }

  expect_failures = [var.state_key_arn]
}

run "rejects_key_wildcard" {
  command = plan

  variables {
    state_key_arn = "arn:aws:kms:ap-northeast-1:123456789012:key/*"
  }

  expect_failures = [var.state_key_arn]
}

run "rejects_multi_region_key" {
  command = plan

  variables {
    state_key_arn = "arn:aws:kms:ap-northeast-1:123456789012:key/mrk-1234abcd12ab34cd56ef1234567890ab"
  }

  expect_failures = [var.state_key_arn]
}

run "rejects_bare_key_id" {
  command = plan

  variables {
    state_key_arn = "00000000-0000-0000-0000-000000000000"
  }

  expect_failures = [var.state_key_arn]
}

run "rejects_bucket_arn_as_key" {
  command = plan

  variables {
    state_key_arn = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an"
  }

  expect_failures = [var.state_key_arn]
}

run "rejects_empty_key_arn" {
  command = plan

  variables {
    state_key_arn = ""
  }

  expect_failures = [var.state_key_arn]
}

# 名前の上限は delivery destination の 60 文字（modules/vended-log-group の name）。
run "accepts_access_log_name_of_60_chars" {
  command = plan

  variables {
    access_log_group_arn       = "arn:aws:logs:ap-northeast-1:123456789012:log-group:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    access_log_destination_arn = "arn:aws:logs:ap-northeast-1:123456789012:delivery-destination:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    access_log_source_arns     = ["arn:aws:logs:ap-northeast-1:123456789012:delivery-source:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"]
  }
}

run "rejects_access_log_name_of_61_chars" {
  command = plan

  variables {
    access_log_group_arn = "arn:aws:logs:ap-northeast-1:123456789012:log-group:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  }

  expect_failures = [var.access_log_group_arn]
}

# Action ごとの :* の有無は IAM の側が付ける。
run "rejects_access_log_group_arn_with_stream_wildcard" {
  command = plan

  variables {
    access_log_group_arn = "arn:aws:logs:ap-northeast-1:123456789012:log-group:tabp-admin-state-backend-access:*"
  }

  expect_failures = [var.access_log_group_arn]
}

run "rejects_destination_arn_as_access_log_group" {
  command = plan

  variables {
    access_log_group_arn = "arn:aws:logs:ap-northeast-1:123456789012:delivery-destination:tabp-admin-state-backend-access"
  }

  expect_failures = [var.access_log_group_arn]
}

run "rejects_access_log_group_arn_with_slash" {
  command = plan

  variables {
    access_log_group_arn = "arn:aws:logs:ap-northeast-1:123456789012:log-group:/aws/vendedlogs/x"
  }

  expect_failures = [var.access_log_group_arn]
}

run "rejects_log_group_arn_as_destination" {
  command = plan

  variables {
    access_log_destination_arn = "arn:aws:logs:ap-northeast-1:123456789012:log-group:tabp-admin-state-backend-access"
  }

  expect_failures = [var.access_log_destination_arn]
}

run "rejects_empty_access_log_source_arns" {
  command = plan

  variables {
    access_log_source_arns = []
  }

  expect_failures = [var.access_log_source_arns]
}

run "rejects_destination_arn_as_source" {
  command = plan

  variables {
    access_log_source_arns = ["arn:aws:logs:ap-northeast-1:123456789012:delivery-destination:tabp-admin-tfstate"]
  }

  expect_failures = [var.access_log_source_arns]
}

run "rejects_duplicate_access_log_source_arns" {
  command = plan

  variables {
    access_log_source_arns = ["arn:aws:logs:ap-northeast-1:123456789012:delivery-source:tabp-admin-tfstate", "arn:aws:logs:ap-northeast-1:123456789012:delivery-source:tabp-admin-tfstate"]
  }

  expect_failures = [var.access_log_source_arns]
}
