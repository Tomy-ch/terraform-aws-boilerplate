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
  project                   = "tabp"
  env                       = "dev"
  github_oidc_provider_arn  = "arn:aws:iam::123456789012:oidc-provider/token.actions.githubusercontent.com"
  registry_manager_role_arn = "arn:aws:iam::123456789012:role/tabp-dev-ci-apply"
  repositories = {
    api = {
      retained_images = 30
      publisher = {
        owner         = "example"
        owner_id      = "1000001"
        repository    = "go-boilerplate"
        repository_id = "2000001"
        environment   = "dev-publish"
      }
    }
  }
}

run "rejects_admin_env" {
  command = plan

  variables {
    env = "admin"
  }

  expect_failures = [var.env]
}

run "rejects_uppercase_project" {
  command = plan

  variables {
    project = "Tabp"
  }

  expect_failures = [var.project]
}

run "rejects_empty_repositories" {
  command = plan

  variables {
    repositories = {}
  }

  expect_failures = [var.repositories]
}

run "rejects_repository_key_with_slash" {
  command = plan

  variables {
    repositories = {
      "a/b" = {
        retained_images = 30
        publisher = {
          owner         = "example"
          owner_id      = "1000001"
          repository    = "go-boilerplate"
          repository_id = "2000001"
          environment   = "dev-publish"
        }
      }
    }
  }

  expect_failures = [var.repositories]
}

run "rejects_provider_of_other_issuer" {
  command = plan

  variables {
    github_oidc_provider_arn = "arn:aws:iam::123456789012:oidc-provider/example.com"
  }

  expect_failures = [var.github_oidc_provider_arn]
}

run "rejects_manager_user_arn" {
  command = plan

  variables {
    registry_manager_role_arn = "arn:aws:iam::123456789012:user/admin"
  }

  expect_failures = [var.registry_manager_role_arn]
}
