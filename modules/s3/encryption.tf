# kics-scan disable=e592a0c5-5bdb-414c-9066-5dba7cdea370
# e592a0c5（IAM Access Analyzer）: アカウント単位の統制で account-baseline が持つ（docs/v1.0-plan/scope.md 第7節）。撤回: account-baseline を実装したとき。

resource "aws_s3_bucket_server_side_encryption_configuration" "this" {
  bucket = aws_s3_bucket.this.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm     = "aws:kms"
      kms_master_key_id = var.kms_key_arn
    }
    bucket_key_enabled       = false
    blocked_encryption_types = ["SSE-C"]
  }

  lifecycle {
    # S3 は PutBucketEncryption で鍵を検証しないので、別の場所の鍵は書き込みまで落ちない。
    precondition {
      condition     = startswith(var.kms_key_arn, "arn:${data.aws_partition.current.partition}:kms:${data.aws_region.current.region}:${data.aws_caller_identity.current.account_id}:key/")
      error_message = "kms_key_arn が、Bucket と同じ partition・リージョン・アカウントの鍵ではありません。"
    }
  }
}
