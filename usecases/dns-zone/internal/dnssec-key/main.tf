# kics-scan disable=7ebc9038-0bde-479a-acc4-6ed7b6758899,e592a0c5-5bdb-414c-9066-5dba7cdea370,e38a8e0a-b88b-4902-b3fe-b0fcb17d5c10
# 7ebc9038（鍵の inline の policy）: Key Policy は aws_kms_key_policy が持つ。inline と併用すると互いに上書きする（AWS Provider の aws_kms_key の NOTE）。文書は conftest/plan の plan.kms_key_policy と plan.dns_zone が見る。撤回: KICS が aws_kms_key_policy の文書を見るようになったとき。
# e592a0c5（IAM Access Analyzer）: KICS はファイルごとに analyzer の宣言を求める。analyzer はアカウント単位の統制で account-baseline が持つ（docs/v1.0-plan/scope.md 第7節）。撤回: KICS がファイルの外の宣言を数えるようになったとき。
# e38a8e0a（tag）: tag は共通の単一入力でだけ付ける（ADR-0204 決定5）。この module はその入力を持たない。撤回: 共通の tag 入力を配線したとき。

# Route 53 は DNSSEC の鍵を us-east-1 にしか受けない（Route 53 Developer Guide「Working with customer managed keys for DNSSEC」）。
# region を literal で書くのは egress の宣言の検査が宛先を literal から導くため（dns-zone/ADR-0303）。

data "aws_caller_identity" "current" {}
data "aws_partition" "current" {}

locals {
  account   = data.aws_caller_identity.current.account_id
  partition = data.aws_partition.current.partition

  # modules/kms と同じ委任。暗号操作と kms:CreateGrant を含まない（ADR-0301 決定12）。
  administration_actions = [
    "kms:CancelKeyDeletion",
    "kms:CreateAlias",
    "kms:DeleteAlias",
    "kms:DescribeKey",
    "kms:DisableKey",
    "kms:EnableKey",
    "kms:GetKeyPolicy",
    "kms:ListGrants",
    "kms:ListKeyPolicies",
    "kms:ListResourceTags",
    "kms:PutKeyPolicy",
    "kms:RevokeGrant",
    "kms:ScheduleKeyDeletion",
    "kms:TagResource",
    "kms:UntagResource",
    "kms:UpdateAlias",
    "kms:UpdateKeyDescription",
  ]

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid       = "AccountKeyAdministration"
        Effect    = "Allow"
        Principal = { AWS = "arn:${local.partition}:iam::${local.account}:root" }
        Action    = local.administration_actions
        Resource  = "*"
      },
      # aws:SourceArn を置かない理由は dns-zone/ADR-0302 決定3。
      {
        Sid       = "Route53DnssecService"
        Effect    = "Allow"
        Principal = { Service = "dnssec-route53.amazonaws.com" }
        Action    = ["kms:DescribeKey", "kms:GetPublicKey", "kms:Sign"]
        Resource  = "*"
        Condition = { StringEquals = { "aws:SourceAccount" = local.account } }
      },
      {
        Sid       = "Route53DnssecGrant"
        Effect    = "Allow"
        Principal = { Service = "dnssec-route53.amazonaws.com" }
        Action    = "kms:CreateGrant"
        Resource  = "*"
        Condition = {
          Bool         = { "kms:GrantIsForAWSResource" = "true" }
          StringEquals = { "aws:SourceAccount" = local.account }
        }
      },
    ]
  })
}

# 非対称鍵は自動 rotation を持てない。KSK の rotation は人の手順（README「公式推奨からの逸脱」の KMS.4）。撤回: KMS が非対称鍵の自動 rotation を持ったとき。
# nosemgrep: terraform.aws.security.aws-kms-no-rotation
resource "aws_kms_key" "this" {
  region                             = "us-east-1"
  description                        = "Route 53 DNSSEC KSK for ${var.zone_name}"
  key_usage                          = "SIGN_VERIFY"
  customer_master_key_spec           = "ECC_NIST_P256"
  multi_region                       = false
  is_enabled                         = true
  enable_key_rotation                = false # 非対称鍵は自動 rotation を持てない。KSK の rotation は人の手順（README）
  deletion_window_in_days            = 30
  bypass_policy_lockout_safety_check = false
}

resource "aws_kms_alias" "this" {
  region        = "us-east-1"
  name          = "alias/${var.name}"
  target_key_id = aws_kms_key.this.key_id
}

resource "aws_kms_key_policy" "this" {
  region                             = "us-east-1"
  key_id                             = aws_kms_key.this.id
  policy                             = local.policy
  bypass_policy_lockout_safety_check = false

  depends_on = [aws_kms_alias.this]
}
