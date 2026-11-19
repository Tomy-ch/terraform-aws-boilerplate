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
  bucket                     = "arn:aws:s3:::tabp-log-archive-audit-123456789012-ap-northeast-1-an"
  trail                      = "arn:aws:cloudtrail:ap-northeast-1:111111111111:trail/tabp-org"

  deny_statements = [
    {
      Sid       = "DenyInsecureTransport"
      Effect    = "Deny"
      Principal = "*"
      Action    = "s3:*"
      Resource  = ["arn:aws:s3:::tabp-log-archive-audit-123456789012-ap-northeast-1-an", "arn:aws:s3:::tabp-log-archive-audit-123456789012-ap-northeast-1-an/*"]
      Condition = { Bool = { "aws:SecureTransport" = "false" } }
    },
    {
      Sid       = "DenyObjectWriteByNonService"
      Effect    = "Deny"
      Principal = "*"
      Action    = "s3:PutObject"
      Resource  = "arn:aws:s3:::tabp-log-archive-audit-123456789012-ap-northeast-1-an/*"
      Condition = { Bool = { "aws:PrincipalIsAWSService" = "false" } }
    },
    {
      Sid       = "DenyObjectDeletion"
      Effect    = "Deny"
      Principal = "*"
      Action    = ["s3:DeleteObject", "s3:DeleteObjectVersion"]
      Resource  = "arn:aws:s3:::tabp-log-archive-audit-123456789012-ap-northeast-1-an/*"
    },
    {
      Sid       = "DenyRetentionBeyondDefault"
      Effect    = "Deny"
      Principal = "*"
      Action    = ["s3:PutObject", "s3:PutObjectRetention"]
      Resource  = "arn:aws:s3:::tabp-log-archive-audit-123456789012-ap-northeast-1-an/*"
      Condition = { NumericGreaterThan = { "s3:object-lock-remaining-retention-days" = "365" } }
    },
    {
      Sid       = "DenyOtherLockMode"
      Effect    = "Deny"
      Principal = "*"
      Action    = ["s3:PutObject", "s3:PutObjectRetention"]
      Resource  = "arn:aws:s3:::tabp-log-archive-audit-123456789012-ap-northeast-1-an/*"
      Condition = {
        Null            = { "s3:object-lock-mode" = "false" }
        StringNotEquals = { "s3:object-lock-mode" = "COMPLIANCE" }
      }
    },
    {
      Sid       = "DenyLegalHold"
      Effect    = "Deny"
      Principal = "*"
      Action    = "s3:PutObjectLegalHold"
      Resource  = "arn:aws:s3:::tabp-log-archive-audit-123456789012-ap-northeast-1-an/*"
    },
    {
      Sid       = "DenyLegalHoldOnWrite"
      Effect    = "Deny"
      Principal = "*"
      Action    = "s3:PutObject"
      Resource  = "arn:aws:s3:::tabp-log-archive-audit-123456789012-ap-northeast-1-an/*"
      Condition = { Null = { "s3:object-lock-legal-hold" = "false" } }
    },
    {
      Sid       = "DenyEventHold"
      Effect    = "Deny"
      Principal = "*"
      Action    = ["s3:PutObject", "s3:PutObjectRetention"]
      Resource  = "arn:aws:s3:::tabp-log-archive-audit-123456789012-ap-northeast-1-an/*"
      Condition = { Null = { "s3:object-lock-event-hold" = "false" } }
    },
  ]

  cloudtrail_statements = [
    {
      Sid       = "AWSCloudTrailAclCheck"
      Effect    = "Allow"
      Principal = { Service = "cloudtrail.amazonaws.com" }
      Action    = "s3:GetBucketAcl"
      Resource  = "arn:aws:s3:::tabp-log-archive-audit-123456789012-ap-northeast-1-an"
      Condition = { StringEquals = { "aws:SourceArn" = "arn:aws:cloudtrail:ap-northeast-1:111111111111:trail/tabp-org" } }
    },
    {
      Sid       = "AWSCloudTrailWrite"
      Effect    = "Allow"
      Principal = { Service = "cloudtrail.amazonaws.com" }
      Action    = "s3:PutObject"
      Resource  = "arn:aws:s3:::tabp-log-archive-audit-123456789012-ap-northeast-1-an/AWSLogs/111111111111/*"
      Condition = { StringEquals = {
        "s3:x-amz-acl"  = "bucket-owner-full-control"
        "aws:SourceArn" = "arn:aws:cloudtrail:ap-northeast-1:111111111111:trail/tabp-org"
      } }
    },
    {
      Sid       = "AWSCloudTrailOrganizationWrite"
      Effect    = "Allow"
      Principal = { Service = "cloudtrail.amazonaws.com" }
      Action    = "s3:PutObject"
      Resource  = "arn:aws:s3:::tabp-log-archive-audit-123456789012-ap-northeast-1-an/AWSLogs/o-abcdefghij/*"
      Condition = { StringEquals = {
        "s3:x-amz-acl"  = "bucket-owner-full-control"
        "aws:SourceArn" = "arn:aws:cloudtrail:ap-northeast-1:111111111111:trail/tabp-org"
      } }
    },
  ]
}

run "without_config" {
  command = apply

  assert {
    condition = jsondecode(aws_s3_bucket_policy.this.policy) == {
      Version   = "2012-10-17"
      Statement = concat(var.deny_statements, var.cloudtrail_statements)
    }
    error_message = "Bucket Policy が、Deny の8文と組織の trail の3文になっていません。"
  }

  assert {
    condition     = aws_s3_bucket_policy.this.bucket == aws_s3_bucket.this.id
    error_message = "Bucket Policy が、この module の Bucket に付いていません。"
  }
}

# 書き込みの文はアカウントごとに分かれ、並びは入力の順序に依らない。
run "with_config" {
  command = apply

  variables {
    writers = {
      cloudtrail_organization_trail = {
        trail_name       = "tabp-org"
        owner_account_id = "111111111111"
        organization_id  = "o-abcdefghij"
        region           = "ap-northeast-1"
      }
      config_source_accounts = ["333333333333", "222222222222"]
    }
  }

  assert {
    condition = jsondecode(aws_s3_bucket_policy.this.policy) == {
      Version = "2012-10-17"
      Statement = concat(var.deny_statements, var.cloudtrail_statements, [
        {
          Sid       = "AWSConfigBucketPermissionsCheck"
          Effect    = "Allow"
          Principal = { Service = "config.amazonaws.com" }
          Action    = "s3:GetBucketAcl"
          Resource  = var.bucket
          Condition = { StringEquals = { "aws:SourceAccount" = ["222222222222", "333333333333"] } }
        },
        {
          Sid       = "AWSConfigBucketExistenceCheck"
          Effect    = "Allow"
          Principal = { Service = "config.amazonaws.com" }
          Action    = "s3:ListBucket"
          Resource  = var.bucket
          Condition = { StringEquals = { "aws:SourceAccount" = ["222222222222", "333333333333"] } }
        },
        {
          Sid       = "AWSConfigBucketDelivery222222222222"
          Effect    = "Allow"
          Principal = { Service = "config.amazonaws.com" }
          Action    = "s3:PutObject"
          Resource  = "${var.bucket}/AWSLogs/222222222222/Config/*"
          Condition = { StringEquals = {
            "s3:x-amz-acl"      = "bucket-owner-full-control"
            "aws:SourceAccount" = "222222222222"
          } }
        },
        {
          Sid       = "AWSConfigBucketDelivery333333333333"
          Effect    = "Allow"
          Principal = { Service = "config.amazonaws.com" }
          Action    = "s3:PutObject"
          Resource  = "${var.bucket}/AWSLogs/333333333333/Config/*"
          Condition = { StringEquals = {
            "s3:x-amz-acl"      = "bucket-owner-full-control"
            "aws:SourceAccount" = "333333333333"
          } }
        },
      ])
    }
    error_message = "Config のアカウントを渡したときの Bucket Policy に、Config の確認の2文とアカウントごとの書き込みの文が並んでいません。"
  }
}

run "governance_conditions" {
  command = apply

  variables {
    retention = { mode = "GOVERNANCE", days = 1 }
  }

  assert {
    condition     = [for s in jsondecode(aws_s3_bucket_policy.this.policy).Statement : s.Condition if s.Sid == "DenyRetentionBeyondDefault"][0] == { NumericGreaterThan = { "s3:object-lock-remaining-retention-days" = "1" } }
    error_message = "保持の上限の Deny が、入力の日数になっていません。"
  }

  assert {
    condition     = [for s in jsondecode(aws_s3_bucket_policy.this.policy).Statement : s.Condition if s.Sid == "DenyOtherLockMode"][0].StringNotEquals == { "s3:object-lock-mode" = "GOVERNANCE" }
    error_message = "mode の Deny が、入力の mode 以外を拒んでいません。"
  }
}
