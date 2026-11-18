# kics-scan disable=e592a0c5-5bdb-414c-9066-5dba7cdea370,4bc4dd4c-7d8d-405e-a0fb-57fa4c31b4d9
# e592a0c5（IAM Access Analyzer）: アカウント単位の統制で account-baseline が持つ（docs/v1.0-plan/scope.md 第7節）。撤回: account-baseline を実装したとき。
# 4bc4dd4c（HTTP の要求を受ける）: Statement を concat で組むので KICS が TLS の Deny を読めない。Deny の在ることは Policy Test の PLAN-AUDIT-03 が見る。撤回: KICS が concat を評価するか、Statement を literal の列で書けるようになったとき。

locals {
  objects = "${aws_s3_bucket.this.arn}/*"
  trail   = var.writers.cloudtrail_organization_trail
  # 組織の trail の aws:SourceArn は、管理アカウントが持つ trail の ARN（CloudTrail User Guide「Amazon S3 bucket policy for CloudTrail」）。
  trail_arn       = "arn:${data.aws_partition.current.partition}:cloudtrail:${local.trail.region}:${local.trail.owner_account_id}:trail/${local.trail.trail_name}"
  config_accounts = sort(var.writers.config_source_accounts)

  # Bucket と鍵の設定の変更の拒否は、Bucket Policy ではなく SCP が持つ（audit-evidence/ADR-0102 決定6）。
  deny_statements = [
    {
      Sid       = "DenyInsecureTransport"
      Effect    = "Deny"
      Principal = "*"
      Action    = "s3:*"
      Resource  = [aws_s3_bucket.this.arn, local.objects]
      Condition = { Bool = { "aws:SecureTransport" = "false" } }
    },
    {
      Sid       = "DenyObjectWriteByNonService"
      Effect    = "Deny"
      Principal = "*"
      Action    = "s3:PutObject"
      Resource  = local.objects
      Condition = { Bool = { "aws:PrincipalIsAWSService" = "false" } }
    },
    {
      Sid       = "DenyObjectDeletion"
      Effect    = "Deny"
      Principal = "*"
      Action    = ["s3:DeleteObject", "s3:DeleteObjectVersion"]
      Resource  = local.objects
    },
    # 既定より長い保持は、取り消せない保管の費用になる（Amazon S3 User Guide「Object Lock considerations」の上限の例）。
    {
      Sid       = "DenyRetentionBeyondDefault"
      Effect    = "Deny"
      Principal = "*"
      Action    = ["s3:PutObject", "s3:PutObjectRetention"]
      Resource  = local.objects
      Condition = { NumericGreaterThan = { "s3:object-lock-remaining-retention-days" = tostring(var.retention.days) } }
    },
    # 否定の演算子は条件キーが無い要求で真になるので、Null で header が在ることを確かめてから値を比べる。
    {
      Sid       = "DenyOtherLockMode"
      Effect    = "Deny"
      Principal = "*"
      Action    = ["s3:PutObject", "s3:PutObjectRetention"]
      Resource  = local.objects
      Condition = {
        Null            = { "s3:object-lock-mode" = "false" }
        StringNotEquals = { "s3:object-lock-mode" = var.retention.mode }
      }
    },
    # hold は保持の上限の Deny を越えて object を残す。
    {
      Sid       = "DenyLegalHold"
      Effect    = "Deny"
      Principal = "*"
      Action    = "s3:PutObjectLegalHold"
      Resource  = local.objects
    },
    {
      Sid       = "DenyLegalHoldOnWrite"
      Effect    = "Deny"
      Principal = "*"
      Action    = "s3:PutObject"
      Resource  = local.objects
      Condition = { Null = { "s3:object-lock-legal-hold" = "false" } }
    },
    {
      Sid       = "DenyEventHold"
      Effect    = "Deny"
      Principal = "*"
      Action    = ["s3:PutObject", "s3:PutObjectRetention"]
      Resource  = local.objects
      Condition = { Null = { "s3:object-lock-event-hold" = "false" } }
    },
  ]

  cloudtrail_statements = [
    {
      Sid       = "AWSCloudTrailAclCheck"
      Effect    = "Allow"
      Principal = { Service = "cloudtrail.amazonaws.com" }
      Action    = "s3:GetBucketAcl"
      Resource  = aws_s3_bucket.this.arn
      Condition = { StringEquals = { "aws:SourceArn" = local.trail_arn } }
    },
    {
      Sid       = "AWSCloudTrailWrite"
      Effect    = "Allow"
      Principal = { Service = "cloudtrail.amazonaws.com" }
      Action    = "s3:PutObject"
      Resource  = "${aws_s3_bucket.this.arn}/AWSLogs/${local.trail.owner_account_id}/*"
      Condition = { StringEquals = {
        "s3:x-amz-acl"  = "bucket-owner-full-control"
        "aws:SourceArn" = local.trail_arn
      } }
    },
    {
      Sid       = "AWSCloudTrailOrganizationWrite"
      Effect    = "Allow"
      Principal = { Service = "cloudtrail.amazonaws.com" }
      Action    = "s3:PutObject"
      Resource  = "${aws_s3_bucket.this.arn}/AWSLogs/${local.trail.organization_id}/*"
      Condition = { StringEquals = {
        "s3:x-amz-acl"  = "bucket-owner-full-control"
        "aws:SourceArn" = local.trail_arn
      } }
    },
  ]

  # 書き込みの文はアカウントごとに分ける（audit-evidence/ADR-0102 決定4）。
  config_statements = concat(
    [for check in length(local.config_accounts) == 0 ? [] : [
      {
        Sid       = "AWSConfigBucketPermissionsCheck"
        Effect    = "Allow"
        Principal = { Service = "config.amazonaws.com" }
        Action    = "s3:GetBucketAcl"
        Resource  = aws_s3_bucket.this.arn
        Condition = { StringEquals = { "aws:SourceAccount" = local.config_accounts } }
      },
      {
        Sid       = "AWSConfigBucketExistenceCheck"
        Effect    = "Allow"
        Principal = { Service = "config.amazonaws.com" }
        Action    = "s3:ListBucket"
        Resource  = aws_s3_bucket.this.arn
        Condition = { StringEquals = { "aws:SourceAccount" = local.config_accounts } }
      },
    ] : check],
    [for a in local.config_accounts : {
      Sid       = "AWSConfigBucketDelivery${a}"
      Effect    = "Allow"
      Principal = { Service = "config.amazonaws.com" }
      Action    = "s3:PutObject"
      Resource  = "${aws_s3_bucket.this.arn}/AWSLogs/${a}/Config/*"
      Condition = { StringEquals = {
        "s3:x-amz-acl"      = "bucket-owner-full-control"
        "aws:SourceAccount" = a
      } }
    }],
  )
}

resource "aws_s3_bucket_policy" "this" {
  bucket = aws_s3_bucket.this.id

  policy = jsonencode({
    Version   = "2012-10-17"
    Statement = concat(local.deny_statements, local.cloudtrail_statements, local.config_statements)
  })
}
