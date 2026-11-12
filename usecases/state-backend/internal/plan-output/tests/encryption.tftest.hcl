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
  name           = "tabp-admin-tfplan"
  retention_days = 7
  kms_key_arn    = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
}

run "accepts_key_in_same_account_and_region" {
  command = plan
}

run "accepts_key_arn_in_other_partition" {
  command = plan

  variables {
    kms_key_arn = "arn:aws-us-gov:kms:us-gov-west-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  }

  override_data {
    target = data.aws_partition.current
    values = { partition = "aws-us-gov" }
  }
  override_data {
    target = data.aws_region.current
    values = { region = "us-gov-west-1" }
  }

  assert {
    condition     = one(one(aws_s3_bucket_server_side_encryption_configuration.this.rule).apply_server_side_encryption_by_default).kms_master_key_id == var.kms_key_arn
    error_message = "別の partition の鍵が既定の暗号化に届いていません。"
  }
}

run "rejects_alias_arn" {
  command = plan

  variables {
    kms_key_arn = "arn:aws:kms:ap-northeast-1:123456789012:alias/tabp-admin-tfstate"
  }

  expect_failures = [var.kms_key_arn]
}

run "rejects_key_wildcard" {
  command = plan

  variables {
    kms_key_arn = "arn:aws:kms:ap-northeast-1:123456789012:key/*"
  }

  expect_failures = [var.kms_key_arn]
}

run "rejects_multi_region_key" {
  command = plan

  variables {
    kms_key_arn = "arn:aws:kms:ap-northeast-1:123456789012:key/mrk-1234abcd12ab34cd56ef1234567890ab"
  }

  expect_failures = [var.kms_key_arn]
}

run "rejects_bare_key_id" {
  command = plan

  variables {
    kms_key_arn = "00000000-0000-0000-0000-000000000000"
  }

  expect_failures = [var.kms_key_arn]
}

run "rejects_bucket_arn_as_key" {
  command = plan

  variables {
    kms_key_arn = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an"
  }

  expect_failures = [var.kms_key_arn]
}

run "rejects_empty_key_arn" {
  command = plan

  variables {
    kms_key_arn = ""
  }

  expect_failures = [var.kms_key_arn]
}

run "rejects_unknown_partition_name" {
  command = plan

  variables {
    kms_key_arn = "arn:gcp:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  }

  expect_failures = [var.kms_key_arn]
}

run "rejects_11_digit_account" {
  command = plan

  variables {
    kms_key_arn = "arn:aws:kms:ap-northeast-1:12345678901:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  }

  expect_failures = [var.kms_key_arn]
}

run "rejects_uppercase_key_id" {
  command = plan

  variables {
    kms_key_arn = "arn:aws:kms:ap-northeast-1:123456789012:key/1234ABCD-12ab-34cd-56ef-1234567890ab"
  }

  expect_failures = [var.kms_key_arn]
}

run "rejects_key_in_other_account" {
  command = plan

  variables {
    kms_key_arn = "arn:aws:kms:ap-northeast-1:210987654321:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  }

  expect_failures = [aws_s3_bucket_server_side_encryption_configuration.this]
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
    kms_key_arn = "arn:aws-us-gov:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  }

  expect_failures = [aws_s3_bucket_server_side_encryption_configuration.this]
}
