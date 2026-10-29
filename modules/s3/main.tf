# kics-scan disable=f861041c-8c9f-4156-acfc-5e6e524f5884,c5b31ab9-0f26-4a49-b8aa-4cc064392f4d,e592a0c5-5bdb-414c-9066-5dba7cdea370,e38a8e0a-b88b-4902-b3fe-b0fcb17d5c10
# f861041c（アクセスログ）: 配送は state-backend の段5 で足す（README の「まだ持たないもの」）。撤回: 段5 で足したとき。
# c5b31ab9（MFA delete）: 掛けない（usecases/state-backend/docs/adr/0102-no-mfa-delete-or-object-lock.md）。撤回: 同 ADR が変わったとき。
# e592a0c5（IAM Access Analyzer）: アカウント単位の統制で account-baseline が持つ（docs/v1.0-plan/scope.md 第7節）。撤回: account-baseline を実装したとき。
# e38a8e0a（tag）: tag は共通の単一入力でだけ付ける（ADR-0204 決定5）。この module はその入力を持たない。撤回: 共通の tag 入力を配線したとき。

data "aws_caller_identity" "current" {}
data "aws_region" "current" {}

locals {
  bucket_name = "${var.name}-${data.aws_caller_identity.current.account_id}-${data.aws_region.current.region}-an"
}

# 版管理は aws_s3_bucket_versioning.this が有効にする。このルールは非推奨の inline の versioning しか認めない。撤回: ルールが分離した resource を認めたとき。
# nosemgrep: terraform.aws.best-practice.aws-s3-bucket-versioning-not-enabled
resource "aws_s3_bucket" "this" {
  #checkov:skip=CKV_AWS_145:段5 まで SSE-S3（usecases/state-backend/docs/adr/0101-sse-s3-until-cmk.md）。撤回: 段5 で SSE-KMS に切り替えたとき
  #checkov:skip=CKV_AWS_18:アクセスログの配送は段5 で足す（README）。撤回: 段5 で足したとき
  #checkov:skip=CKV2_AWS_61:lifecycle は段5 で足す（README）。撤回: 段5 で足したとき
  #checkov:skip=CKV2_AWS_62:通知はこの module に足さない（README、root の ADR-0102 決定15・18）。撤回: 通知をこの module の責務にしたとき
  #checkov:skip=CKV_AWS_144:リージョン間の複製は state-backend の責務外（usecases/state-backend/README.md）。撤回: 責務に入れたとき
  bucket           = local.bucket_name
  bucket_namespace = "account-regional"
  force_destroy    = false

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
