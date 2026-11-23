mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }
}

variables {
  name               = "tabp-sandbox-ops-notification-us-east-1"
  heartbeat_role_arn = "arn:aws:iam::123456789012:role/tabp-sandbox-ops-heartbeat"
}

run "rejects_name_with_slash" {
  command = plan

  variables {
    name = "tabp/sandbox"
  }

  expect_failures = [var.name]
}

run "accepts_256_character_name" {
  command = plan

  variables {
    name = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  }
}

run "rejects_257_character_name" {
  command = plan

  variables {
    name = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  }

  expect_failures = [var.name]
}

run "rejects_empty_name" {
  command = plan

  variables {
    name = ""
  }

  expect_failures = [var.name]
}

run "rejects_policy_arn_as_role" {
  command = plan

  variables {
    heartbeat_role_arn = "arn:aws:iam::123456789012:policy/tabp-sandbox-ops-heartbeat"
  }

  expect_failures = [var.heartbeat_role_arn]
}

run "rejects_role_in_another_account" {
  command = plan

  variables {
    heartbeat_role_arn = "arn:aws:iam::210987654321:role/tabp-sandbox-ops-heartbeat"
  }

  expect_failures = [aws_kms_key_policy.this]
}

# us-east-1 は aws の partition にしか無い（ops-notification/ADR-0001 決定1）。
run "rejects_role_in_another_partition" {
  command = plan

  variables {
    heartbeat_role_arn = "arn:aws-us-gov:iam::123456789012:role/tabp-sandbox-ops-heartbeat"
  }

  expect_failures = [aws_kms_key_policy.this]
}

run "rejects_partition_outside_aws" {
  command = plan

  override_data {
    target = data.aws_partition.current
    values = { partition = "aws-us-gov" }
  }

  variables {
    heartbeat_role_arn = "arn:aws-us-gov:iam::123456789012:role/tabp-sandbox-ops-heartbeat"
  }

  expect_failures = [aws_kms_key.this]
}
