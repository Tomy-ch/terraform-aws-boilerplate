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

override_resource {
  target = aws_s3_bucket.this
  values = {
    id  = "tabp-log-archive-audit-123456789012-ap-northeast-1-an"
    arn = "arn:aws:s3:::tabp-log-archive-audit-123456789012-ap-northeast-1-an"
  }
}

variables {
  name          = "tabp-log-archive-audit"
  kms_key_arn   = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  retention     = { mode = "COMPLIANCE", days = 365 }
  force_destroy = false
  writers = {
    cloudtrail_organization_trail = {
      trail_name       = "tabp-org"
      owner_account_id = "111111111111"
      organization_id  = "o-abcdefghij"
      region           = "ap-northeast-1"
    }
  }
  access_log_source_name     = "tabp-log-archive-audit-access-log"
  access_log_destination_arn = "arn:aws:logs:ap-northeast-1:123456789012:delivery-destination:tabp-log-archive-audit-access-log"
  notify_topic_arn           = "arn:aws:sns:ap-northeast-1:123456789012:tabp-log-archive-ops-notification"
}

run "rejects_upper_case_name" {
  command = plan
  variables {
    name = "Tabp-audit"
  }
  expect_failures = [var.name]
}

run "rejects_reserved_prefix" {
  command = plan
  variables {
    name = "xn--audit"
  }
  expect_failures = [var.name]
}

run "rejects_alias_as_key" {
  command = plan
  variables {
    kms_key_arn = "arn:aws:kms:ap-northeast-1:123456789012:alias/tabp-audit"
  }
  expect_failures = [var.kms_key_arn]
}

run "rejects_lower_case_mode" {
  command = plan
  variables {
    retention = { mode = "compliance", days = 365 }
  }
  expect_failures = [var.retention]
}

run "rejects_zero_days" {
  command = plan
  variables {
    retention = { mode = "COMPLIANCE", days = 0 }
  }
  expect_failures = [var.retention]
}

run "rejects_fractional_days" {
  command = plan
  variables {
    retention = { mode = "COMPLIANCE", days = 1.5 }
  }
  expect_failures = [var.retention]
}

run "rejects_days_over_100_years" {
  command = plan
  variables {
    retention = { mode = "COMPLIANCE", days = 36501 }
  }
  expect_failures = [var.retention]
}

run "rejects_trail_name_with_slash" {
  command = plan
  variables {
    writers = { cloudtrail_organization_trail = { trail_name = "tabp/org", owner_account_id = "111111111111", organization_id = "o-abcdefghij", region = "ap-northeast-1" } }
  }
  expect_failures = [var.writers]
}

run "rejects_trail_name_with_consecutive_symbols" {
  command = plan
  variables {
    writers = { cloudtrail_organization_trail = { trail_name = "tabp--org", owner_account_id = "111111111111", organization_id = "o-abcdefghij", region = "ap-northeast-1" } }
  }
  expect_failures = [var.writers]
}

run "rejects_owner_account_wildcard" {
  command = plan
  variables {
    writers = { cloudtrail_organization_trail = { trail_name = "tabp-org", owner_account_id = "*", organization_id = "o-abcdefghij", region = "ap-northeast-1" } }
  }
  expect_failures = [var.writers]
}

run "rejects_bad_organization_id" {
  command = plan
  variables {
    writers = { cloudtrail_organization_trail = { trail_name = "tabp-org", owner_account_id = "111111111111", organization_id = "o-ABC", region = "ap-northeast-1" } }
  }
  expect_failures = [var.writers]
}

run "rejects_bad_region" {
  command = plan
  variables {
    writers = { cloudtrail_organization_trail = { trail_name = "tabp-org", owner_account_id = "111111111111", organization_id = "o-abcdefghij", region = "*" } }
  }
  expect_failures = [var.writers]
}

run "rejects_bad_config_account" {
  command = plan
  variables {
    writers = {
      cloudtrail_organization_trail = { trail_name = "tabp-org", owner_account_id = "111111111111", organization_id = "o-abcdefghij", region = "ap-northeast-1" }
      config_source_accounts        = ["*"]
    }
  }
  expect_failures = [var.writers]
}

run "rejects_source_name_with_slash" {
  command = plan
  variables {
    access_log_source_name = "tabp/audit"
  }
  expect_failures = [var.access_log_source_name]
}

run "rejects_log_group_as_destination" {
  command = plan
  variables {
    access_log_destination_arn = "arn:aws:logs:ap-northeast-1:123456789012:log-group:tabp-audit"
  }
  expect_failures = [var.access_log_destination_arn]
}

run "rejects_fifo_topic" {
  command = plan
  variables {
    notify_topic_arn = "arn:aws:sns:ap-northeast-1:123456789012:tabp-ops.fifo"
  }
  expect_failures = [var.notify_topic_arn]
}

run "rejects_trail_name_ending_with_symbol" {
  command = plan
  variables {
    writers = { cloudtrail_organization_trail = { trail_name = "tabp-org-", owner_account_id = "111111111111", organization_id = "o-abcdefghij", region = "ap-northeast-1" } }
  }
  expect_failures = [var.writers]
}

run "rejects_trail_name_as_ip_address" {
  command = plan
  variables {
    writers = { cloudtrail_organization_trail = { trail_name = "192.168.5.4", owner_account_id = "111111111111", organization_id = "o-abcdefghij", region = "ap-northeast-1" } }
  }
  expect_failures = [var.writers]
}

run "accepts_days_at_100_years" {
  command = plan
  variables {
    retention = { mode = "COMPLIANCE", days = 36500 }
  }
}

run "rejects_two_character_trail_name" {
  command = plan
  variables {
    writers = { cloudtrail_organization_trail = { trail_name = "ab", owner_account_id = "111111111111", organization_id = "o-abcdefghij", region = "ap-northeast-1" } }
  }
  expect_failures = [var.writers]
}

run "accepts_128_character_trail_name" {
  command = plan
  variables {
    writers = { cloudtrail_organization_trail = { trail_name = "taaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaz", owner_account_id = "111111111111", organization_id = "o-abcdefghij", region = "ap-northeast-1" } }
  }
}

run "rejects_129_character_trail_name" {
  command = plan
  variables {
    writers = { cloudtrail_organization_trail = { trail_name = "taaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaz", owner_account_id = "111111111111", organization_id = "o-abcdefghij", region = "ap-northeast-1" } }
  }
  expect_failures = [var.writers]
}
