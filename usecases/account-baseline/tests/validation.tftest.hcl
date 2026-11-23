mock_provider "aws" {
  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }
  mock_data "aws_regions" {
    defaults = { names = ["ap-northeast-1"] }
  }
}

variables {
  project = "tabp"
  env     = "dev"
  config_delivery = {
    bucket_name = "tabp-log-archive-audit-evidence-210987654321-ap-northeast-1-an"
    kms_key_arn = "arn:aws:kms:ap-northeast-1:210987654321:key/11111111-2222-3333-4444-555555555555"
  }
  config_history_retention_days = 365
  security_contact = {
    name          = "Security Team"
    title         = "Security"
    email_address = "security@example.com"
    phone_number  = "+81 3 0000 0000"
  }
}

run "project_with_hyphen" {
  command = plan
  variables {
    project = "ta-bp"
  }
  expect_failures = [var.project]
}

run "project_at_maximum_length" {
  command = plan
  variables {
    project = "${join("", [for i in range(23) : "abcdefghij"])}abcd"
    env     = "log-archive"
  }
}

run "project_over_maximum_length" {
  command = plan
  variables {
    project = "${join("", [for i in range(23) : "abcdefghij"])}abcde"
  }
  expect_failures = [var.project]
}

run "unknown_env" {
  command = plan
  variables {
    env = "test"
  }
  expect_failures = [var.env]
}

run "kms_alias_instead_of_key" {
  command = plan
  variables {
    config_delivery = {
      bucket_name = "tabp-log-archive-audit-evidence-210987654321-ap-northeast-1-an"
      kms_key_arn = "arn:aws:kms:ap-northeast-1:210987654321:alias/tabp"
    }
  }
  expect_failures = [var.config_delivery]
}

run "retention_below_minimum" {
  command = plan
  variables {
    config_history_retention_days = 29
  }
  expect_failures = [var.config_history_retention_days]
}

run "retention_at_minimum" {
  command = plan
  variables {
    config_history_retention_days = 30
  }
}

run "retention_above_maximum" {
  command = plan
  variables {
    config_history_retention_days = 2558
  }
  expect_failures = [var.config_history_retention_days]
}

run "retention_not_integer" {
  command = plan
  variables {
    config_history_retention_days = 365.5
  }
  expect_failures = [var.config_history_retention_days]
}

run "empty_contact_name" {
  command = plan
  variables {
    security_contact = {
      name          = " "
      title         = "Security"
      email_address = "security@example.com"
      phone_number  = "+81 3 0000 0000"
    }
  }
  expect_failures = [var.security_contact]
}

run "contact_email_without_domain" {
  command = plan
  variables {
    security_contact = {
      name          = "Security Team"
      title         = "Security"
      email_address = "security"
      phone_number  = "+81 3 0000 0000"
    }
  }
  expect_failures = [var.security_contact]
}

run "contact_phone_without_country_code" {
  command = plan
  variables {
    security_contact = {
      name          = "Security Team"
      title         = "Security"
      email_address = "security@example.com"
      phone_number  = "03 0000 0000"
    }
  }
  expect_failures = [var.security_contact]
}
