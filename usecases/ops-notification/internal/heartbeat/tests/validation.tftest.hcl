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
  name                     = "tabp-sandbox-ops-heartbeat"
  env                      = "sandbox"
  topic_arn                = "arn:aws:sns:ap-northeast-1:123456789012:tabp-sandbox-ops-notification"
  permissions_boundary_arn = "arn:aws:iam::123456789012:policy/tabp-sandbox-workload-boundary"
}

run "accepts_64_character_name" {
  command = plan

  variables {
    name = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  }
}

run "rejects_65_character_name" {
  command = plan

  variables {
    name = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  }

  expect_failures = [var.name]
}

run "rejects_name_with_slash" {
  command = plan

  variables {
    name = "tabp/sandbox"
  }

  expect_failures = [var.name]
}

run "rejects_empty_env" {
  command = plan

  variables {
    env = ""
  }

  expect_failures = [var.env]
}

run "rejects_topic_name_instead_of_arn" {
  command = plan

  variables {
    topic_arn = "tabp-sandbox-ops-notification"
  }

  expect_failures = [var.topic_arn]
}

run "rejects_fifo_topic" {
  command = plan

  variables {
    topic_arn = "arn:aws:sns:ap-northeast-1:123456789012:tabp-sandbox-ops-notification.fifo"
  }

  expect_failures = [var.topic_arn]
}

run "rejects_topic_in_another_account" {
  command = plan

  variables {
    topic_arn = "arn:aws:sns:ap-northeast-1:210987654321:tabp-sandbox-ops-notification"
  }

  expect_failures = [aws_iam_role_policy.this]
}

run "rejects_topic_in_another_region" {
  command = plan

  variables {
    topic_arn = "arn:aws:sns:us-east-1:123456789012:tabp-sandbox-ops-notification"
  }

  expect_failures = [aws_iam_role_policy.this]
}

run "rejects_aws_managed_boundary" {
  command = plan

  variables {
    permissions_boundary_arn = "arn:aws:iam::aws:policy/ReadOnlyAccess"
  }

  expect_failures = [var.permissions_boundary_arn]
}

run "rejects_role_arn_as_boundary" {
  command = plan

  variables {
    permissions_boundary_arn = "arn:aws:iam::123456789012:role/tabp-sandbox-workload-boundary"
  }

  expect_failures = [var.permissions_boundary_arn]
}

run "rejects_boundary_in_another_account" {
  command = plan

  variables {
    permissions_boundary_arn = "arn:aws:iam::210987654321:policy/tabp-sandbox-workload-boundary"
  }

  expect_failures = [aws_iam_role.this]
}
