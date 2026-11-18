# kics-scan disable=e592a0c5-5bdb-414c-9066-5dba7cdea370,e38a8e0a-b88b-4902-b3fe-b0fcb17d5c10,e39f87f5-0abf-488b-864c-63ee1f588140
# e592a0c5（IAM Access Analyzer）: アカウント単位の統制で account-baseline が持つ（docs/v1.0-plan/scope.md 第7節）。撤回: account-baseline を実装したとき。
# e38a8e0a（tag）: tag は共通の単一入力でだけ付ける（ADR-0204 決定5）。この module はその入力を持たない。撤回: 共通の tag 入力を配線したとき。
# e39f87f5（S3 の通知）: SNS の Topic を持つファイルに aws_s3_bucket_notification が無いことを落とす。この Topic は S3 のイベントの送り先ではなく、S3 の通知は Bucket を持つ usecase が持つ。撤回: KICS がこの query を通知の送り先を持つ Bucket に限ったとき。

data "aws_caller_identity" "current" {}
data "aws_region" "current" {}
data "aws_partition" "current" {}

resource "aws_sns_topic" "this" {
  name              = var.name
  kms_master_key_id = var.kms_key_arn
  fifo_topic        = false
  signature_version = 2
  tracing_config    = "PassThrough"

  lifecycle {
    # validation が他の object を参照できるのは Terraform 1.9 からで、下限の 1.7 では precondition に置く。
    precondition {
      condition     = startswith(var.kms_key_arn, "arn:${data.aws_partition.current.partition}:kms:${data.aws_region.current.region}:${data.aws_caller_identity.current.account_id}:key/")
      error_message = "kms_key_arn が、Topic と同じ partition・リージョン・アカウントの鍵ではありません。"
    }
  }
}
