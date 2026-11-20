# kics-scan disable=7ebc9038-0bde-479a-acc4-6ed7b6758899,ef0b316a-211e-42f1-888e-64efe172b755,e38a8e0a-b88b-4902-b3fe-b0fcb17d5c10,e592a0c5-5bdb-414c-9066-5dba7cdea370
# 7ebc9038（鍵の inline の policy）: Key Policy は aws_kms_key_policy が持つ。inline と併用すると互いに上書きする（AWS Provider の aws_kms_key の NOTE）。文書は conftest/plan の plan.kms_key_policy が見る。撤回: KICS が aws_kms_key_policy の文書を見るようになったとき。
# ef0b316a（保持日数）: KICS は変数参照の値を解けず、許容値に無いと判定する。許容値への限定は usecase の variables.tf の validation が持つ。撤回: KICS が変数参照の値を解くようになったとき。
# e38a8e0a（tag）: tag は共通の単一入力でだけ付ける（ADR-0204 決定5）。この module はその入力を持たない。撤回: 共通の tag 入力を配線したとき。
# e592a0c5（IAM Access Analyzer）: アカウント単位の統制で account-baseline が持つ（docs/v1.0-plan/scope.md 第7節）。撤回: account-baseline を実装したとき。

# Route 53 は公開の Hosted Zone のクエリログを us-east-1 の Log Group にしか送らない（Route 53 Developer Guide「Public DNS query logging」）。
# region を literal で書くのは egress の宣言の検査が宛先を literal から導くため（dns-zone/ADR-0303）。

data "aws_caller_identity" "current" {}
data "aws_partition" "current" {}

locals {
  account        = data.aws_caller_identity.current.account_id
  partition      = data.aws_partition.current.partition
  log_group_name = "/aws/route53/${var.zone_name}"
  log_group_arn  = "arn:${local.partition}:logs:us-east-1:${local.account}:log-group:${local.log_group_name}"

  # modules/kms と同じ委任。暗号操作と kms:CreateGrant を含まない（ADR-0301 決定12）。
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
}

resource "aws_kms_key" "this" {
  region                             = "us-east-1"
  description                        = "Route 53 query logs for ${var.zone_name}"
  key_usage                          = "ENCRYPT_DECRYPT"
  customer_master_key_spec           = "SYMMETRIC_DEFAULT"
  multi_region                       = false
  is_enabled                         = true
  enable_key_rotation                = true
  rotation_period_in_days            = 365
  deletion_window_in_days            = 30
  bypass_policy_lockout_safety_check = false
}

resource "aws_kms_alias" "this" {
  region        = "us-east-1"
  name          = "alias/${var.name}-dns-query-logs"
  target_key_id = aws_kms_key.this.key_id
}

resource "aws_kms_key_policy" "this" {
  region = "us-east-1"
  key_id = aws_kms_key.this.id
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
      {
        Sid       = "LogsService"
        Effect    = "Allow"
        Principal = { Service = "logs.us-east-1.amazonaws.com" }
        Action    = ["kms:Decrypt", "kms:Describe*", "kms:Encrypt", "kms:GenerateDataKey*", "kms:ReEncrypt*"]
        Resource  = "*"
        Condition = { ArnEquals = { "kms:EncryptionContext:aws:logs:arn" = [local.log_group_arn] } }
      },
    ]
  })
  bypass_policy_lockout_safety_check = false

  depends_on = [aws_kms_alias.this]
}

resource "aws_cloudwatch_log_group" "this" {
  region                      = "us-east-1"
  name                        = local.log_group_name
  log_group_class             = "STANDARD"
  retention_in_days           = var.retention_days
  kms_key_id                  = aws_kms_key.this.arn
  deletion_protection_enabled = var.deletion_protection
  skip_destroy                = false

  # Log Group の作成は、CloudWatch Logs に鍵を許す Key Policy の後にする。
  depends_on = [aws_kms_key_policy.this]
}

# Route 53 のクエリログは、アカウント単位の resource policy（policy_name）が許す Log Group にしか書けない。
# 枠はリージョンに10個までの共有の資源で、この usecase が us-east-1 の1つを持つ（README「所有者」）。
resource "aws_cloudwatch_log_resource_policy" "this" {
  region      = "us-east-1"
  policy_name = "${var.name}-dns-query-logs"
  policy_document = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Sid       = "Route53QueryLogs"
      Effect    = "Allow"
      Principal = { Service = "route53.amazonaws.com" }
      Action    = ["logs:CreateLogStream", "logs:PutLogEvents"]
      Resource  = "${aws_cloudwatch_log_group.this.arn}:*"
      Condition = { StringEquals = { "aws:SourceAccount" = local.account } }
    }]
  })
}
