mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }
}

variables {
  name               = "tabp-sandbox-ops-notification-us-east-1"
  heartbeat_role_arn = "arn:aws:iam::123456789012:role/tabp-sandbox-ops-heartbeat"
}

run "key_policy" {
  command = plan

  assert {
    condition = jsondecode(aws_kms_key_policy.this.policy) == {
      Version = "2012-10-17"
      Statement = [
        {
          Sid       = "AccountKeyAdministration"
          Effect    = "Allow"
          Principal = { AWS = "arn:aws:iam::123456789012:root" }
          Action = [
            "kms:CancelKeyDeletion",
            "kms:CreateAlias",
            "kms:DeleteAlias",
            "kms:DescribeKey",
            "kms:DisableKey",
            "kms:DisableKeyRotation",
            "kms:EnableKey",
            "kms:EnableKeyRotation",
            "kms:GetKeyPolicy",
            "kms:GetKeyRotationStatus",
            "kms:ListGrants",
            "kms:ListKeyPolicies",
            "kms:ListResourceTags",
            "kms:PutKeyPolicy",
            "kms:RevokeGrant",
            "kms:RotateKeyOnDemand",
            "kms:ScheduleKeyDeletion",
            "kms:TagResource",
            "kms:UntagResource",
            "kms:UpdateAlias",
            "kms:UpdateKeyDescription",
          ]
          Resource = "*"
        },
        {
          Sid       = "RolesViaSns0"
          Effect    = "Allow"
          Principal = { AWS = ["arn:aws:iam::123456789012:role/tabp-sandbox-ops-heartbeat"] }
          Action    = ["kms:Decrypt", "kms:GenerateDataKey*"]
          Resource  = "*"
          Condition = { StringEquals = {
            "kms:ViaService"                         = "sns.us-east-1.amazonaws.com"
            "kms:EncryptionContext:aws:sns:topicArn" = ["arn:aws:sns:us-east-1:123456789012:tabp-sandbox-ops-notification-us-east-1"]
          } }
        },
        {
          Sid       = "SnsPublisherCloudWatch"
          Effect    = "Allow"
          Principal = { Service = "cloudwatch.amazonaws.com" }
          Action    = ["kms:Decrypt", "kms:GenerateDataKey*"]
          Resource  = "*"
          Condition = { StringEquals = { "aws:SourceAccount" = "123456789012" } }
        },
      ]
    }
    error_message = "Key Policy が、管理の委任・心拍の role の SNS 経由・cloudwatch の送り手の3つの文になっていません。"
  }

  assert {
    condition     = output.key_policy == aws_kms_key_policy.this.policy
    error_message = "key_policy の出力が Key Policy の文書を返していません。"
  }
}

run "topic_policy" {
  command = plan

  assert {
    condition = jsondecode(aws_sns_topic_policy.this.policy) == {
      Version = "2012-10-17"
      Statement = [
        {
          Sid       = "DenyInsecureTransport"
          Effect    = "Deny"
          Principal = "*"
          Action    = "sns:Publish"
          Resource  = "arn:aws:sns:us-east-1:123456789012:tabp-sandbox-ops-notification-us-east-1"
          Condition = { Bool = { "aws:SecureTransport" = "false" } }
        },
        {
          Sid       = "AllowCloudWatchPublish"
          Effect    = "Allow"
          Principal = { Service = "cloudwatch.amazonaws.com" }
          Action    = "sns:Publish"
          Resource  = "arn:aws:sns:us-east-1:123456789012:tabp-sandbox-ops-notification-us-east-1"
          Condition = { StringEquals = { "aws:SourceAccount" = "123456789012" } }
        },
      ]
    }
    error_message = "Topic Policy が、TLS でない publish の Deny と、cloudwatch だけの送り手の Allow になっていません（ops-notification/ADR-0101）。"
  }
}
