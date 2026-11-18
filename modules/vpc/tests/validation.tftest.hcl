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
  name                    = "tabp-dev-network"
  cidr                    = "10.0.0.0/16"
  flow_logs_log_group_arn = "arn:aws:logs:ap-northeast-1:123456789012:log-group:/tabp/dev/network-foundation/flow-logs"
}

run "accepts_name_of_50_characters" {
  command = plan

  variables {
    name = join("", [for i in range(50) : "a"])
  }
}

run "accepts_name_with_inner_hyphen" {
  command = plan

  variables {
    name = "a-0"
  }
}

run "rejects_empty_name" {
  command = plan

  variables {
    name = ""
  }

  expect_failures = [var.name]
}

run "rejects_name_of_51_characters" {
  command = plan

  variables {
    name = join("", [for i in range(51) : "a"])
  }

  expect_failures = [var.name]
}

run "rejects_name_with_upper_case" {
  command = plan

  variables {
    name = "Tabp-dev"
  }

  expect_failures = [var.name]
}

run "rejects_name_starting_with_hyphen" {
  command = plan

  variables {
    name = "-tabp"
  }

  expect_failures = [var.name]
}

run "rejects_name_ending_with_hyphen" {
  command = plan

  variables {
    name = "tabp-"
  }

  expect_failures = [var.name]
}

run "accepts_cidr_of_16_in_10" {
  command = plan

  variables {
    cidr = "10.255.0.0/16"
  }
}

run "accepts_cidr_of_28_in_192_168" {
  command = plan

  variables {
    cidr = "192.168.255.240/28"
  }
}

run "accepts_cidr_in_172_16_outside_172_17" {
  command = plan

  variables {
    cidr = "172.31.0.0/16"
  }
}

run "rejects_cidr_that_is_not_a_cidr" {
  command = plan

  variables {
    cidr = "abc"
  }

  expect_failures = [var.cidr]
}

run "rejects_ipv6_cidr" {
  command = plan

  variables {
    cidr = "fd00::/56"
  }

  expect_failures = [var.cidr]
}

run "rejects_cidr_with_host_bits" {
  command = plan

  variables {
    cidr = "10.0.0.1/16"
  }

  expect_failures = [var.cidr]
}

run "rejects_cidr_larger_than_16" {
  command = plan

  variables {
    cidr = "10.0.0.0/15"
  }

  expect_failures = [var.cidr]
}

run "rejects_cidr_smaller_than_28" {
  command = plan

  variables {
    cidr = "10.0.0.0/29"
  }

  expect_failures = [var.cidr]
}

run "rejects_public_cidr" {
  command = plan

  variables {
    cidr = "8.8.0.0/16"
  }

  expect_failures = [var.cidr]
}

run "rejects_shared_address_space_cidr" {
  command = plan

  variables {
    cidr = "100.64.0.0/16"
  }

  expect_failures = [var.cidr]
}

run "rejects_cidr_of_172_17" {
  command = plan

  variables {
    cidr = "172.17.0.0/16"
  }

  expect_failures = [var.cidr]
}

run "rejects_cidr_inside_172_17" {
  command = plan

  variables {
    cidr = "172.17.5.0/24"
  }

  expect_failures = [var.cidr]
}

run "rejects_log_group_arn_with_wildcard_suffix" {
  command = plan

  variables {
    flow_logs_log_group_arn = "arn:aws:logs:ap-northeast-1:123456789012:log-group:/tabp/dev/network-foundation/flow-logs:*"
  }

  expect_failures = [var.flow_logs_log_group_arn]
}

run "rejects_arn_that_is_not_a_log_group" {
  command = plan

  variables {
    flow_logs_log_group_arn = "arn:aws:s3:::tabp-dev-flow-logs"
  }

  expect_failures = [var.flow_logs_log_group_arn]
}

run "rejects_log_group_in_another_region" {
  command = plan

  variables {
    flow_logs_log_group_arn = "arn:aws:logs:us-east-1:123456789012:log-group:/tabp/dev/network-foundation/flow-logs"
  }

  expect_failures = [aws_flow_log.this]
}

run "rejects_log_group_in_another_account" {
  command = plan

  variables {
    flow_logs_log_group_arn = "arn:aws:logs:ap-northeast-1:210987654321:log-group:/tabp/dev/network-foundation/flow-logs"
  }

  expect_failures = [aws_flow_log.this]
}

run "rejects_log_group_in_another_partition" {
  command = plan

  variables {
    flow_logs_log_group_arn = "arn:aws-cn:logs:ap-northeast-1:123456789012:log-group:/tabp/dev/network-foundation/flow-logs"
  }

  expect_failures = [aws_flow_log.this]
}
