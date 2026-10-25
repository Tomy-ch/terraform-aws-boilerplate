# kics-scan disable=e592a0c5-5bdb-414c-9066-5dba7cdea370
# e592a0c5（IAM Access Analyzer）: アカウント単位の統制で account-baseline が持つ（docs/v1.0-plan/scope.md 第7節）。撤回: account-baseline を実装したとき。

resource "aws_s3_bucket_server_side_encryption_configuration" "this" {
  bucket = aws_s3_bucket.this.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
    blocked_encryption_types = ["SSE-C"]
  }
}
