# kics-scan disable=e592a0c5-5bdb-414c-9066-5dba7cdea370,4bc4dd4c-7d8d-405e-a0fb-57fa4c31b4d9
# e592a0c5（IAM Access Analyzer）: アカウント単位の統制で account-baseline が持つ（docs/v1.0-plan/scope.md 第7節）。撤回: account-baseline を実装したとき。
# 4bc4dd4c（HTTP の要求を受ける）: Statement を concat で組むので KICS が TLS の Deny を読めない。TLS の Deny は Unit Test（tests/policy.tftest.hcl）が文書の完全一致で固定する。撤回: KICS が concat で組んだ Statement を読めるようになったとき、または Statement の組み方を変えたとき。

locals {
  writer_roles = sort(distinct([for w in var.writers : w.role_arn]))

  # 決定は usecases/state-backend/docs/adr/0201-plan-output-bucket.md 決定8。
  writer_statements = [
    for i, arn in local.writer_roles : {
      Sid         = "DenyObjectWriteOutsidePrefixes${i}"
      Effect      = "Deny"
      Principal   = "*"
      Action      = ["s3:PutObject", "s3:ReplicateObject"]
      NotResource = sort([for w in var.writers : "${aws_s3_bucket.this.arn}/${w.key_prefix}*" if w.role_arn == arn])
      Condition   = { ArnEquals = { "aws:PrincipalArn" = arn } }
    }
  ]
}

resource "aws_s3_bucket_policy" "this" {
  bucket = aws_s3_bucket.this.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = concat(
      [
        {
          Sid       = "DenyInsecureTransport"
          Effect    = "Deny"
          Principal = "*"
          Action    = "s3:*"
          Resource  = [aws_s3_bucket.this.arn, "${aws_s3_bucket.this.arn}/*"]
          Condition = { Bool = { "aws:SecureTransport" = "false" } }
        },
        {
          Sid       = "DenyObjectWriteByOthers"
          Effect    = "Deny"
          Principal = "*"
          Action    = ["s3:PutObject", "s3:ReplicateObject"]
          Resource  = "${aws_s3_bucket.this.arn}/*"
          Condition = { ArnNotEquals = { "aws:PrincipalArn" = local.writer_roles } }
        },
        {
          Sid       = "DenyObjectDeletion"
          Effect    = "Deny"
          Principal = "*"
          Action    = ["s3:DeleteObject", "s3:DeleteObjectVersion", "s3:ReplicateDelete"]
          Resource  = "${aws_s3_bucket.this.arn}/*"
        },
      ],
      local.writer_statements,
    )
  })

  # 同じ Bucket への PutBucketPolicy と PutPublicAccessBlock の並行は OperationAborted で落ちうる。
  depends_on = [aws_s3_bucket_public_access_block.this, aws_s3_bucket_ownership_controls.this]
}
