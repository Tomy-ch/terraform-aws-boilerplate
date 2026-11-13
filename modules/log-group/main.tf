# kics-scan disable=ef0b316a-211e-42f1-888e-64efe172b755,e38a8e0a-b88b-4902-b3fe-b0fcb17d5c10,e592a0c5-5bdb-414c-9066-5dba7cdea370
# ef0b316a（保持日数）: KICS は変数参照の値を解けず、許容値に無いと判定する。許容値への限定は variables.tf の validation が持つ。撤回: KICS が変数参照の値を解くようになったとき。
# e38a8e0a（tag）: tag は共通の単一入力でだけ付ける（ADR-0204 決定5）。この module はその入力を持たない。撤回: 共通の tag 入力を配線したとき。
# e592a0c5（IAM Access Analyzer）: アカウント単位の統制で account-baseline が持つ（docs/v1.0-plan/scope.md 第7節）。撤回: account-baseline を実装したとき。

data "aws_caller_identity" "current" {}
data "aws_region" "current" {}
data "aws_partition" "current" {}

resource "aws_cloudwatch_log_group" "this" {
  name                        = var.name
  log_group_class             = "STANDARD"
  retention_in_days           = var.retention_days
  kms_key_id                  = var.kms_key_arn
  deletion_protection_enabled = var.deletion_protection
  skip_destroy                = false

  lifecycle {
    # validation が他の object を参照できるのは Terraform 1.9 からで、下限の 1.7 では precondition に置く。
    precondition {
      condition     = startswith(var.kms_key_arn, "arn:${data.aws_partition.current.partition}:kms:${data.aws_region.current.region}:${data.aws_caller_identity.current.account_id}:key/")
      error_message = "kms_key_arn が、Log Group と同じ partition・リージョン・アカウントの鍵ではありません。"
    }
  }
}
