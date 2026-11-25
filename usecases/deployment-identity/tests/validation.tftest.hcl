mock_provider "aws" {
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
  project                      = "tabp"
  env                          = "admin"
  github                       = { owner = "octo-org", owner_id = "123456", repository = "octo-repo", repository_id = "456789" }
  state_bucket_arn             = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an"
  state_key_arn                = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  plan_output_bucket_arn       = "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an"
  apply_output_bucket_arn      = "arn:aws:s3:::tabp-admin-tfapply-123456789012-ap-northeast-1-an"
  break_glass_arns             = ["arn:aws:iam::123456789012:user/break-glass/break-glass-1", "arn:aws:iam::123456789012:user/break-glass/break-glass-2", "arn:aws:iam::123456789012:group/break-glass/break-glass", "arn:aws:iam::123456789012:role/break-glass/tabp-break-glass"]
  bootstrap_read_role          = { name = "tabp-bootstrap-read", path = "/bootstrap/" }
  bootstrap_member_account_ids = { dev = "111111111111" }
}

run "accepts_boundary_inputs" {
  command = plan

  variables {
    project                      = "abcdefghijklmnopqrstuvwxyz0123456"
    env                          = "log-archive"
    bootstrap_read_role          = null
    bootstrap_member_account_ids = {}
    github                       = { owner = "abbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbc", owner_id = "1", repository = "rrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrr", repository_id = "1" }
    state_bucket_arn             = "arn:aws-cn:s3:::tabp-log-archive-tfstate-123456789012-cn-north-1-an"
    plan_output_bucket_arn       = "arn:aws-cn:s3:::tabp-log-archive-tfplan-123456789012-cn-north-1-an"
    apply_output_bucket_arn      = "arn:aws-cn:s3:::tabp-log-archive-tfapply-123456789012-cn-north-1-an"
  }

  assert {
    condition     = module.github_oidc.roles.unlock_identity.name == "abcdefghijklmnopqrstuvwxyz0123456-log-archive-ci-unlock-identity"
    error_message = "project が33文字・env が log-archive・owner が39文字・repository が100文字の入力で、64文字の role 名が組まれていません。"
  }
}

run "accepts_dev" {
  command = plan

  variables {
    env                          = "dev"
    bootstrap_read_role          = null
    bootstrap_member_account_ids = {}
  }
}

run "accepts_stg" {
  command = plan

  variables {
    env                          = "stg"
    bootstrap_read_role          = null
    bootstrap_member_account_ids = {}
  }
}

run "accepts_prd" {
  command = plan

  variables {
    env                          = "prd"
    bootstrap_read_role          = null
    bootstrap_member_account_ids = {}
  }
}

run "rejects_project_over_33_chars" {
  command = plan

  variables {
    project = "abcdefghijklmnopqrstuvwxyz01234567"
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

run "rejects_non_s3_apply_output_arn" {
  command = plan

  variables {
    apply_output_bucket_arn = "arn:aws:iam::123456789012:role/x"
  }

  expect_failures = [var.apply_output_bucket_arn]
}

run "rejects_object_arn_as_apply_output_bucket" {
  command = plan

  variables {
    apply_output_bucket_arn = "arn:aws:s3:::tabp-admin-tfapply-123456789012-ap-northeast-1-an/*"
  }

  expect_failures = [var.apply_output_bucket_arn]
}

# 名前を他のアカウントが先に作れる global の namespace の Bucket に、書き込みと作成の権限を与えない。
run "rejects_global_namespace_plan_output_bucket" {
  command = plan

  variables {
    plan_output_bucket_arn = "arn:aws:s3:::tabp-admin-tfplan"
  }

  expect_failures = [var.plan_output_bucket_arn]
}

run "rejects_global_namespace_apply_output_bucket" {
  command = plan

  variables {
    apply_output_bucket_arn = "arn:aws:s3:::tabp-admin-tfapply"
  }

  expect_failures = [var.apply_output_bucket_arn]
}

run "rejects_empty_apply_output_bucket_arn" {
  command = plan

  variables {
    apply_output_bucket_arn = ""
  }

  expect_failures = [var.apply_output_bucket_arn]
}

run "rejects_apply_output_bucket_account_11_digits" {
  command = plan

  variables {
    apply_output_bucket_arn = "arn:aws:s3:::tabp-admin-tfapply-12345678901-ap-northeast-1-an"
  }

  expect_failures = [var.apply_output_bucket_arn]
}

run "rejects_apply_output_bucket_account_13_digits" {
  command = plan

  variables {
    apply_output_bucket_arn = "arn:aws:s3:::tabp-admin-tfapply-1234567890123-ap-northeast-1-an"
  }

  expect_failures = [var.apply_output_bucket_arn]
}

run "rejects_an_suffix_without_account_and_region" {
  command = plan

  variables {
    apply_output_bucket_arn = "arn:aws:s3:::tabp-admin-tfapply-an"
  }

  expect_failures = [var.apply_output_bucket_arn]
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

run "accepts_security" {
  command = plan

  variables {
    env                          = "security"
    bootstrap_read_role          = null
    bootstrap_member_account_ids = {}
  }

  assert {
    condition     = module.github_oidc.roles.apply_identity == { name = "tabp-security-ci-apply-identity", environment = "security-apply-identity" }
    error_message = "env が security の role と environment の名前になっていません。"
  }
}

run "accepts_log_archive" {
  command = plan

  variables {
    env                          = "log-archive"
    bootstrap_read_role          = null
    bootstrap_member_account_ids = {}
  }

  assert {
    condition     = module.github_oidc.roles.apply_identity == { name = "tabp-log-archive-ci-apply-identity", environment = "log-archive-apply-identity" }
    error_message = "env が log-archive の role と environment の名前になっていません。"
  }
}

run "accepts_sandbox" {
  command = plan

  variables {
    env                          = "sandbox"
    bootstrap_read_role          = null
    bootstrap_member_account_ids = {}
  }

  assert {
    condition     = module.github_oidc.roles.apply_identity == { name = "tabp-sandbox-ci-apply-identity", environment = "sandbox-apply-identity" }
    error_message = "env が sandbox の role と environment の名前になっていません。"
  }
}

run "accepts_break_glass_names_at_iam_limit" {
  command = plan

  variables {
    break_glass_arns = [
      format("arn:aws:iam::123456789012:user/break-glass/%064d", 1),
      format("arn:aws:iam::123456789012:user/break-glass/%064d", 2),
      format("arn:aws:iam::123456789012:group/break-glass/%0128d", 3),
      format("arn:aws:iam::123456789012:role/break-glass/r%063d", 4),
    ]
  }
}

run "rejects_break_glass_user_name_over_64" {
  command = plan

  variables {
    break_glass_arns = [format("arn:aws:iam::123456789012:user/break-glass/%065d", 1)]
  }

  expect_failures = [var.break_glass_arns]
}

run "rejects_break_glass_role_name_over_64" {
  command = plan

  variables {
    break_glass_arns = [format("arn:aws:iam::123456789012:role/break-glass/%065d", 1)]
  }

  expect_failures = [var.break_glass_arns]
}

run "rejects_break_glass_group_name_over_128" {
  command = plan

  variables {
    break_glass_arns = [format("arn:aws:iam::123456789012:group/break-glass/%0129d", 1)]
  }

  expect_failures = [var.break_glass_arns]
}

run "rejects_five_break_glass_arns" {
  command = plan

  variables {
    break_glass_arns = [
      "arn:aws:iam::123456789012:user/break-glass/break-glass-1",
      "arn:aws:iam::123456789012:user/break-glass/break-glass-2",
      "arn:aws:iam::123456789012:user/break-glass/break-glass-3",
      "arn:aws:iam::123456789012:group/break-glass/break-glass",
      "arn:aws:iam::123456789012:role/break-glass/tabp-break-glass",
    ]
  }

  expect_failures = [var.break_glass_arns]
}

run "rejects_valid_and_invalid_break_glass_arns_mixed" {
  command = plan

  variables {
    break_glass_arns = [
      "arn:aws:iam::123456789012:user/break-glass/break-glass-1",
      "arn:aws:iam::123456789012:group/break-glass/break-glass",
      "arn:aws:iam::123456789012:role/tabp-break-glass",
    ]
  }

  expect_failures = [var.break_glass_arns]
}

run "rejects_iam_policy_arn_as_break_glass" {
  command = plan

  variables {
    break_glass_arns = ["arn:aws:iam::123456789012:policy/break-glass/break-glass"]
  }

  expect_failures = [var.break_glass_arns]
}

run "rejects_empty_break_glass_arns" {
  command = plan

  variables {
    break_glass_arns = []
  }

  expect_failures = [var.break_glass_arns]
}

run "rejects_break_glass_arn_outside_path" {
  command = plan

  variables {
    break_glass_arns = ["arn:aws:iam::123456789012:role/tabp-break-glass"]
  }

  expect_failures = [var.break_glass_arns]
}

run "rejects_non_iam_break_glass_arn" {
  command = plan

  variables {
    break_glass_arns = ["arn:aws:s3:::break-glass/break-glass"]
  }

  expect_failures = [var.break_glass_arns]
}

run "rejects_wildcard_in_break_glass_arn" {
  command = plan

  variables {
    break_glass_arns = ["arn:aws:iam::123456789012:user/break-glass/*"]
  }

  expect_failures = [var.break_glass_arns]
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
    break_glass_arns = [
      "arn:aws-us-gov:iam::123456789012:user/break-glass/break-glass-1",
      "arn:aws-us-gov:iam::123456789012:group/break-glass/break-glass",
      "arn:aws-us-gov:iam::123456789012:role/break-glass/tabp-break-glass",
    ]
    state_key_arn = "arn:aws-us-gov:kms:us-gov-west-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
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

run "accepts_bootstrap_read_role_at_limits" {
  command = plan

  variables {
    bootstrap_read_role = { name = format("b%063d", 1), path = format("/%064d/", 2) }
  }
}

run "rejects_bootstrap_read_role_name_with_underscore" {
  command = plan

  variables {
    bootstrap_read_role = { name = "tabp_bootstrap_read", path = "/bootstrap/" }
  }

  expect_failures = [var.bootstrap_read_role]
}

run "rejects_bootstrap_read_role_name_over_64_chars" {
  command = plan

  variables {
    bootstrap_read_role = { name = format("b%064d", 1), path = "/bootstrap/" }
  }

  expect_failures = [var.bootstrap_read_role]
}

run "rejects_bootstrap_read_role_nested_path" {
  command = plan

  variables {
    bootstrap_read_role = { name = "tabp-bootstrap-read", path = "/bootstrap/read/" }
  }

  expect_failures = [var.bootstrap_read_role]
}

run "rejects_bootstrap_read_role_root_path" {
  command = plan

  variables {
    bootstrap_read_role = { name = "tabp-bootstrap-read", path = "/" }
  }

  expect_failures = [var.bootstrap_read_role]
}

run "rejects_bootstrap_account_id_11_digits" {
  command = plan

  variables {
    bootstrap_member_account_ids = { dev = "11111111111" }
  }

  expect_failures = [var.bootstrap_member_account_ids]
}

run "rejects_bootstrap_account_for_admin" {
  command = plan

  variables {
    bootstrap_member_account_ids = { admin = "111111111111" }
  }

  expect_failures = [var.bootstrap_member_account_ids]
}

run "rejects_bootstrap_read_role_name_leading_digit" {
  command = plan

  variables {
    bootstrap_read_role = { name = "1tabp-bootstrap-read", path = "/bootstrap/" }
  }

  expect_failures = [var.bootstrap_read_role]
}

run "rejects_bootstrap_read_role_path_over_64_chars" {
  command = plan

  variables {
    bootstrap_read_role = { name = "tabp-bootstrap-read", path = format("/%065d/", 1) }
  }

  expect_failures = [var.bootstrap_read_role]
}

run "rejects_bootstrap_read_role_path_without_trailing_slash" {
  command = plan

  variables {
    bootstrap_read_role = { name = "tabp-bootstrap-read", path = "/bootstrap" }
  }

  expect_failures = [var.bootstrap_read_role]
}

run "rejects_bootstrap_account_id_13_digits" {
  command = plan

  variables {
    bootstrap_member_account_ids = { dev = "1111111111111" }
  }

  expect_failures = [var.bootstrap_member_account_ids]
}

run "rejects_bootstrap_account_id_non_digit" {
  command = plan

  variables {
    bootstrap_member_account_ids = { dev = "11111111111a" }
  }

  expect_failures = [var.bootstrap_member_account_ids]
}

run "rejects_bootstrap_account_unknown_env" {
  command = plan

  variables {
    bootstrap_member_account_ids = { prod = "111111111111" }
  }

  expect_failures = [var.bootstrap_member_account_ids]
}
