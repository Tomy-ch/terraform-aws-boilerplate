mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
  mock_data "aws_region" {
    defaults = { region = "ap-northeast-1" }
  }
}

run "account_regional_name" {
  command = plan

  variables {
    name = "tabp-mgmt-tfstate"
  }

  assert {
    condition     = output.bucket_name == "tabp-mgmt-tfstate-123456789012-ap-northeast-1-an"
    error_message = "完全な名前が name と account_id と region から組まれていません。"
  }
}

# usecase の import block が id に使うので、plan の時点で Bucket の名前と一致していなければならない。
run "bucket_name_output_matches_bucket" {
  command = plan

  variables {
    name = "tabp-admin-tfstate"
  }

  assert {
    condition     = output.bucket_name == aws_s3_bucket.this.bucket
    error_message = "bucket_name が Bucket の名前と一致していません。"
  }
}

# ARN は plan では決まらないので、mock の apply で確かめる。
run "bucket_arn_output" {
  command = apply

  variables {
    name = "tabp-mgmt-tfstate"
  }

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

run "name_uppercase" {
  command = plan

  variables {
    name = "tabp-Mgmt"
  }

  expect_failures = [var.name]
}

run "name_underscore" {
  command = plan

  variables {
    name = "tabp_mgmt"
  }

  expect_failures = [var.name]
}

run "name_dot" {
  command = plan

  variables {
    name = "tabp.mgmt"
  }

  expect_failures = [var.name]
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

run "name_single_character" {
  command = plan

  variables {
    name = "a"
  }

  assert {
    condition     = output.bucket_name == "a-123456789012-ap-northeast-1-an"
    error_message = "1文字の name から完全な名前が組まれていません。"
  }
}

run "name_leading_hyphen" {
  command = plan

  variables {
    name = "-abc"
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

run "name_empty" {
  command = plan

  variables {
    name = ""
  }

  expect_failures = [var.name]
}

run "name_reserved_prefix_xn" {
  command = plan

  variables {
    name = "xn--abc"
  }

  expect_failures = [var.name]
}

run "name_reserved_prefix_sthree" {
  command = plan

  variables {
    name = "sthree-abc"
  }

  expect_failures = [var.name]
}

run "name_reserved_prefix_amzn_s3_demo" {
  command = plan

  variables {
    name = "amzn-s3-demo-abc"
  }

  expect_failures = [var.name]
}

run "name_reserved_string_not_prefix" {
  command = plan

  variables {
    name = "a-xn--sthree-amzn-s3-demo-b"
  }

  assert {
    condition     = output.bucket_name == "a-xn--sthree-amzn-s3-demo-b-123456789012-ap-northeast-1-an"
    error_message = "予約された文字列を途中に含むだけの name が拒まれています。"
  }
}

run "name_near_reserved_prefix" {
  command = plan

  variables {
    name = "xn-abc"
  }

  assert {
    condition     = output.bucket_name == "xn-abc-123456789012-ap-northeast-1-an"
    error_message = "予約された接頭辞に近いだけの name が拒まれています。"
  }
}

# Security の道具が見ない、または一部の道具しか見ない性質なので Unit で持つ（README「保証しないもの」）。
run "bucket_attributes" {
  command = plan

  variables {
    name = "tabp-mgmt-tfstate"
  }

  assert {
    condition     = aws_s3_bucket.this.bucket_namespace == "account-regional"
    error_message = "bucket_namespace が account-regional ではありません。"
  }

  assert {
    condition     = aws_s3_bucket.this.force_destroy == false
    error_message = "force_destroy が false ではありません。"
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
