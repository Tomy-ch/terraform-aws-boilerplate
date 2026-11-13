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
  mock_resource "aws_cloudwatch_log_group" {
    defaults = { arn = "arn:aws:logs:ap-northeast-1:123456789012:log-group:tabp-admin-state-access" }
  }
  mock_resource "aws_cloudwatch_log_delivery_destination" {
    defaults = { arn = "arn:aws:logs:ap-northeast-1:123456789012:delivery-destination:tabp-admin-state-access" }
  }
}

variables {
  name                = "tabp-admin-state-access"
  retention_days      = 365
  kms_key_arn         = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  deletion_protection = true
  delivery_sources    = ["tabp-admin-tfstate"]
}

run "allows_only_the_named_delivery_sources_to_write" {
  command = apply

  variables {
    delivery_sources = ["tabp-admin-tfstate", "tabp-admin-plan-output"]
  }

  assert {
    condition     = aws_cloudwatch_log_resource_policy.this.resource_arn == aws_cloudwatch_log_group.this.arn
    error_message = "resource policy が Log Group に付いていません。"
  }
  assert {
    condition = jsondecode(aws_cloudwatch_log_resource_policy.this.policy_document) == {
      Version = "2012-10-17"
      Statement = [{
        Sid       = "AWSLogDeliveryWrite"
        Effect    = "Allow"
        Principal = { Service = "delivery.logs.amazonaws.com" }
        Action    = ["logs:CreateLogStream", "logs:PutLogEvents"]
        Resource  = "${aws_cloudwatch_log_group.this.arn}:log-stream:*"
        Condition = {
          StringEquals = { "aws:SourceAccount" = "123456789012" }
          ArnLike = { "aws:SourceArn" = [
            "arn:aws:logs:ap-northeast-1:123456789012:delivery-source:tabp-admin-plan-output",
            "arn:aws:logs:ap-northeast-1:123456789012:delivery-source:tabp-admin-tfstate",
          ] }
        }
      }]
    }
    error_message = "resource policy の文書が、名指した delivery source だけに書き込みを許す1文になっていません。"
  }
}

run "derives_condition_from_partition_region_and_account" {
  command = apply

  variables {
    kms_key_arn = "arn:aws-us-gov:kms:us-gov-west-1:210987654321:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  }

  override_data {
    target = data.aws_partition.current
    values = { partition = "aws-us-gov" }
  }
  override_data {
    target = data.aws_region.current
    values = { region = "us-gov-west-1" }
  }
  override_data {
    target = data.aws_caller_identity.current
    values = { account_id = "210987654321" }
  }

  assert {
    condition = one(jsondecode(aws_cloudwatch_log_resource_policy.this.policy_document).Statement).Condition == {
      StringEquals = { "aws:SourceAccount" = "210987654321" }
      ArnLike      = { "aws:SourceArn" = ["arn:aws-us-gov:logs:us-gov-west-1:210987654321:delivery-source:tabp-admin-tfstate"] }
    }
    error_message = "resource policy の Condition が、呼んだ partition・リージョン・アカウントに追随していません。"
  }
}
