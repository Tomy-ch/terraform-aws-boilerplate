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

run "log_archive" {
  command = plan

  assert {
    condition     = module.archive.retention == { mode = "COMPLIANCE", days = 731 }
    error_message = "log-archive の既定の保持が、COMPLIANCE と retention_days になっていません。"
  }

  assert {
    condition     = module.archive.force_destroy == false
    error_message = "log-archive の Bucket が force_destroy を持っています。"
  }

}

run "sandbox" {
  command = plan

  variables {
    env = "sandbox"
  }

  assert {
    condition     = module.archive.retention == { mode = "GOVERNANCE", days = 1 }
    error_message = "sandbox の既定の保持が、GOVERNANCE の1日になっていません。"
  }

  assert {
    condition     = module.archive.force_destroy == true
    error_message = "sandbox の Bucket が force_destroy を持っていません。"
  }

}

run "key_policy_wiring" {
  command = plan

  variables {
    writers = {
      cloudtrail_organization_trail = {
        trail_name       = "tabp-org"
        owner_account_id = "111111111111"
        organization_id  = "o-abcdefghij"
        region           = "us-west-2"
      }
      config_source_accounts = ["333333333333", "222222222222"]
    }
  }

  assert {
    condition     = [for s in jsondecode(module.archive_key.key_policy).Statement : s.Condition.StringEquals["aws:SourceArn"] if s.Sid == "CloudTrailEncrypt"] == [["arn:aws:cloudtrail:us-west-2:111111111111:trail/tabp-org"]]
    error_message = "証跡の鍵の CloudTrail の文が、trail の名前・所有するアカウント・リージョンから組んだ ARN を要求元にしていません。"
  }

  assert {
    condition     = [for s in jsondecode(module.archive_key.key_policy).Statement : s.Condition.StringEquals["aws:SourceAccount"] if s.Sid == "ConfigService"] == [["222222222222", "333333333333"]]
    error_message = "証跡の鍵の Config の文が、Config のアカウントを昇順で要求元にしていません。"
  }

  assert {
    condition     = [for s in jsondecode(module.access_log_key.key_policy).Statement : s.Sid] == ["AccountKeyAdministration", "LogsService"]
    error_message = "アクセスログの鍵が、CloudWatch Logs の文だけを持っていません。"
  }
}
