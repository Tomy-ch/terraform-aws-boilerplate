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

run "rules" {
  command = plan

  assert {
    condition     = length(aws_s3_bucket_lifecycle_configuration.this.rule) == 2
    error_message = "lifecycle の規則が2つではありません。"
  }

  assert {
    condition = (
      aws_s3_bucket_lifecycle_configuration.this.rule[0].id == "expire-after-retention" &&
      aws_s3_bucket_lifecycle_configuration.this.rule[0].status == "Enabled" &&
      one(aws_s3_bucket_lifecycle_configuration.this.rule[0].expiration).days == 365 &&
      one(aws_s3_bucket_lifecycle_configuration.this.rule[0].noncurrent_version_expiration).noncurrent_days == 1 &&
      one(aws_s3_bucket_lifecycle_configuration.this.rule[0].abort_incomplete_multipart_upload).days_after_initiation == 1
    )
    error_message = "1つ目の規則が、保持と同じ日数での失効・上書きされた版の1日での失効・multipart の1日での中止になっていません。"
  }

  assert {
    condition = (
      aws_s3_bucket_lifecycle_configuration.this.rule[1].id == "expire-delete-markers" &&
      aws_s3_bucket_lifecycle_configuration.this.rule[1].status == "Enabled" &&
      one(aws_s3_bucket_lifecycle_configuration.this.rule[1].expiration).expired_object_delete_marker == true
    )
    error_message = "2つ目の規則が、版の無い delete marker を消す規則になっていません。"
  }
}

run "days_follow_retention" {
  command = plan

  variables {
    retention = { mode = "GOVERNANCE", days = 1 }
  }

  assert {
    condition     = one(aws_s3_bucket_lifecycle_configuration.this.rule[0].expiration).days == 1
    error_message = "失効の日数が、保持の日数に揃っていません。"
  }
}
