# kics-scan disable=7ebc9038-0bde-479a-acc4-6ed7b6758899,e592a0c5-5bdb-414c-9066-5dba7cdea370,e38a8e0a-b88b-4902-b3fe-b0fcb17d5c10,e39f87f5-0abf-488b-864c-63ee1f588140
# 7ebc9038（鍵の inline の policy）: Key Policy は aws_kms_key_policy が持つ。inline と併用すると互いに上書きする（AWS Provider の aws_kms_key の NOTE）。文書は conftest/plan の plan.kms_key_policy が見る。撤回: KICS が aws_kms_key_policy の文書を見るようになったとき。
# e592a0c5（IAM Access Analyzer）: アカウント単位の統制で account-baseline が持つ（docs/v1.0-plan/scope.md 第7節）。撤回: account-baseline を実装したとき。
# e38a8e0a（tag）: tag は共通の単一入力でだけ付ける（ADR-0204 決定5）。この module はその入力を持たない。撤回: 共通の tag 入力を配線したとき。
# e39f87f5（S3 の通知）: SNS の Topic を持つファイルに aws_s3_bucket_notification が無いことを落とす。この Topic は S3 のイベントの送り先ではなく、S3 の通知は Bucket を持つ usecase が持つ。撤回: KICS がこの query を通知の送り先を持つ Bucket に限ったとき。

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
