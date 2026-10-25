# kics-scan disable=e592a0c5-5bdb-414c-9066-5dba7cdea370
# e592a0c5（IAM Access Analyzer）: main.tf の同じ抑止と同じ理由と撤回条件。

resource "aws_s3_bucket_server_side_encryption_configuration" "this" {
  bucket = aws_s3_bucket.this.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
    blocked_encryption_types = ["SSE-C"]
  }
}
