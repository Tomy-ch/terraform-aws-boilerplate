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

run "name_invalid_characters" {
  command = plan

  variables {
    name = "Tabp_Mgmt"
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

run "name_reserved_prefix" {
  command = plan

  variables {
    name = "xn--abc"
  }

  expect_failures = [var.name]
}

# Trivy にも Policy Test にも無い性質なので、Policy Test（#74）が配線されるまで Unit で持つ。
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
}
