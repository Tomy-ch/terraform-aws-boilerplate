mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
  mock_data "aws_region" {
    defaults = { region = "ap-northeast-1" }
  }
  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }
}

variables {
  name                              = "tabp-admin-tfstate"
  kms_key_arn                       = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  noncurrent_version_retention_days = 90
  object_writers = {
    identity = {
      role_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
      keys     = ["identity/terraform.tfstate", "identity/terraform.tfstate.tflock"]
      access   = "write"
    }
    workload = {
      role_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply"
      keys     = ["workload/terraform.tfstate", "workload/terraform.tfstate.tflock"]
      access   = "write"
    }
    identityUnlock = {
      role_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"
      keys     = ["identity/terraform.tfstate.tflock"]
      access   = "delete"
    }
  }
}

# Bucket の ARN は plan では決まらないので、mock の apply で確かめる。
run "statements" {
  command = apply

  assert {
    condition = jsondecode(aws_s3_bucket_policy.this.policy) == {
      Version = "2012-10-17"
      Statement = [
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
          Action    = ["s3:DeleteObject", "s3:DeleteObjectVersion", "s3:PutObject", "s3:ReplicateDelete", "s3:ReplicateObject"]
          Resource  = "${aws_s3_bucket.this.arn}/*"
          Condition = { ArnNotEquals = { "aws:PrincipalArn" = ["arn:aws:iam::123456789012:role/tabp-admin-ci-apply", "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity", "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"] } }
        },
        {
          Sid         = "DenyObjectWriteOutsideKeysIdentity"
          Effect      = "Deny"
          Principal   = "*"
          Action      = ["s3:DeleteObject", "s3:DeleteObjectVersion", "s3:PutObject", "s3:ReplicateDelete", "s3:ReplicateObject"]
          NotResource = ["${aws_s3_bucket.this.arn}/identity/terraform.tfstate", "${aws_s3_bucket.this.arn}/identity/terraform.tfstate.tflock"]
          Condition   = { ArnEquals = { "aws:PrincipalArn" = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity" } }
        },
        {
          Sid       = "DenyObjectActionsOfIdentity"
          Effect    = "Deny"
          Principal = "*"
          Action    = ["s3:DeleteObjectVersion", "s3:ReplicateDelete", "s3:ReplicateObject"]
          Resource  = ["${aws_s3_bucket.this.arn}/identity/terraform.tfstate", "${aws_s3_bucket.this.arn}/identity/terraform.tfstate.tflock"]
          Condition = { ArnEquals = { "aws:PrincipalArn" = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity" } }
        },
        {
          Sid         = "DenyObjectWriteOutsideKeysIdentityUnlock"
          Effect      = "Deny"
          Principal   = "*"
          Action      = ["s3:DeleteObject", "s3:DeleteObjectVersion", "s3:PutObject", "s3:ReplicateDelete", "s3:ReplicateObject"]
          NotResource = ["${aws_s3_bucket.this.arn}/identity/terraform.tfstate.tflock"]
          Condition   = { ArnEquals = { "aws:PrincipalArn" = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity" } }
        },
        {
          Sid       = "DenyObjectActionsOfIdentityUnlock"
          Effect    = "Deny"
          Principal = "*"
          Action    = ["s3:DeleteObjectVersion", "s3:PutObject", "s3:ReplicateDelete", "s3:ReplicateObject"]
          Resource  = ["${aws_s3_bucket.this.arn}/identity/terraform.tfstate.tflock"]
          Condition = { ArnEquals = { "aws:PrincipalArn" = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity" } }
        },
        {
          Sid         = "DenyObjectWriteOutsideKeysWorkload"
          Effect      = "Deny"
          Principal   = "*"
          Action      = ["s3:DeleteObject", "s3:DeleteObjectVersion", "s3:PutObject", "s3:ReplicateDelete", "s3:ReplicateObject"]
          NotResource = ["${aws_s3_bucket.this.arn}/workload/terraform.tfstate", "${aws_s3_bucket.this.arn}/workload/terraform.tfstate.tflock"]
          Condition   = { ArnEquals = { "aws:PrincipalArn" = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply" } }
        },
        {
          Sid       = "DenyObjectActionsOfWorkload"
          Effect    = "Deny"
          Principal = "*"
          Action    = ["s3:DeleteObjectVersion", "s3:ReplicateDelete", "s3:ReplicateObject"]
          Resource  = ["${aws_s3_bucket.this.arn}/workload/terraform.tfstate", "${aws_s3_bucket.this.arn}/workload/terraform.tfstate.tflock"]
          Condition = { ArnEquals = { "aws:PrincipalArn" = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply" } }
        },
        {
          Sid       = "DenyObjectEncryptionOtherThanKms"
          Effect    = "Deny"
          Principal = "*"
          Action    = "s3:PutObject"
          Resource  = "${aws_s3_bucket.this.arn}/*"
          Condition = {
            Null            = { "s3:x-amz-server-side-encryption" = "false" }
            StringNotEquals = { "s3:x-amz-server-side-encryption" = "aws:kms" }
          }
        },
        {
          Sid       = "DenyObjectEncryptionWithOtherKey"
          Effect    = "Deny"
          Principal = "*"
          Action    = "s3:PutObject"
          Resource  = "${aws_s3_bucket.this.arn}/*"
          Condition = {
            Null            = { "s3:x-amz-server-side-encryption-aws-kms-key-id" = "false" }
            StringNotEquals = { "s3:x-amz-server-side-encryption-aws-kms-key-id" = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab" }
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
    error_message = "Bucket Policy が、TLS の拒否、writer 以外の書き込みの拒否、writer ごとの key と操作の閉じ込め、state の鍵以外の暗号化の拒否の文書になっていません。"
  }

  assert {
    condition     = aws_s3_bucket_policy.this.bucket == aws_s3_bucket.this.id
    error_message = "Bucket Policy が、この module の Bucket に付いていません。"
  }

  assert {
    condition = output.object_writers == {
      identity       = { principal_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity", keys = toset(["identity/terraform.tfstate", "identity/terraform.tfstate.tflock"]), access = "write" }
      identityUnlock = { principal_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity", keys = toset(["identity/terraform.tfstate.tflock"]), access = "delete" }
      workload       = { principal_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply", keys = toset(["workload/terraform.tfstate", "workload/terraform.tfstate.tflock"]), access = "write" }
    }
    error_message = "object_writers が、Bucket Policy が書き込みを許す主体を返していません。"
  }
}

# bootstrap の実行者は、identity root の apply の role と同じ key と操作を持ち、暗号化の方式の header の拒否の例外にも入る。
run "statements_with_bootstrap_executor" {
  command = apply

  variables {
    bootstrap_executor = {
      user_name = "tabp-bootstrap"
      keys      = ["identity/terraform.tfstate", "identity/terraform.tfstate.tflock"]
    }
  }

  assert {
    condition = jsondecode(aws_s3_bucket_policy.this.policy) == {
      Version = "2012-10-17"
      Statement = [
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
          Action    = ["s3:DeleteObject", "s3:DeleteObjectVersion", "s3:PutObject", "s3:ReplicateDelete", "s3:ReplicateObject"]
          Resource  = "${aws_s3_bucket.this.arn}/*"
          Condition = { ArnNotEquals = { "aws:PrincipalArn" = ["arn:aws:iam::123456789012:role/tabp-admin-ci-apply", "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity", "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity", "arn:aws:iam::123456789012:user/tabp-bootstrap"] } }
        },
        {
          Sid         = "DenyObjectWriteOutsideKeysBootstrapExecutor"
          Effect      = "Deny"
          Principal   = "*"
          Action      = ["s3:DeleteObject", "s3:DeleteObjectVersion", "s3:PutObject", "s3:ReplicateDelete", "s3:ReplicateObject"]
          NotResource = ["${aws_s3_bucket.this.arn}/identity/terraform.tfstate", "${aws_s3_bucket.this.arn}/identity/terraform.tfstate.tflock"]
          Condition   = { ArnEquals = { "aws:PrincipalArn" = "arn:aws:iam::123456789012:user/tabp-bootstrap" } }
        },
        {
          Sid       = "DenyObjectActionsOfBootstrapExecutor"
          Effect    = "Deny"
          Principal = "*"
          Action    = ["s3:DeleteObjectVersion", "s3:ReplicateDelete", "s3:ReplicateObject"]
          Resource  = ["${aws_s3_bucket.this.arn}/identity/terraform.tfstate", "${aws_s3_bucket.this.arn}/identity/terraform.tfstate.tflock"]
          Condition = { ArnEquals = { "aws:PrincipalArn" = "arn:aws:iam::123456789012:user/tabp-bootstrap" } }
        },
        {
          Sid         = "DenyObjectWriteOutsideKeysIdentity"
          Effect      = "Deny"
          Principal   = "*"
          Action      = ["s3:DeleteObject", "s3:DeleteObjectVersion", "s3:PutObject", "s3:ReplicateDelete", "s3:ReplicateObject"]
          NotResource = ["${aws_s3_bucket.this.arn}/identity/terraform.tfstate", "${aws_s3_bucket.this.arn}/identity/terraform.tfstate.tflock"]
          Condition   = { ArnEquals = { "aws:PrincipalArn" = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity" } }
        },
        {
          Sid       = "DenyObjectActionsOfIdentity"
          Effect    = "Deny"
          Principal = "*"
          Action    = ["s3:DeleteObjectVersion", "s3:ReplicateDelete", "s3:ReplicateObject"]
          Resource  = ["${aws_s3_bucket.this.arn}/identity/terraform.tfstate", "${aws_s3_bucket.this.arn}/identity/terraform.tfstate.tflock"]
          Condition = { ArnEquals = { "aws:PrincipalArn" = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity" } }
        },
        {
          Sid         = "DenyObjectWriteOutsideKeysIdentityUnlock"
          Effect      = "Deny"
          Principal   = "*"
          Action      = ["s3:DeleteObject", "s3:DeleteObjectVersion", "s3:PutObject", "s3:ReplicateDelete", "s3:ReplicateObject"]
          NotResource = ["${aws_s3_bucket.this.arn}/identity/terraform.tfstate.tflock"]
          Condition   = { ArnEquals = { "aws:PrincipalArn" = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity" } }
        },
        {
          Sid       = "DenyObjectActionsOfIdentityUnlock"
          Effect    = "Deny"
          Principal = "*"
          Action    = ["s3:DeleteObjectVersion", "s3:PutObject", "s3:ReplicateDelete", "s3:ReplicateObject"]
          Resource  = ["${aws_s3_bucket.this.arn}/identity/terraform.tfstate.tflock"]
          Condition = { ArnEquals = { "aws:PrincipalArn" = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity" } }
        },
        {
          Sid         = "DenyObjectWriteOutsideKeysWorkload"
          Effect      = "Deny"
          Principal   = "*"
          Action      = ["s3:DeleteObject", "s3:DeleteObjectVersion", "s3:PutObject", "s3:ReplicateDelete", "s3:ReplicateObject"]
          NotResource = ["${aws_s3_bucket.this.arn}/workload/terraform.tfstate", "${aws_s3_bucket.this.arn}/workload/terraform.tfstate.tflock"]
          Condition   = { ArnEquals = { "aws:PrincipalArn" = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply" } }
        },
        {
          Sid       = "DenyObjectActionsOfWorkload"
          Effect    = "Deny"
          Principal = "*"
          Action    = ["s3:DeleteObjectVersion", "s3:ReplicateDelete", "s3:ReplicateObject"]
          Resource  = ["${aws_s3_bucket.this.arn}/workload/terraform.tfstate", "${aws_s3_bucket.this.arn}/workload/terraform.tfstate.tflock"]
          Condition = { ArnEquals = { "aws:PrincipalArn" = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply" } }
        },
        {
          Sid       = "DenyObjectEncryptionOtherThanKms"
          Effect    = "Deny"
          Principal = "*"
          Action    = "s3:PutObject"
          Resource  = "${aws_s3_bucket.this.arn}/*"
          Condition = {
            Null            = { "s3:x-amz-server-side-encryption" = "false" }
            StringNotEquals = { "s3:x-amz-server-side-encryption" = "aws:kms" }
            ArnNotEquals    = { "aws:PrincipalArn" = "arn:aws:iam::123456789012:user/tabp-bootstrap" }
          }
        },
        {
          Sid       = "DenyObjectEncryptionWithOtherKey"
          Effect    = "Deny"
          Principal = "*"
          Action    = "s3:PutObject"
          Resource  = "${aws_s3_bucket.this.arn}/*"
          Condition = {
            Null            = { "s3:x-amz-server-side-encryption-aws-kms-key-id" = "false" }
            StringNotEquals = { "s3:x-amz-server-side-encryption-aws-kms-key-id" = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab" }
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
    error_message = "bootstrap_executor の user が、書き込みの例外・key と操作の閉じ込め・暗号化の方式の header の拒否の例外に入っていません。"
  }
}
