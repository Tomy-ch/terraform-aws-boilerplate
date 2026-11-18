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
  kms_key_arn    = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  name           = "tabp-admin-tfplan"
  retention_days = 7
  writers = [
    { role_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan", key_prefix = "pr/" },
    { role_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan", key_prefix = "branch/" },
    { role_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity", key_prefix = "apply/" },
  ]
}

# Bucket の ARN は plan では決まらないので、mock の apply で確かめる。
run "bucket_policy" {
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
          Action    = "s3:PutObject"
          Resource  = "${aws_s3_bucket.this.arn}/*"
          Condition = { ArnNotEquals = { "aws:PrincipalArn" = [
            "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity",
            "arn:aws:iam::123456789012:role/tabp-admin-ci-plan",
          ] } }
        },
        {
          Sid       = "DenyObjectDeletion"
          Effect    = "Deny"
          Principal = "*"
          Action    = ["s3:DeleteObject", "s3:DeleteObjectVersion"]
          Resource  = "${aws_s3_bucket.this.arn}/*"
        },
        {
          Sid         = "DenyObjectWriteOutsidePrefixes0"
          Effect      = "Deny"
          Principal   = "*"
          Action      = "s3:PutObject"
          NotResource = ["${aws_s3_bucket.this.arn}/apply/*"]
          Condition   = { ArnEquals = { "aws:PrincipalArn" = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity" } }
        },
        {
          Sid         = "DenyObjectWriteOutsidePrefixes1"
          Effect      = "Deny"
          Principal   = "*"
          Action      = "s3:PutObject"
          NotResource = ["${aws_s3_bucket.this.arn}/branch/*", "${aws_s3_bucket.this.arn}/pr/*"]
          Condition   = { ArnEquals = { "aws:PrincipalArn" = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan" } }
        },
      ]
    }
    error_message = "Bucket Policy が、TLS の要求・書き手の外の PutObject の拒否・削除の無条件の拒否・書き手ごとの接頭辞の外の PutObject の拒否の文だけになっていません。"
  }

  assert {
    condition     = aws_s3_bucket_policy.this.bucket == aws_s3_bucket.this.id
    error_message = "Bucket Policy が、この module の Bucket に付いていません。"
  }

  assert {
    condition     = output.writers == var.writers
    error_message = "writers の出力が、Bucket Policy に渡した書き手を返していません。"
  }
}

# 接頭辞の空は、その role が Bucket のすべての key に書けることを表す。
run "bucket_policy_whole_bucket_writer" {
  command = apply

  variables {
    name    = "tabp-admin-tfapply"
    writers = [{ role_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply", key_prefix = "" }]
  }

  assert {
    condition = [for s in jsondecode(aws_s3_bucket_policy.this.policy).Statement : s if s.Sid != "DenyInsecureTransport"] == [
      {
        Sid       = "DenyObjectWriteByOthers"
        Effect    = "Deny"
        Principal = "*"
        Action    = "s3:PutObject"
        Resource  = "${aws_s3_bucket.this.arn}/*"
        Condition = { ArnNotEquals = { "aws:PrincipalArn" = ["arn:aws:iam::123456789012:role/tabp-admin-ci-apply"] } }
      },
      {
        Sid       = "DenyObjectDeletion"
        Effect    = "Deny"
        Principal = "*"
        Action    = ["s3:DeleteObject", "s3:DeleteObjectVersion"]
        Resource  = "${aws_s3_bucket.this.arn}/*"
      },
      {
        Sid         = "DenyObjectWriteOutsidePrefixes0"
        Effect      = "Deny"
        Principal   = "*"
        Action      = "s3:PutObject"
        NotResource = ["${aws_s3_bucket.this.arn}/*"]
        Condition   = { ArnEquals = { "aws:PrincipalArn" = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply" } }
      },
    ]
    error_message = "接頭辞が空の書き手の Bucket Policy が、Bucket のすべての key への PutObject をその role に残していません。"
  }
}

run "writers_empty" {
  command = plan

  variables {
    writers = []
  }

  expect_failures = [var.writers]
}

run "writers_user" {
  command = plan

  variables {
    writers = [{ role_arn = "arn:aws:iam::123456789012:user/tabp-admin-ci-plan", key_prefix = "pr/" }]
  }

  expect_failures = [var.writers]
}

run "writers_role_wildcard" {
  command = plan

  variables {
    writers = [{ role_arn = "arn:aws:iam::123456789012:role/*", key_prefix = "pr/" }]
  }

  expect_failures = [var.writers]
}

run "writers_prefix_without_trailing_slash" {
  command = plan

  variables {
    writers = [{ role_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan", key_prefix = "pr" }]
  }

  expect_failures = [var.writers]
}

run "writers_prefix_wildcard" {
  command = plan

  variables {
    writers = [{ role_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan", key_prefix = "p*/" }]
  }

  expect_failures = [var.writers]
}

run "writers_prefix_leading_slash" {
  command = plan

  variables {
    writers = [{ role_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan", key_prefix = "/pr/" }]
  }

  expect_failures = [var.writers]
}

run "writers_nested_prefix" {
  command = plan

  variables {
    writers = [{ role_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan", key_prefix = "apply/identity/" }]
  }
}
