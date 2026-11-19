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
  mock_data "aws_availability_zones" {
    defaults = { zone_ids = ["apne1-az1", "apne1-az2", "apne1-az4"] }
  }
}

# modules/log-group の kms_key_arn の validation と、modules/vpc の flow_logs_log_group_arn の validation を通すため、ARN を形のある値にする。
override_resource {
  target = module.kms.aws_kms_key.this
  values = { arn = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab" }
}

override_resource {
  target = module.flow_logs.aws_cloudwatch_log_group.this
  values = { arn = "arn:aws:logs:ap-northeast-1:123456789012:log-group:/tabp/dev/network-foundation/vpc-flow-logs" }
}

override_resource {
  target = module.vpc.aws_iam_role.flow_logs
  values = { arn = "arn:aws:iam::123456789012:role/tabp-dev-vpc-flow-logs" }
}

variables {
  project                       = "tabp"
  env                           = "dev"
  cidr                          = "10.0.0.0/16"
  availability_zone_ids         = ["apne1-az1", "apne1-az2", "apne1-az4"]
  flow_logs_retention_days      = 365
  flow_logs_deletion_protection = true
}

run "accepts_project_of_42_characters" {
  command = plan

  variables {
    project = join("", [for i in range(42) : "a"])
  }
}

run "rejects_project_of_43_characters" {
  command = plan

  variables {
    project = join("", [for i in range(43) : "a"])
  }

  expect_failures = [var.project]
}

run "rejects_project_with_hyphen" {
  command = plan

  variables {
    project = "ta-bp"
  }

  expect_failures = [var.project]
}

run "rejects_empty_project" {
  command = plan

  variables {
    project = ""
  }

  expect_failures = [var.project]
}

run "accepts_env_stg" {
  command = plan

  variables {
    env = "stg"
  }
}

run "accepts_env_prd" {
  command = plan

  variables {
    env = "prd"
  }
}

run "accepts_env_sandbox" {
  command = plan

  variables {
    env = "sandbox"
  }
}

run "rejects_env_admin" {
  command = plan

  variables {
    env = "admin"
  }

  expect_failures = [var.env]
}

run "rejects_env_log_archive" {
  command = plan

  variables {
    env = "log-archive"
  }

  expect_failures = [var.env]
}

run "accepts_cidr_of_24" {
  command = plan

  variables {
    cidr = "172.31.255.0/24"
  }
}

run "rejects_cidr_of_25" {
  command = plan

  variables {
    cidr = "10.0.0.0/25"
  }

  expect_failures = [var.cidr]
}

run "rejects_cidr_of_15" {
  command = plan

  variables {
    cidr = "10.0.0.0/15"
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

run "rejects_public_cidr" {
  command = plan

  variables {
    cidr = "8.8.0.0/16"
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

run "rejects_cidr_that_is_not_a_cidr" {
  command = plan

  variables {
    cidr = "abc"
  }

  expect_failures = [var.cidr]
}

run "accepts_two_availability_zones" {
  command = plan

  variables {
    availability_zone_ids = ["apne1-az1", "apne1-az2"]
  }
}

run "rejects_one_availability_zone" {
  command = plan

  variables {
    availability_zone_ids = ["apne1-az1"]
  }

  expect_failures = [var.availability_zone_ids]
}

run "rejects_five_availability_zones" {
  command = plan

  variables {
    availability_zone_ids = ["apne1-az1", "apne1-az2", "apne1-az3", "apne1-az4", "apne1-az5"]
  }

  expect_failures = [var.availability_zone_ids]
}

run "rejects_duplicate_availability_zones" {
  command = plan

  variables {
    availability_zone_ids = ["apne1-az1", "apne1-az1"]
  }

  expect_failures = [var.availability_zone_ids]
}

run "rejects_availability_zone_name" {
  command = plan

  variables {
    availability_zone_ids = ["ap-northeast-1a", "ap-northeast-1c"]
  }

  expect_failures = [var.availability_zone_ids]
}

run "accepts_retention_of_3653_days" {
  command = plan

  variables {
    flow_logs_retention_days = 3653
  }
}

run "rejects_retention_of_364_days" {
  command = plan

  variables {
    flow_logs_retention_days = 364
  }

  expect_failures = [var.flow_logs_retention_days]
}

run "rejects_unlimited_retention" {
  command = plan

  variables {
    flow_logs_retention_days = 0
  }

  expect_failures = [var.flow_logs_retention_days]
}

run "accepts_project_of_42_characters_with_sandbox" {
  command = plan

  variables {
    project = join("", [for i in range(42) : "a"])
    env     = "sandbox"
  }
}

run "accepts_project_with_digits" {
  command = plan

  variables {
    project = "tabp2"
  }
}

run "rejects_project_with_upper_case" {
  command = plan

  variables {
    project = "Tabp"
  }

  expect_failures = [var.project]
}

run "rejects_cidr_just_below_10" {
  command = plan

  variables {
    cidr = "9.255.0.0/16"
  }

  expect_failures = [var.cidr]
}

run "rejects_cidr_just_above_10" {
  command = plan

  variables {
    cidr = "11.0.0.0/16"
  }

  expect_failures = [var.cidr]
}

run "rejects_cidr_just_below_172_16" {
  command = plan

  variables {
    cidr = "172.15.0.0/16"
  }

  expect_failures = [var.cidr]
}

run "rejects_cidr_just_above_172_31" {
  command = plan

  variables {
    cidr = "172.32.0.0/16"
  }

  expect_failures = [var.cidr]
}

run "rejects_cidr_just_below_192_168" {
  command = plan

  variables {
    cidr = "192.167.0.0/16"
  }

  expect_failures = [var.cidr]
}

run "rejects_cidr_just_above_192_168" {
  command = plan

  variables {
    cidr = "192.169.0.0/16"
  }

  expect_failures = [var.cidr]
}

run "accepts_cidr_at_start_of_172_16" {
  command = plan

  variables {
    cidr = "172.16.0.0/16"
  }
}

run "accepts_cidr_just_above_172_17" {
  command = plan

  variables {
    cidr = "172.18.0.0/16"
  }
}

run "rejects_cidr_inside_172_17" {
  command = plan

  variables {
    cidr = "172.17.5.0/24"
  }

  expect_failures = [var.cidr]
}

run "accepts_four_availability_zones" {
  command = plan

  variables {
    availability_zone_ids = ["apne1-az1", "apne1-az2", "apne1-az3", "apne1-az4"]
  }

  override_data {
    target = module.subnet_tiers.data.aws_availability_zones.available
    values = { zone_ids = ["apne1-az1", "apne1-az2", "apne1-az3", "apne1-az4"] }
  }
}

run "rejects_retention_between_allowed_values" {
  command = plan

  variables {
    flow_logs_retention_days = 366
  }

  expect_failures = [var.flow_logs_retention_days]
}
