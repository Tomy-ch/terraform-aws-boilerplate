# 公開 output の名前と、それが返す値の配線を固定する（ADR-0202 決定4）。
mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
  mock_data "aws_region" {
    defaults = { region = "ap-northeast-1" }
  }
}

variables {
  project                                 = "tabp"
  env                                     = "admin"
  state_noncurrent_version_retention_days = 90
}

run "outputs" {
  command = apply

  assert {
    condition     = can(regex("^tabp-admin-tfstate-[0-9]{12}-[a-z0-9-]+-an$", output.bucket_name))
    error_message = "bucket_name が state Bucket の完全な名前を返していません。"
  }

  assert {
    condition     = output.bucket_arn == module.s3.bucket_arn
    error_message = "bucket_arn が state Bucket の ARN を返していません。"
  }

  assert {
    condition     = output.plan_output_bucket_name == "tabp-admin-tfplan-123456789012-ap-northeast-1-an"
    error_message = "plan_output_bucket_name が plan の出力の Bucket の完全な名前を返していません。"
  }

  assert {
    condition     = output.plan_output_bucket_arn == module.plan_output.bucket_arn
    error_message = "plan_output_bucket_arn が plan の出力の Bucket の ARN を返していません。"
  }

  assert {
    condition     = output.plan_output_bucket_arn != output.bucket_arn
    error_message = "plan の出力の Bucket が state Bucket と分かれていません。"
  }
}

# alias の名前は deployment-identity の権限が受けるので、鍵より前に plan で決まらなければならない。
run "state_key_alias_name" {
  command = plan

  variables {
    project = "t4bp"
    env     = "dev"
  }

  assert {
    condition     = output.state_key_alias_name == "alias/t4bp-dev-tfstate"
    error_message = "state_key_alias_name が alias/<project>-<env>-tfstate を返していません。"
  }
}

run "plan_output_retention_days_default" {
  command = plan

  assert {
    condition     = var.plan_output_retention_days == 7
    error_message = "plan_output_retention_days の既定が7日になっていません。"
  }
}

run "plan_output_retention_days_zero" {
  command = plan

  variables {
    plan_output_retention_days = 0
  }

  expect_failures = [var.plan_output_retention_days]
}

run "plan_output_retention_days_fraction" {
  command = plan

  variables {
    plan_output_retention_days = 1.5
  }

  expect_failures = [var.plan_output_retention_days]
}

# 既定の 7 とは別の値を渡す。既定のままでは、配線が切れていても通る。
run "plan_output_retention_days_wiring" {
  command = plan

  variables {
    plan_output_retention_days = 3
  }

  assert {
    condition     = module.plan_output.retention_days == { current = 3, noncurrent = 3 }
    error_message = "plan_output_retention_days が plan の出力の lifecycle の日数に届いていません。"
  }
}

# ファイル先頭の 90 とは別の値を渡す。同じ値では、配線が切れていても通る。
run "state_noncurrent_version_retention_days_wiring" {
  command = plan

  variables {
    state_noncurrent_version_retention_days = 45
  }

  assert {
    condition     = module.s3.noncurrent_version_retention_days == 45
    error_message = "state_noncurrent_version_retention_days が state Bucket の lifecycle の日数に届いていません。"
  }
}

run "state_noncurrent_version_retention_days_minimum" {
  command = plan

  variables {
    state_noncurrent_version_retention_days = 1
  }

  assert {
    condition     = module.s3.noncurrent_version_retention_days == 1
    error_message = "state_noncurrent_version_retention_days = 1 が受け付けられていません。"
  }
}

run "state_noncurrent_version_retention_days_zero" {
  command = plan

  variables {
    state_noncurrent_version_retention_days = 0
  }

  expect_failures = [var.state_noncurrent_version_retention_days]
}

run "state_noncurrent_version_retention_days_fraction" {
  command = plan

  variables {
    state_noncurrent_version_retention_days = 1.5
  }

  expect_failures = [var.state_noncurrent_version_retention_days]
}
