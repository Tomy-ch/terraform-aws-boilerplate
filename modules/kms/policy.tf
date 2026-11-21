# kics-scan disable=e592a0c5-5bdb-414c-9066-5dba7cdea370
# e592a0c5（IAM Access Analyzer）: アカウント単位の統制で account-baseline が持つ（docs/v1.0-plan/scope.md 第7節）。撤回: account-baseline を実装したとき。

locals {
  account   = data.aws_caller_identity.current.account_id
  region    = data.aws_region.current.region
  partition = data.aws_partition.current.partition

  # 管理の委任は暗号操作と kms:CreateGrant を含まない。暗号操作は下の文で名指した相手だけに許す（README「公式推奨からの逸脱」）。
  administration_actions = [
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

  s3_roles = sort([for r in var.users.roles : r.arn if r.via == "s3" && r.access == "read-write"])
  # read の role は文を分けて、暗号化コンテキストの object を role ごとに限る。
  s3_readers = { for r in var.users.roles : r.arn => sort(r.objects) if r.via == "s3" && r.access == "read" }
  ecr_roles  = sort([for r in var.users.roles : r.arn if r.via == "ecr"])
  log_groups = sort(distinct(var.users.services.logs))
  trails     = sort(distinct(var.users.services.cloudtrail))
  config     = sort(distinct(var.users.services.config))

  sns_roles      = { for r in var.users.roles : r.arn => sort(r.topics) if r.via == "sns" }
  sns_publishers = { for p in ["budgets", "cloudwatch", "costalerts", "events"] : p => sort([for t in var.users.services.sns : t.topic if contains(t.publishers, p)]) }
  topic_arn      = "arn:${local.partition}:sns:${local.region}:${local.account}"

  statements = concat(
    [{
      Sid       = "AccountKeyAdministration"
      Effect    = "Allow"
      Principal = { AWS = "arn:${local.partition}:iam::${local.account}:root" }
      Action    = local.administration_actions
      Resource  = "*"
    }],
    length(local.s3_roles) == 0 ? [] : [{
      Sid       = "RolesViaS3ReadWrite"
      Effect    = "Allow"
      Principal = { AWS = local.s3_roles }
      Action    = ["kms:Decrypt", "kms:GenerateDataKey"]
      Resource  = "*"
      Condition = { StringEquals = { "kms:ViaService" = "s3.${local.region}.amazonaws.com" } }
    }],
    # object の ARN で限れるのは Bucket Key を使わない Bucket だけ（README「Key Policy の文書」）。
    [for i, arn in sort(keys(local.s3_readers)) : {
      Sid       = "RolesViaS3Read${i}"
      Effect    = "Allow"
      Principal = { AWS = [arn] }
      Action    = "kms:Decrypt"
      Resource  = "*"
      Condition = { StringEquals = {
        "kms:ViaService"                   = "s3.${local.region}.amazonaws.com"
        "kms:EncryptionContext:aws:s3:arn" = local.s3_readers[arn]
      } }
    }],
    # ECR は repository の作成者の権限で grant を作り、削除で廃止する（Amazon ECR User Guide「Encryption at rest」）。
    # 管理の委任は CreateGrant を含まないので、作成者の role を名指す。
    # kms:GrantIsForAWSResource は CreateGrant にだけ効く条件キーなので、grant の作成を別の文にする（ADR-0301 案D）。
    length(local.ecr_roles) == 0 ? [] : [{
      Sid       = "RolesViaEcrGrant"
      Effect    = "Allow"
      Principal = { AWS = local.ecr_roles }
      Action    = "kms:CreateGrant"
      Resource  = "*"
      Condition = {
        StringEquals = { "kms:ViaService" = "ecr.${local.region}.amazonaws.com" }
        Bool         = { "kms:GrantIsForAWSResource" = "true" }
      }
    }],
    length(local.ecr_roles) == 0 ? [] : [{
      Sid       = "RolesViaEcr"
      Effect    = "Allow"
      Principal = { AWS = local.ecr_roles }
      Action    = ["kms:DescribeKey", "kms:RetireGrant"]
      Resource  = "*"
      Condition = { StringEquals = { "kms:ViaService" = "ecr.${local.region}.amazonaws.com" } }
    }],
    length(local.log_groups) == 0 ? [] : [{
      Sid       = "LogsService"
      Effect    = "Allow"
      Principal = { Service = "logs.${local.region}.amazonaws.com" }
      Action    = ["kms:Decrypt", "kms:Describe*", "kms:Encrypt", "kms:GenerateDataKey*", "kms:ReEncrypt*"]
      Resource  = "*"
      Condition = { ArnEquals = { "kms:EncryptionContext:aws:logs:arn" = [
        for n in local.log_groups : "arn:${local.partition}:logs:${local.region}:${local.account}:log-group:${n}"
      ] } }
    }],
    # SNS は Topic の ARN を暗号化コンテキスト aws:sns:topicArn に渡す（Amazon SNS Developer Guide「Setting up Amazon SNS topic encryption」）。
    [for i, arn in sort(keys(local.sns_roles)) : {
      Sid       = "RolesViaSns${i}"
      Effect    = "Allow"
      Principal = { AWS = [arn] }
      Action    = ["kms:Decrypt", "kms:GenerateDataKey*"]
      Resource  = "*"
      Condition = { StringEquals = {
        "kms:ViaService"                         = "sns.${local.region}.amazonaws.com"
        "kms:EncryptionContext:aws:sns:topicArn" = [for t in local.sns_roles[arn] : "${local.topic_arn}:${t}"]
      } }
    }],
    # CloudWatch と Budgets は公式の例の aws:SourceArn のワイルドカードを置かず、アカウントの固定値だけで限る（README「公式推奨からの逸脱」）。
    [for p in ["budgets", "cloudwatch", "costalerts"] : {
      Sid       = { budgets = "SnsPublisherBudgets", cloudwatch = "SnsPublisherCloudWatch", costalerts = "SnsPublisherCostAlerts" }[p]
      Effect    = "Allow"
      Principal = { Service = "${p}.amazonaws.com" }
      Action    = ["kms:Decrypt", "kms:GenerateDataKey*"]
      Resource  = "*"
      Condition = { StringEquals = { "aws:SourceAccount" = local.account } }
    } if length(local.sns_publishers[p]) > 0],
    # EventBridge から暗号化した Topic へ送る経路は aws:Source* の条件を受けない（Amazon SNS Developer Guide「Managing Amazon SNS encryption keys and costs」）。
    length(local.sns_publishers.events) == 0 ? [] : [{
      Sid       = "SnsPublisherEvents"
      Effect    = "Allow"
      Principal = { Service = "events.amazonaws.com" }
      Action    = ["kms:Decrypt", "kms:GenerateDataKey*"]
      Resource  = "*"
      Condition = { StringEquals = {
        "kms:EncryptionContext:aws:sns:topicArn" = [for t in local.sns_publishers.events : "${local.topic_arn}:${t}"]
      } }
    }],
    # aws:SourceAccount を足さない。組織の trail では管理アカウントを指し、trail を限らない（README「Key Policy の文書」）。
    length(local.trails) == 0 ? [] : [
      {
        Sid       = "CloudTrailEncrypt"
        Effect    = "Allow"
        Principal = { Service = "cloudtrail.amazonaws.com" }
        Action    = "kms:GenerateDataKey*"
        Resource  = "*"
        Condition = { StringEquals = {
          "aws:SourceArn"                            = local.trails
          "kms:EncryptionContext:aws:cloudtrail:arn" = local.trails
        } }
      },
      {
        Sid       = "CloudTrailDescribeKey"
        Effect    = "Allow"
        Principal = { Service = "cloudtrail.amazonaws.com" }
        Action    = "kms:DescribeKey"
        Resource  = "*"
        Condition = { StringEquals = { "aws:SourceArn" = local.trails } }
      },
    ],
    length(local.config) == 0 ? [] : [{
      Sid       = "ConfigService"
      Effect    = "Allow"
      Principal = { Service = "config.amazonaws.com" }
      Action    = ["kms:Decrypt", "kms:GenerateDataKey"]
      Resource  = "*"
      Condition = { StringEquals = { "aws:SourceAccount" = local.config } }
    }],
  )

  policy = jsonencode({ Version = "2012-10-17", Statement = local.statements })
}

resource "aws_kms_key_policy" "this" {
  key_id                             = aws_kms_key.this.id
  policy                             = local.policy
  bypass_policy_lockout_safety_check = false

  # Key Policy の書き込みが落ちても alias は残す（README「保証しないもの」）。
  depends_on = [aws_kms_alias.this]

  lifecycle {
    # variable の validation が他の object を参照できるのは Terraform 1.9 からで、下限の 1.7.0（versions.tf）では書けない。
    precondition {
      condition     = alltrue([for r in var.users.roles : startswith(r.arn, "arn:${local.partition}:iam::${local.account}:role/")])
      error_message = "users.roles[*].arn は、この鍵と同じアカウントと partition の role にしてください。"
    }

    precondition {
      condition     = alltrue(flatten([for r in var.users.roles : [for o in r.objects : startswith(o, "arn:${local.partition}:s3:::")]]))
      error_message = "users.roles[*].objects は、この鍵と同じ partition の S3 の object にしてください。"
    }

    # 所有は Bucket の名前の account regional namespace の接尾辞で読む。PLAN-KMS-12 と同じ判定なので、片方を変えたらもう片方も変える（README「公式実装の参照」）。
    precondition {
      condition     = alltrue(flatten([for r in var.users.roles : [for o in r.objects : endswith(try(regex("^arn:[^:]+:s3:::([^/]+)/", o)[0], ""), "-${local.account}-${local.region}-an")]]))
      error_message = "users.roles[*].objects は、この鍵のアカウントと region の account regional namespace の Bucket（名前が -<account>-<region>-an で終わる）の object にしてください。"
    }

    precondition {
      condition     = alltrue([for a in var.users.services.cloudtrail : startswith(a, "arn:${local.partition}:cloudtrail:")])
      error_message = "users.services.cloudtrail は、この鍵と同じ partition の trail にしてください。"
    }

    precondition {
      condition     = length(local.policy) <= 32768
      error_message = "Key Policy が 32768 文字を超えます（${length(local.policy)} 文字）。users を減らしてください。"
    }
  }
}
