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

override_resource {
  target = aws_s3_bucket.this
  values = {
    id  = "tabp-log-archive-audit-123456789012-ap-northeast-1-an"
    arn = "arn:aws:s3:::tabp-log-archive-audit-123456789012-ap-northeast-1-an"
  }
}

variables {
  name          = "tabp-log-archive-audit"
  kms_key_arn   = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  retention     = { mode = "COMPLIANCE", days = 365 }
  force_destroy = false
  writers = {
    cloudtrail_organization_trail = {
      trail_name       = "tabp-org"
      owner_account_id = "111111111111"
      organization_id  = "o-abcdefghij"
      region           = "ap-northeast-1"
    }
  }
  access_log_source_name     = "tabp-log-archive-audit-access-log"
  access_log_destination_arn = "arn:aws:logs:ap-northeast-1:123456789012:delivery-destination:tabp-log-archive-audit-access-log"
  notify_topic_arn           = "arn:aws:sns:ap-northeast-1:123456789012:tabp-log-archive-ops-notification"
}

run "invariants" {
  command = plan

  assert {
    condition     = aws_s3_bucket.this.bucket == "tabp-log-archive-audit-123456789012-ap-northeast-1-an" && aws_s3_bucket.this.bucket_namespace == "account-regional"
    error_message = "Bucket の名前か namespace が、<name>-<account>-<region>-an と account-regional になっていません。"
  }

  assert {
    condition     = aws_s3_bucket.this.object_lock_enabled == true
    error_message = "Bucket の Object Lock が有効になっていません。"
  }

  assert {
    condition     = aws_s3_bucket.this.force_destroy == false
    error_message = "force_destroy が入力のとおりになっていません。"
  }

  assert {
    condition = (
      aws_s3_bucket_public_access_block.this.block_public_acls &&
      aws_s3_bucket_public_access_block.this.block_public_policy &&
      aws_s3_bucket_public_access_block.this.ignore_public_acls &&
      aws_s3_bucket_public_access_block.this.restrict_public_buckets
    )
    error_message = "Public Access Block の4項目のどれかが true になっていません。"
  }

  assert {
    condition     = one(aws_s3_bucket_ownership_controls.this.rule).object_ownership == "BucketOwnerEnforced"
    error_message = "所有権が BucketOwnerEnforced になっていません。"
  }

  assert {
    condition     = one(aws_s3_bucket_versioning.this.versioning_configuration).status == "Enabled"
    error_message = "版管理が有効になっていません。"
  }

  assert {
    condition     = one(one(aws_s3_bucket_object_lock_configuration.this.rule).default_retention).mode == "COMPLIANCE" && one(one(aws_s3_bucket_object_lock_configuration.this.rule).default_retention).days == 365
    error_message = "Object Lock の既定の保持が、入力の mode と days になっていません。"
  }

  assert {
    condition = (
      one(one(aws_s3_bucket_server_side_encryption_configuration.this.rule).apply_server_side_encryption_by_default).sse_algorithm == "aws:kms" &&
      one(one(aws_s3_bucket_server_side_encryption_configuration.this.rule).apply_server_side_encryption_by_default).kms_master_key_id == var.kms_key_arn &&
      one(aws_s3_bucket_server_side_encryption_configuration.this.rule).bucket_key_enabled == false &&
      toset(one(aws_s3_bucket_server_side_encryption_configuration.this.rule).blocked_encryption_types) == toset(["SSE-C"])
    )
    error_message = "既定の暗号化が、入力の鍵による SSE-KMS（Bucket Key なし、SSE-C を拒む）になっていません。"
  }
}

run "governance_and_force_destroy" {
  command = plan

  variables {
    retention     = { mode = "GOVERNANCE", days = 1 }
    force_destroy = true
  }

  assert {
    condition     = one(one(aws_s3_bucket_object_lock_configuration.this.rule).default_retention).mode == "GOVERNANCE" && one(one(aws_s3_bucket_object_lock_configuration.this.rule).default_retention).days == 1
    error_message = "Object Lock の既定の保持が、入力の GOVERNANCE と1日になっていません。"
  }

  assert {
    condition     = aws_s3_bucket.this.force_destroy == true
    error_message = "force_destroy が入力のとおりになっていません。"
  }
}

run "rejects_long_name" {
  command = plan

  variables {
    name = "tabp-log-archive-audit-abcdefghijklmn"
  }

  expect_failures = [aws_s3_bucket.this]
}

run "rejects_key_in_other_account" {
  command = plan

  variables {
    kms_key_arn = "arn:aws:kms:ap-northeast-1:210987654321:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  }

  expect_failures = [aws_s3_bucket_server_side_encryption_configuration.this]
}

run "accepts_63_character_name" {
  command = plan

  variables {
    name = "tabp-log-archive-audit-abcdefghi"
  }

  assert {
    condition     = length(aws_s3_bucket.this.bucket) == 63
    error_message = "境界の Bucket 名が 63 文字になっていません。名前の組み立てが変わったなら、name の長さを直してください。"
  }
}

run "rejects_64_character_name" {
  command = plan

  variables {
    name = "tabp-log-archive-audit-abcdefghij"
  }

  expect_failures = [aws_s3_bucket.this]
}

run "rejects_key_in_other_region" {
  command = plan

  variables {
    kms_key_arn = "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  }

  expect_failures = [aws_s3_bucket_server_side_encryption_configuration.this]
}

run "rejects_key_in_other_partition" {
  command = plan

  variables {
    kms_key_arn = "arn:aws-cn:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  }

  expect_failures = [aws_s3_bucket_server_side_encryption_configuration.this]
}
