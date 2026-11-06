# kics-scan disable=7ebc9038-0bde-479a-acc4-6ed7b6758899,e592a0c5-5bdb-414c-9066-5dba7cdea370,e38a8e0a-b88b-4902-b3fe-b0fcb17d5c10
# 7ebc9038（鍵の inline の policy）: Key Policy は aws_kms_key_policy が持つ。inline と併用すると互いに上書きする（AWS Provider の aws_kms_key の NOTE）。文書は conftest/plan の plan.kms_key_policy が見る。撤回: KICS が aws_kms_key_policy の文書を見るようになったとき。
# e592a0c5（IAM Access Analyzer）: アカウント単位の統制で account-baseline が持つ（docs/v1.0-plan/scope.md 第7節）。撤回: account-baseline を実装したとき。
# e38a8e0a（tag）: tag は共通の単一入力でだけ付ける（ADR-0204 決定5）。この module はその入力を持たない。撤回: 共通の tag 入力を配線したとき。

data "aws_caller_identity" "current" {}
data "aws_region" "current" {}
data "aws_partition" "current" {}

resource "aws_kms_key" "this" {
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
  name          = "alias/${var.name}"
  target_key_id = aws_kms_key.this.key_id
}
