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

# 鍵・delivery destination・Bucket の ARN の validation を通すため、形のある値にする。
override_resource {
  target = module.archive_key.aws_kms_key.this
  values = { arn = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab" }
}

override_resource {
  target = module.access_log_key.aws_kms_key.this
  values = { arn = "arn:aws:kms:ap-northeast-1:123456789012:key/5678abcd-12ab-34cd-56ef-1234567890ab" }
}

override_resource {
  target = module.access_log.aws_cloudwatch_log_delivery_destination.this
  values = { arn = "arn:aws:logs:ap-northeast-1:123456789012:delivery-destination:tabp-log-archive-audit-access-log" }
}

override_resource {
  target = module.access_log.aws_cloudwatch_log_group.this
  values = { arn = "arn:aws:logs:ap-northeast-1:123456789012:log-group:tabp-log-archive-audit-access-log" }
}

override_resource {
  target = module.archive.aws_s3_bucket.this
  values = {
    id  = "tabp-log-archive-audit-123456789012-ap-northeast-1-an"
    arn = "arn:aws:s3:::tabp-log-archive-audit-123456789012-ap-northeast-1-an"
  }
}

variables {
  project        = "tabp"
  env            = "log-archive"
  retention_days = 731
  writers = {
    cloudtrail_organization_trail = {
      trail_name       = "tabp-org"
      owner_account_id = "111111111111"
      organization_id  = "o-abcdefghij"
      region           = "ap-northeast-1"
    }
    config_source_accounts = ["222222222222"]
  }
  access_log_retention_days = 365
  notify_topic_arn          = "arn:aws:sns:ap-northeast-1:123456789012:tabp-log-archive-ops-notification"
}

run "rejects_project_with_hyphen" {
  command = plan
  variables {
    project = "tabp-x"
  }
  expect_failures = [var.project]
}

run "rejects_other_env" {
  command = plan
  variables {
    env = "prd"
  }
  expect_failures = [var.env]
}

run "rejects_retention_below_365" {
  command = plan
  variables {
    retention_days = 364
  }
  expect_failures = [var.retention_days]
}

run "accepts_retention_365" {
  command = plan
  variables {
    retention_days = 365
  }
}

run "accepts_retention_3653" {
  command = plan
  variables {
    retention_days = 3653
  }
}

run "rejects_retention_above_3653" {
  command = plan
  variables {
    retention_days = 3654
  }
  expect_failures = [var.retention_days]
}

run "rejects_fractional_retention" {
  command = plan
  variables {
    retention_days = 400.5
  }
  expect_failures = [var.retention_days]
}

run "rejects_unsupported_access_log_retention" {
  command = plan
  variables {
    access_log_retention_days = 30
  }
  expect_failures = [var.access_log_retention_days]
}

run "rejects_topic_name_only" {
  command = plan
  variables {
    notify_topic_arn = "tabp-log-archive-ops-notification"
  }
  expect_failures = [var.notify_topic_arn]
}

run "rejects_bad_trail_owner" {
  command = plan
  variables {
    writers = { cloudtrail_organization_trail = { trail_name = "tabp-org", owner_account_id = "1111", organization_id = "o-abcdefghij", region = "ap-northeast-1" } }
  }
  expect_failures = [var.writers]
}
