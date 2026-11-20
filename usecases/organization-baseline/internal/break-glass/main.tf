# kics-scan disable=e592a0c5-5bdb-414c-9066-5dba7cdea370,e38a8e0a-b88b-4902-b3fe-b0fcb17d5c10
# e592a0c5（IAM Access Analyzer）: アカウント単位の統制で account-baseline が持つ（docs/v1.0-plan/scope.md 第7節）。撤回: account-baseline を実装したとき。
# e38a8e0a（tag）: tag は共通の単一入力でだけ付ける（ADR-0204 決定5）。この module はその入力を持たない。撤回: 共通の tag 入力を配線したとき。

data "aws_partition" "current" {}
data "aws_caller_identity" "current" {}
data "aws_region" "current" {}

locals {
  partition = data.aws_partition.current.partition
  # 2つ目は予備（ADR-0301 決定19）。
  user_names = toset(["break-glass-1", "break-glass-2"])
  group_name = "break-glass"

  # 作る前に決まる ARN。deployment-identity の policy が、まだ無い break-glass を名指せる。
  arn_prefix = "arn:${local.partition}:iam::${data.aws_caller_identity.current.account_id}"
  user_arns  = [for n in sort(local.user_names) : "${local.arn_prefix}:user${var.path}${n}"]
  arns = concat(
    local.user_arns,
    ["${local.arn_prefix}:group${var.path}${local.group_name}", "${local.arn_prefix}:role${var.path}${var.role_name}"],
  )
}

# パスワードとハードウェア MFA は Terraform で作らず、人が設定する。
resource "aws_iam_user" "this" {
  #checkov:skip=CKV_AWS_273:Identity Center が使えないときの緊急の経路（ADR-0301 決定19、organization-baseline/ADR-0201）。撤回: break-glass を IAM user で持たなくなったとき
  for_each = local.user_names

  name = each.key
  path = var.path

  lifecycle {
    prevent_destroy = true
  }
}

resource "aws_iam_group" "this" {
  name = local.group_name
  path = var.path
}

resource "aws_iam_group_membership" "this" {
  name  = "break-glass"
  group = aws_iam_group.this.name
  users = [for u in aws_iam_user.this : u.name]
}

resource "aws_iam_group_policy" "this" {
  name  = "break-glass"
  group = aws_iam_group.this.name
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "AssumeBreakGlassRole"
        Effect = "Allow"
        Action = "sts:AssumeRole"
        # メンバーアカウントの ID は、アカウントを作るまで決まらない（organization-baseline/ADR-0201）。
        Resource  = "arn:${local.partition}:iam::*:role${var.path}${var.role_name}"
        Condition = { Bool = { "aws:MultiFactorAuthPresent" = "true" } }
      },
      {
        Sid    = "ManageOwnPasswordAndMfa"
        Effect = "Allow"
        Action = [
          "iam:ChangePassword",
          "iam:EnableMFADevice",
          "iam:GetUser",
          "iam:ListMFADevices",
          "iam:ResyncMFADevice",
        ]
        Resource = "arn:${local.partition}:iam::${data.aws_caller_identity.current.account_id}:user${var.path}$${aws:username}"
      },
      {
        Sid       = "DeactivateOwnMfaWithMfa"
        Effect    = "Allow"
        Action    = "iam:DeactivateMFADevice"
        Resource  = "arn:${local.partition}:iam::${data.aws_caller_identity.current.account_id}:user${var.path}$${aws:username}"
        Condition = { Bool = { "aws:MultiFactorAuthPresent" = "true" } }
      },
      {
        Sid      = "ReadPasswordPolicy"
        Effect   = "Allow"
        Action   = "iam:GetAccountPasswordPolicy"
        Resource = "*"
      },
      {
        Sid      = "DenyLongTermCredentials"
        Effect   = "Deny"
        Action   = ["iam:CreateAccessKey", "iam:CreateServiceSpecificCredential", "iam:UploadSSHPublicKey", "iam:UploadSigningCertificate"]
        Resource = "*"
      },
    ]
  })
}

resource "aws_iam_role" "this" {
  name                 = var.role_name
  path                 = var.path
  max_session_duration = 3600
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { AWS = sort([for u in aws_iam_user.this : u.arn]) }
      Action    = "sts:AssumeRole"
      Condition = { Bool = { "aws:MultiFactorAuthPresent" = "true" } }
    }]
  })

  lifecycle {
    prevent_destroy = true
  }
}

resource "aws_iam_role_policy_attachments_exclusive" "this" {
  role_name   = aws_iam_role.this.name
  policy_arns = ["arn:${local.partition}:iam::aws:policy/AdministratorAccess"]
}

# break-glass の user のパスワードに掛かる（Security Hub IAM.7・IAM.15・IAM.16）。
resource "aws_iam_account_password_policy" "this" {
  minimum_password_length        = 14
  password_reuse_prevention      = 24
  require_lowercase_characters   = true
  require_uppercase_characters   = true
  require_numbers                = true
  require_symbols                = true
  allow_users_to_change_password = true
  max_password_age               = 90
  hard_expiry                    = false
}

# メンバーアカウントの break-glass の role（organization-baseline/ADR-0202）。
resource "aws_cloudformation_stack_set" "this" {
  name             = var.role_name
  description      = "メンバーアカウントの break-glass の role"
  permission_model = "SERVICE_MANAGED"
  capabilities     = ["CAPABILITY_NAMED_IAM"]

  auto_deployment {
    enabled                          = true
    retain_stacks_on_account_removal = false
  }

  managed_execution {
    active = true
  }

  template_body = templatefile("${path.module}/templates/member-break-glass-role.json.tftpl", {
    role_name = var.role_name
    role_path = var.path
    partition = local.partition
    user_arns = local.user_arns
  })

  # 信頼条件が名指す user を先に作る。メンバーの IAM は、無い principal を信頼条件に書けない。
  depends_on = [aws_iam_user.this]

  lifecycle {
    prevent_destroy = true
  }
}

resource "aws_cloudformation_stack_instances" "this" {
  stack_set_name = aws_cloudformation_stack_set.this.name
  regions        = [data.aws_region.current.region]

  deployment_targets {
    organizational_unit_ids = [var.organization_root_id]
  }

  lifecycle {
    prevent_destroy = true
  }
}
