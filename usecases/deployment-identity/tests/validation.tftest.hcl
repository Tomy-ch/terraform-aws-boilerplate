mock_provider "aws" {}

variables {
  project          = "tabp"
  env              = "admin"
  github           = { owner = "octo-org", owner_id = "123456", repository = "octo-repo", repository_id = "456789" }
  state_bucket_arn = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an"
}

run "accepts_valid_inputs" {
  command = plan

  assert {
    condition     = module.github_oidc != null
    error_message = "正しい入力で plan が通っていません。"
  }
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
