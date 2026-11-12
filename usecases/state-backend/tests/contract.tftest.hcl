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

# 2つの Bucket の kms_key_arn の validation を通すため、鍵の ARN だけ形のある値にする。
override_resource {
  target = module.kms.aws_kms_key.this
  values = { arn = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab" }
}

# unlock_identity の復号を限る lock file の ARN（modules/kms の objects の validation）を通すため、state Bucket の ARN も形のある値にする。
override_resource {
  target = module.s3.aws_s3_bucket.this
  values = { arn = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an" }
}

variables {
  project                                 = "tabp"
  env                                     = "admin"
  state_noncurrent_version_retention_days = 90
  ci_role_arns = {
    plan            = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan"
    apply_identity  = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
    unlock_identity = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"
  }
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

  assert {
    condition     = output.state_key_arn == module.kms.key_arn_for_iam
    error_message = "state_key_arn が、Key Policy の置き換えを待たない state の鍵の ARN を返していません。"
  }

  assert {
    condition     = module.s3.kms_key_arn == output.state_key_arn
    error_message = "state Bucket の既定の暗号化が state の鍵を名指していません。"
  }

  assert {
    condition     = module.plan_output.kms_key_arn == output.state_key_arn
    error_message = "plan の出力の Bucket の既定の暗号化が state の鍵を名指していません。"
  }
}

run "ci_role_arns_role_with_path" {
  command = plan

  variables {
    ci_role_arns = {
      plan            = "arn:aws:iam::123456789012:role/ci/tabp-admin-ci-plan"
      apply_identity  = "arn:aws:iam::123456789012:role/ci/tabp-admin-ci-apply-identity"
      unlock_identity = "arn:aws:iam::123456789012:role/ci/tabp-admin-ci-unlock-identity"
    }
  }

  assert {
    condition     = output.state_key_alias_name == "alias/tabp-admin-tfstate"
    error_message = "path を持つ role の ARN が受け付けられていません。"
  }
}

run "ci_role_arns_same_role" {
  command = plan

  variables {
    ci_role_arns = {
      plan            = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan"
      apply_identity  = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan"
      unlock_identity = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"
    }
  }

  expect_failures = [var.ci_role_arns]
}

run "ci_role_arns_same_role_plan_unlock" {
  command = plan

  variables {
    ci_role_arns = {
      plan            = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan"
      apply_identity  = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
      unlock_identity = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan"
    }
  }

  expect_failures = [var.ci_role_arns]
}

run "ci_role_arns_same_role_apply_identity_unlock" {
  command = plan

  variables {
    ci_role_arns = {
      plan            = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan"
      apply_identity  = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
      unlock_identity = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
    }
  }

  expect_failures = [var.ci_role_arns]
}

run "ci_role_arns_unlock_identity_user" {
  command = plan

  variables {
    ci_role_arns = {
      plan            = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan"
      apply_identity  = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
      unlock_identity = "arn:aws:iam::123456789012:user/tabp-admin-ci-unlock-identity"
    }
  }

  expect_failures = [var.ci_role_arns]
}

run "ci_role_arns_plan_user" {
  command = plan

  variables {
    ci_role_arns = {
      plan            = "arn:aws:iam::123456789012:user/tabp-admin-ci-plan"
      apply_identity  = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
      unlock_identity = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"
    }
  }

  expect_failures = [var.ci_role_arns]
}

run "ci_role_arns_apply_identity_root" {
  command = plan

  variables {
    ci_role_arns = {
      plan            = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan"
      apply_identity  = "arn:aws:iam::123456789012:root"
      unlock_identity = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"
    }
  }

  expect_failures = [var.ci_role_arns]
}

run "ci_role_arns_wildcard" {
  command = plan

  variables {
    ci_role_arns = {
      plan            = "arn:aws:iam::123456789012:role/*"
      apply_identity  = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
      unlock_identity = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"
    }
  }

  expect_failures = [var.ci_role_arns]
}

run "ci_role_arns_apply_identity_no_name" {
  command = plan

  variables {
    ci_role_arns = {
      plan            = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan"
      apply_identity  = "arn:aws:iam::123456789012:role/"
      unlock_identity = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"
    }
  }

  expect_failures = [var.ci_role_arns]
}

# alias の名前は bootstrap の手順が鍵を名指す名前なので、鍵より前に plan で決まらなければならない。
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

  assert {
    condition     = output.state_key_alias_name == module.kms.alias_name
    error_message = "state_key_alias_name が、鍵に付く alias の名前を返していません。"
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
