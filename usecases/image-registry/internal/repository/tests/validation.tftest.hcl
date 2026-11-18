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
  name                     = "tabp-dev/api"
  publisher_role_name      = "tabp-dev-ecr-publish-api"
  kms_key_arn              = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  retained_images          = 30
  github_oidc_provider_arn = "arn:aws:iam::123456789012:oidc-provider/token.actions.githubusercontent.com"
  publisher = {
    owner         = "example"
    owner_id      = "1000001"
    repository    = "go-boilerplate"
    repository_id = "2000001"
    environment   = "dev-publish"
  }
}

run "rejects_single_retained_image" {
  command = plan

  variables {
    retained_images = 1
  }

  expect_failures = [var.retained_images]
}

run "rejects_fractional_retained_images" {
  command = plan

  variables {
    retained_images = 2.5
  }

  expect_failures = [var.retained_images]
}

run "rejects_wildcard_environment" {
  command = plan

  variables {
    publisher = {
      owner         = "example"
      owner_id      = "1000001"
      repository    = "go-boilerplate"
      repository_id = "2000001"
      environment   = "*"
    }
  }

  expect_failures = [var.publisher]
}

run "rejects_wildcard_repository" {
  command = plan

  variables {
    publisher = {
      owner         = "example"
      owner_id      = "1000001"
      repository    = "go-*"
      repository_id = "2000001"
      environment   = "dev-publish"
    }
  }

  expect_failures = [var.publisher]
}

run "rejects_non_numeric_ids" {
  command = plan

  variables {
    publisher = {
      owner         = "example"
      owner_id      = "example"
      repository    = "go-boilerplate"
      repository_id = "2000001"
      environment   = "dev-publish"
    }
  }

  expect_failures = [var.publisher]
}

run "rejects_alias_as_key" {
  command = plan

  variables {
    kms_key_arn = "arn:aws:kms:ap-northeast-1:123456789012:alias/tabp-dev-ecr"
  }

  expect_failures = [var.kms_key_arn]
}

run "rejects_name_without_prefix" {
  command = plan

  variables {
    name = "api"
  }

  expect_failures = [var.name]
}

run "rejects_long_role_name" {
  command = plan

  variables {
    publisher_role_name = "abcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcde"
  }

  expect_failures = [var.publisher_role_name]
}

run "rejects_owner_with_at_sign" {
  command = plan

  variables {
    publisher = {
      owner         = "ex@mple"
      owner_id      = "1000001"
      repository    = "go-boilerplate"
      repository_id = "2000001"
      environment   = "dev-publish"
    }
  }

  expect_failures = [var.publisher]
}

run "rejects_environment_with_colon" {
  command = plan

  variables {
    publisher = {
      owner         = "example"
      owner_id      = "1000001"
      repository    = "go-boilerplate"
      repository_id = "2000001"
      environment   = "dev:ref"
    }
  }

  expect_failures = [var.publisher]
}

run "rejects_zero_prefixed_repository_id" {
  command = plan

  variables {
    publisher = {
      owner         = "example"
      owner_id      = "1000001"
      repository    = "go-boilerplate"
      repository_id = "02000001"
      environment   = "dev-publish"
    }
  }

  expect_failures = [var.publisher]
}

run "rejects_retained_images_above_limit" {
  command = plan

  variables {
    retained_images = 10001
  }

  expect_failures = [var.retained_images]
}

run "accepts_role_name_at_limit" {
  command = plan

  variables {
    publisher_role_name = "abcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcd"
  }

  assert {
    condition     = length(var.publisher_role_name) == 64
    error_message = "64 文字の role 名が通りません。"
  }
}
