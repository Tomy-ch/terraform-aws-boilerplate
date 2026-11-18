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

run "delivery_stalled" {
  command = apply

  assert {
    condition     = aws_s3_bucket_metric.delivery.bucket == aws_s3_bucket.this.id && aws_s3_bucket_metric.delivery.name == "delivery" && one(aws_s3_bucket_metric.delivery.filter).prefix == "AWSLogs/"
    error_message = "request metrics が、この Bucket の AWSLogs/ の下を数えていません。"
  }

  assert {
    condition = (
      aws_cloudwatch_metric_alarm.delivery_stalled.namespace == "AWS/S3" &&
      aws_cloudwatch_metric_alarm.delivery_stalled.metric_name == "PutRequests" &&
      aws_cloudwatch_metric_alarm.delivery_stalled.statistic == "Sum" &&
      aws_cloudwatch_metric_alarm.delivery_stalled.period == 3600 &&
      aws_cloudwatch_metric_alarm.delivery_stalled.evaluation_periods == 1 &&
      aws_cloudwatch_metric_alarm.delivery_stalled.threshold == 1 &&
      aws_cloudwatch_metric_alarm.delivery_stalled.comparison_operator == "LessThanThreshold"
    )
    error_message = "alarm が、1時間の書き込みが0件のときに発報する形になっていません。"
  }

  assert {
    condition     = tomap(aws_cloudwatch_metric_alarm.delivery_stalled.dimensions) == tomap({ BucketName = aws_s3_bucket.this.id, FilterId = "delivery" })
    error_message = "alarm の dimension が、この Bucket の request metrics を指していません。"
  }

  assert {
    condition     = aws_cloudwatch_metric_alarm.delivery_stalled.treat_missing_data == "breaching"
    error_message = "metric の欠落を途絶として読んでいません。"
  }

  assert {
    condition     = aws_cloudwatch_metric_alarm.delivery_stalled.actions_enabled == true
    error_message = "alarm の action が有効になっていません。"
  }

  assert {
    condition = (
      toset(aws_cloudwatch_metric_alarm.delivery_stalled.alarm_actions) == toset([var.notify_topic_arn]) &&
      toset(aws_cloudwatch_metric_alarm.delivery_stalled.ok_actions) == toset([var.notify_topic_arn]) &&
      toset(aws_cloudwatch_metric_alarm.delivery_stalled.insufficient_data_actions) == toset([var.notify_topic_arn])
    )
    error_message = "alarm の3つの action が、入力の Topic だけを指していません。"
  }

  assert {
    condition     = aws_cloudwatch_metric_alarm.delivery_stalled.alarm_name == "tabp-log-archive-audit-123456789012-ap-northeast-1-an-delivery-stalled"
    error_message = "alarm の名前が Bucket の名前から組まれていません。"
  }
}

run "rejects_topic_in_other_account" {
  command = plan

  variables {
    notify_topic_arn = "arn:aws:sns:ap-northeast-1:210987654321:tabp-log-archive-ops-notification"
  }

  expect_failures = [aws_cloudwatch_metric_alarm.delivery_stalled]
}
