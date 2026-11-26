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
  project = "tabp"
}

run "accepts_38_character_project" {
  command = plan

  variables {
    project = "abcdefghijklmnopqrstuvwxyz012345678901"
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
    project = "abcdefghijklmnopqrstuvwxyz0123456789012"
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
