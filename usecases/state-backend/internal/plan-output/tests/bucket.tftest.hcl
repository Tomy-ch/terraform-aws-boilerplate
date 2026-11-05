mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
  mock_data "aws_region" {
    defaults = { region = "ap-northeast-1" }
  }
}

variables {
  name           = "tabp-admin-tfplan"
  retention_days = 7
}

run "account_regional_name" {
  command = plan

  assert {
    condition     = output.bucket_name == "tabp-admin-tfplan-123456789012-ap-northeast-1-an"
    error_message = "完全な名前が name と account_id と region から組まれていません。"
  }

  assert {
    condition     = output.bucket_name == aws_s3_bucket.this.bucket
    error_message = "bucket_name が Bucket の名前と一致していません。"
  }
}

# ARN は plan では決まらないので、mock の apply で確かめる。
run "bucket_arn_output" {
  command = apply

  assert {
    condition     = output.bucket_arn == aws_s3_bucket.this.arn
    error_message = "bucket_arn が Bucket の ARN を返していません。"
  }
}

run "name_too_long" {
  command = plan

  variables {
    name = "abcdefghijklmnopqrstuvwxyz0123456"
  }

  expect_failures = [aws_s3_bucket.this]
}

run "name_at_limit" {
  command = plan

  variables {
    name = "abcdefghijklmnopqrstuvwxyz012345"
  }

  assert {
    condition     = length(output.bucket_name) == 63
    error_message = "完全な名前がちょうど63文字のとき、plan が通っていません。"
  }
}

run "name_uppercase" {
  command = plan

  variables {
    name = "tabp-Admin"
  }

  expect_failures = [var.name]
}

run "name_trailing_hyphen" {
  command = plan

  variables {
    name = "abc-"
  }

  expect_failures = [var.name]
}

run "name_reserved_prefix" {
  command = plan

  variables {
    name = "sthree-abc"
  }

  expect_failures = [var.name]
}

run "retention_days_zero" {
  command = plan

  variables {
    retention_days = 0
  }

  expect_failures = [var.retention_days]
}

run "retention_days_fraction" {
  command = plan

  variables {
    retention_days = 1.5
  }

  expect_failures = [var.retention_days]
}

run "retention_days_minimum" {
  command = plan

  variables {
    retention_days = 1
  }

  assert {
    condition     = one(aws_s3_bucket_lifecycle_configuration.this.rule[0].expiration).days == 1
    error_message = "retention_days = 1 が現行版の失効日数になっていません。"
  }
}

run "bucket_attributes" {
  command = plan

  assert {
    condition     = aws_s3_bucket.this.bucket_namespace == "account-regional"
    error_message = "bucket_namespace が account-regional ではありません。"
  }

  assert {
    condition     = aws_s3_bucket.this.force_destroy == false
    error_message = "force_destroy が false ではありません。"
  }

  assert {
    condition = (
      aws_s3_bucket_public_access_block.this.block_public_acls &&
      aws_s3_bucket_public_access_block.this.block_public_policy &&
      aws_s3_bucket_public_access_block.this.ignore_public_acls &&
      aws_s3_bucket_public_access_block.this.restrict_public_buckets
    )
    error_message = "Public Access Block の4項目がすべて true ではありません。"
  }

  assert {
    condition     = one(aws_s3_bucket_ownership_controls.this.rule).object_ownership == "BucketOwnerEnforced"
    error_message = "object_ownership が BucketOwnerEnforced ではありません。"
  }

  assert {
    condition     = one(aws_s3_bucket_versioning.this.versioning_configuration).status == "Enabled"
    error_message = "版管理が Enabled ではありません。"
  }

  assert {
    condition     = one(one(aws_s3_bucket_server_side_encryption_configuration.this.rule).apply_server_side_encryption_by_default).sse_algorithm == "AES256"
    error_message = "既定の暗号化が AES256 ではありません。"
  }

  assert {
    condition     = toset(one(aws_s3_bucket_server_side_encryption_configuration.this.rule).blocked_encryption_types) == toset(["SSE-C"])
    error_message = "SSE-C を拒否していません。"
  }
}

run "lifecycle_rules" {
  command = plan

  assert {
    condition     = [for r in aws_s3_bucket_lifecycle_configuration.this.rule : r.id] == ["expire-plan-output", "remove-expired-delete-markers"]
    error_message = "lifecycle の規則が、出力の失効と delete marker の除去の2つになっていません。"
  }

  assert {
    condition     = alltrue([for r in aws_s3_bucket_lifecycle_configuration.this.rule : r.status == "Enabled" && length(r.filter) == 1])
    error_message = "lifecycle の規則が、filter を1つ持つ有効な規則になっていません。"
  }

  assert {
    condition     = one(aws_s3_bucket_lifecycle_configuration.this.rule[0].expiration).days == 7
    error_message = "現行版が retention_days で失効しません。"
  }

  assert {
    condition     = one(aws_s3_bucket_lifecycle_configuration.this.rule[0].noncurrent_version_expiration).noncurrent_days == 7
    error_message = "上書きされた版と失効した版が retention_days で消えません。"
  }

  assert {
    condition     = one(aws_s3_bucket_lifecycle_configuration.this.rule[0].abort_incomplete_multipart_upload).days_after_initiation == 1
    error_message = "未完了の multipart upload が1日で中止されません。"
  }

  assert {
    condition     = one(aws_s3_bucket_lifecycle_configuration.this.rule[1].expiration).expired_object_delete_marker == true
    error_message = "失効した版の delete marker を消す規則がありません。"
  }
}
