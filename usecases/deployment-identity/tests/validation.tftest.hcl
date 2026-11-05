mock_provider "aws" {}

variables {
  project                = "tabp"
  env                    = "admin"
  github                 = { owner = "octo-org", owner_id = "123456", repository = "octo-repo", repository_id = "456789" }
  state_bucket_arn       = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an"
  plan_output_bucket_arn = "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an"
}

run "accepts_boundary_inputs" {
  command = plan

  variables {
    project                = "abcdefghijklmnopqrstuvwxyz012345678901"
    env                    = "sandbox"
    github                 = { owner = "abbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbc", owner_id = "1", repository = "rrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrr", repository_id = "1" }
    state_bucket_arn       = "arn:aws-cn:s3:::tabp-sandbox-tfstate-123456789012-cn-north-1-an"
    plan_output_bucket_arn = "arn:aws-cn:s3:::tabp-sandbox-tfplan-123456789012-cn-north-1-an"
  }

  assert {
    condition     = module.github_oidc.roles.apply_identity.name == "abcdefghijklmnopqrstuvwxyz012345678901-sandbox-ci-apply-identity"
    error_message = "project が38文字・env が sandbox・owner が39文字・repository が100文字の入力で、64文字の role 名が組まれていません。"
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

run "rejects_project_over_38_chars" {
  command = plan

  variables {
    project = "abcdefghijklmnopqrstuvwxyz0123456789012"
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
