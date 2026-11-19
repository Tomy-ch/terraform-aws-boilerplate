# kics-scan disable=f861041c-8c9f-4156-acfc-5e6e524f5884,e38a8e0a-b88b-4902-b3fe-b0fcb17d5c10
# f861041c（アクセスログ）: S3 サーバーアクセスログは CloudWatch Logs へ配送する（usecases/audit-evidence/docs/adr/0104-server-access-logs-to-cloudwatch.md）。撤回: KICS が CloudWatch Logs への配送を認めたとき。
# e38a8e0a（tag）: tag は共通の単一入力でだけ付ける（ADR-0204 決定5）。この module はその入力を持たない。撤回: 共通の tag 入力を配線したとき。

data "aws_caller_identity" "current" {}
data "aws_region" "current" {}
data "aws_partition" "current" {}

locals {
  bucket_name = "${var.name}-${data.aws_caller_identity.current.account_id}-${data.aws_region.current.region}-an"
}

# 版管理は aws_s3_bucket_versioning.this が有効にする。このルールは非推奨の inline の versioning しか認めない。撤回: ルールが分離した resource を認めたとき。
# nosemgrep: terraform.aws.best-practice.aws-s3-bucket-versioning-not-enabled
resource "aws_s3_bucket" "this" {
  #checkov:skip=CKV_AWS_18:S3 サーバーアクセスログは CloudWatch Logs へ配送する（usecases/audit-evidence/docs/adr/0104-server-access-logs-to-cloudwatch.md）。撤回: Checkov が CloudWatch Logs への配送を認めたとき
  #checkov:skip=CKV2_AWS_62:event notification を持たない（usecases/audit-evidence/docs/adr/0103-deviations-replication-mfa-delete-notifications.md）。撤回: 同 ADR が変わったとき
  #checkov:skip=CKV_AWS_144:リージョン間の複製を持たない（usecases/audit-evidence/docs/adr/0103-deviations-replication-mfa-delete-notifications.md）。撤回: 同 ADR が変わったとき
  bucket           = local.bucket_name
  bucket_namespace = "account-regional"
  # Object Lock は Bucket の作成時にしか有効にできず、有効にした後は止められない（Amazon S3 User Guide「Object Lock considerations」）。
  object_lock_enabled = true
  force_destroy       = var.force_destroy

  lifecycle {
    # variable の validation が他の object を参照できるのは Terraform 1.9 からで、下限の 1.7.0（versions.tf）では書けない。
    precondition {
      condition     = length(local.bucket_name) <= 63
      error_message = "Bucket の完全な名前（${local.bucket_name}）が63文字を超えます。name を短くしてください。"
    }
  }
}

resource "aws_s3_bucket_public_access_block" "this" {
  bucket = aws_s3_bucket.this.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_ownership_controls" "this" {
  bucket = aws_s3_bucket.this.id

  rule {
    object_ownership = "BucketOwnerEnforced"
  }
}

resource "aws_s3_bucket_versioning" "this" {
  bucket = aws_s3_bucket.this.id

  versioning_configuration {
    status = "Enabled"
  }
}

# 既定の保持は必須。CloudTrail・Config は保持を指定しない（audit-evidence/ADR-0101 決定2）。
resource "aws_s3_bucket_object_lock_configuration" "this" {
  bucket = aws_s3_bucket.this.id

  rule {
    default_retention {
      mode = var.retention.mode
      days = var.retention.days
    }
  }

  depends_on = [aws_s3_bucket_versioning.this]
}
