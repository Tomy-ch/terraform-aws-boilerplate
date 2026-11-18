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

variables {
  name        = "tabp-dev-ops-notification"
  kms_key_arn = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  publishers  = ["cloudwatch"]

  deny_insecure_transport = {
    Sid       = "DenyInsecureTransport"
    Effect    = "Deny"
    Principal = "*"
    Action    = "sns:Publish"
    Resource  = "arn:aws:sns:ap-northeast-1:123456789012:tabp-dev-ops-notification"
    Condition = { Bool = { "aws:SecureTransport" = "false" } }
  }
  allow = {
    for k, v in { budgets = "AllowBudgetsPublish", cloudwatch = "AllowCloudWatchPublish", costalerts = "AllowCostAlertsPublish", events = "AllowEventsPublish" } : k => {
      Sid                 = v
      Effect              = "Allow"
      Principal           = { Service = "${k}.amazonaws.com" }
      Action              = "sns:Publish"
      Resource            = "arn:aws:sns:ap-northeast-1:123456789012:tabp-dev-ops-notification"
      Condition           = { StringEquals = { "aws:SourceAccount" = "123456789012" } }
    }
  }
}

run "no_publishers" {
  command = plan

  variables {
    publishers = []
  }

  assert {
    condition     = jsondecode(aws_sns_topic_policy.this.policy) == { Version = "2012-10-17", Statement = [var.deny_insecure_transport] }
    error_message = "publishers が空のときの Topic Policy が、TLS でない publish の Deny の1文になっていません。"
  }
}

run "one_publisher" {
  command = plan

  assert {
    condition     = jsondecode(aws_sns_topic_policy.this.policy) == { Version = "2012-10-17", Statement = [var.deny_insecure_transport, var.allow.cloudwatch] }
    error_message = "cloudwatch だけのときの Topic Policy が、Deny と CloudWatch の Allow の2文になっていません。"
  }
}

run "two_publishers" {
  command = plan

  variables {
    publishers = ["events", "budgets"]
  }

  assert {
    condition     = jsondecode(aws_sns_topic_policy.this.policy) == { Version = "2012-10-17", Statement = [var.deny_insecure_transport, var.allow.budgets, var.allow.events] }
    error_message = "budgets と events のときの Topic Policy が、Deny と2つの Allow になっていません。"
  }
}

run "all_publishers" {
  command = plan

  variables {
    publishers = ["events", "costalerts", "cloudwatch", "budgets"]
  }

  assert {
    condition = jsondecode(aws_sns_topic_policy.this.policy) == {
      Version   = "2012-10-17"
      Statement = [var.deny_insecure_transport, var.allow.budgets, var.allow.cloudwatch, var.allow.costalerts, var.allow.events]
    }
    error_message = "4つの publisher のときの Topic Policy が、Deny と名前順の4つの Allow になっていません。"
  }
}
