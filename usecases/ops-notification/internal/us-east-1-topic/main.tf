data "aws_caller_identity" "current" {}
data "aws_partition" "current" {}

locals {
  account   = data.aws_caller_identity.current.account_id
  partition = data.aws_partition.current.partition

  # us-east-1 にしか出ない metric の alarm の通知先（ops-notification/ADR-0001 決定1、repository-wide/ADR-0208）。
  region    = "us-east-1"
  topic_arn = "arn:${local.partition}:sns:${local.region}:${local.account}:${var.name}"
}

# 値は modules/kms と同じにする（repository-wide/ADR-0208 決定2）。
resource "aws_kms_key" "this" {
  region                             = "us-east-1"
  key_usage                          = "ENCRYPT_DECRYPT"
  customer_master_key_spec           = "SYMMETRIC_DEFAULT"
  multi_region                       = false
  is_enabled                         = true
  enable_key_rotation                = true
  rotation_period_in_days            = 365
  deletion_window_in_days            = 30
  bypass_policy_lockout_safety_check = false

  lifecycle {
    precondition {
      condition     = local.partition == "aws"
      error_message = "us-east-1 は aws の partition の region です。このユースケースは aws の partition の外では動きません（ops-notification/ADR-0001 決定1）。"
    }
  }
}

resource "aws_kms_alias" "this" {
  region        = "us-east-1"
  name          = "alias/${var.name}"
  target_key_id = aws_kms_key.this.key_id
}

resource "aws_kms_key_policy" "this" {
  region                             = "us-east-1"
  key_id                             = aws_kms_key.this.id
  policy                             = local.key_policy
  bypass_policy_lockout_safety_check = false

  depends_on = [aws_kms_alias.this]

  lifecycle {
    precondition {
      condition     = startswith(var.heartbeat_role_arn, "arn:${local.partition}:iam::${local.account}:role/")
      error_message = "heartbeat_role_arn が、この鍵と同じアカウントと partition の role ではありません。"
    }
  }
}

resource "aws_sns_topic" "this" {
  region            = "us-east-1"
  name              = var.name
  kms_master_key_id = aws_kms_key.this.arn
  fifo_topic        = false
  signature_version = 2
  tracing_config    = "PassThrough"

  # 鍵を名指す暗号化の設定は、Key Policy が既定から置き換わった後に作る。
  depends_on = [aws_kms_key_policy.this]
}

resource "aws_sns_topic_policy" "this" {
  region = "us-east-1"
  arn    = aws_sns_topic.this.arn
  policy = local.topic_policy
}
