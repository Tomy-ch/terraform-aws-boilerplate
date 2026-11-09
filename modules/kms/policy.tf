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

  s3_roles   = sort(distinct([for r in var.users.roles : r.arn if r.via == "s3"]))
  log_groups = sort(distinct(var.users.services.logs))

  statements = concat(
    [{
      Sid       = "AccountKeyAdministration"
      Effect    = "Allow"
      Principal = { AWS = "arn:${local.partition}:iam::${local.account}:root" }
      Action    = local.administration_actions
      Resource  = "*"
    }],
    length(local.s3_roles) == 0 ? [] : [{
      Sid       = "RolesViaS3"
      Effect    = "Allow"
      Principal = { AWS = local.s3_roles }
      Action    = ["kms:Decrypt", "kms:GenerateDataKey"]
      Resource  = "*"
      Condition = { StringEquals = { "kms:ViaService" = "s3.${local.region}.amazonaws.com" } }
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
      condition     = length(local.policy) <= 32768
      error_message = "Key Policy が 32768 文字を超えます（${length(local.policy)} 文字）。users を減らしてください。"
    }
  }
}
