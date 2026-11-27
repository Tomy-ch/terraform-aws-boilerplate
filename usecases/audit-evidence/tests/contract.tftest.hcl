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
    config_source_accounts = ["111111111111"]
  }
  access_log_retention_days = 365
  notify_topic_arn          = "arn:aws:sns:ap-northeast-1:123456789012:tabp-log-archive-ops-notification"
}

run "outputs" {
  command = apply

  assert {
    condition     = output.bucket_name == "tabp-log-archive-audit-123456789012-ap-northeast-1-an"
    error_message = "bucket_name が保管の Bucket の完全な名前を返していません。"
  }

  assert {
    condition     = output.bucket_arn == "arn:aws:s3:::tabp-log-archive-audit-123456789012-ap-northeast-1-an"
    error_message = "bucket_arn が保管の Bucket の ARN を返していません。"
  }

  assert {
    condition     = output.kms_key_arn == "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
    error_message = "kms_key_arn が証跡の鍵の ARN を返していません。"
  }

  assert {
    condition     = output.access_log_group_arn == "arn:aws:logs:ap-northeast-1:123456789012:log-group:tabp-log-archive-audit-access-log"
    error_message = "access_log_group_arn がアクセスログの Log Group の ARN を返していません。"
  }
}
