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

run "delivery" {
  command = apply

  assert {
    condition     = aws_cloudwatch_log_delivery_source.access_log.log_type == "S3_SERVER_ACCESS_LOGS"
    error_message = "delivery source の log_type が S3 のサーバーアクセスログになっていません。"
  }

  assert {
    condition     = aws_cloudwatch_log_delivery_source.access_log.resource_arn == "arn:aws:s3:::tabp-log-archive-audit-123456789012-ap-northeast-1-an"
    error_message = "delivery source が、この module の Bucket を指していません。"
  }

  assert {
    condition     = aws_cloudwatch_log_delivery_source.access_log.name == var.access_log_source_name
    error_message = "delivery source の名前が入力のとおりになっていません。受け口の resource policy が名指す名前と食い違います。"
  }

  assert {
    condition     = aws_cloudwatch_log_delivery.access_log.delivery_source_name == var.access_log_source_name && aws_cloudwatch_log_delivery.access_log.delivery_destination_arn == var.access_log_destination_arn
    error_message = "delivery が、この module の delivery source から入力の宛先へ配送していません。"
  }
}
