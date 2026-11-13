# kics-scan disable=e592a0c5-5bdb-414c-9066-5dba7cdea370,4bc4dd4c-7d8d-405e-a0fb-57fa4c31b4d9
# e592a0c5（IAM Access Analyzer）: アカウント単位の統制で account-baseline が持つ（docs/v1.0-plan/scope.md 第7節）。撤回: account-baseline を実装したとき。
# 4bc4dd4c（HTTP の要求を受ける）: Statement を concat で組むので KICS が TLS の Deny を読めない。Deny の在ることは tests/policy.tftest.hcl と Policy Test の PLAN-SBW-08 が、Deny 以外の文の無いことは PLAN-SBW-09 が見る。撤回: KICS が concat を評価するか、Statement を literal の列で書けるようになったとき。

locals {
  # Resource は object の ARN だけにし、Bucket の設定の Action を含めない。拒否を誤っても Bucket Policy を書き換えられる主体が直せる。
  object_write_actions = ["s3:DeleteObject", "s3:DeleteObjectVersion", "s3:PutObject", "s3:ReplicateDelete", "s3:ReplicateObject"]
  access_actions = {
    write  = ["s3:DeleteObject", "s3:PutObject"]
    delete = ["s3:DeleteObject"]
  }

  bootstrap_executor_arn = var.bootstrap_executor == null ? null : "arn:${data.aws_partition.current.partition}:iam::${data.aws_caller_identity.current.account_id}:user/${var.bootstrap_executor.user_name}"

  object_writers = merge(
    { for k, w in var.object_writers : k => { principal_arn = w.role_arn, keys = w.keys, access = w.access } },
    { for e in var.bootstrap_executor == null ? [] : [var.bootstrap_executor] : "bootstrapExecutor" => { principal_arn = local.bootstrap_executor_arn, keys = e.keys, access = "write" } },
  )

  writer_statements = flatten([
    for k, w in local.object_writers : [
      {
        Sid         = "DenyObjectWriteOutsideKeys${upper(substr(k, 0, 1))}${substr(k, 1, -1)}"
        Effect      = "Deny"
        Principal   = "*"
        Action      = local.object_write_actions
        NotResource = sort([for key in w.keys : "${aws_s3_bucket.this.arn}/${key}"])
        Condition   = { ArnEquals = { "aws:PrincipalArn" = w.principal_arn } }
      },
      {
        Sid       = "DenyObjectActionsOf${upper(substr(k, 0, 1))}${substr(k, 1, -1)}"
        Effect    = "Deny"
        Principal = "*"
        Action    = sort(setsubtract(local.object_write_actions, local.access_actions[w.access]))
        Resource  = sort([for key in w.keys : "${aws_s3_bucket.this.arn}/${key}"])
        Condition = { ArnEquals = { "aws:PrincipalArn" = w.principal_arn } }
      },
    ]
  ])

  # 否定の演算子は条件キーが無い要求で真になるので、Null で header が在ることを確かめてから値を比べる。
  encryption_statements = [
    {
      Sid       = "DenyObjectEncryptionOtherThanKms"
      Effect    = "Deny"
      Principal = "*"
      Action    = "s3:PutObject"
      Resource  = "${aws_s3_bucket.this.arn}/*"
      Condition = merge(
        {
          Null            = { "s3:x-amz-server-side-encryption" = "false" }
          StringNotEquals = { "s3:x-amz-server-side-encryption" = "aws:kms" }
        },
        { for arn in local.bootstrap_executor_arn == null ? [] : [local.bootstrap_executor_arn] : "ArnNotEquals" => { "aws:PrincipalArn" = arn } },
      )
    },
    {
      Sid       = "DenyObjectEncryptionWithOtherKey"
      Effect    = "Deny"
      Principal = "*"
      Action    = "s3:PutObject"
      Resource  = "${aws_s3_bucket.this.arn}/*"
      Condition = {
        Null            = { "s3:x-amz-server-side-encryption-aws-kms-key-id" = "false" }
        StringNotEquals = { "s3:x-amz-server-side-encryption-aws-kms-key-id" = var.kms_key_arn }
      }
    },
    {
      Sid       = "DenyObjectEncryptionWithManagedKey"
      Effect    = "Deny"
      Principal = "*"
      Action    = "s3:PutObject"
      Resource  = "${aws_s3_bucket.this.arn}/*"
      Condition = {
        StringEquals = { "s3:x-amz-server-side-encryption" = "aws:kms" }
        Null         = { "s3:x-amz-server-side-encryption-aws-kms-key-id" = "true" }
      }
    },
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
          Action    = local.object_write_actions
          Resource  = "${aws_s3_bucket.this.arn}/*"
          Condition = { ArnNotEquals = { "aws:PrincipalArn" = sort([for w in values(local.object_writers) : w.principal_arn]) } }
        },
      ],
      local.writer_statements,
      local.encryption_statements,
    )
  })
}
