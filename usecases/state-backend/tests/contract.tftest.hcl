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

variables {
  project                                 = "tabp"
  env                                     = "admin"
  state_noncurrent_version_retention_days = 90
  ci_role_arns = {
    plan            = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan"
    apply           = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply"
    apply_identity  = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
    unlock_identity = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"
    unlock          = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock"
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
    condition     = output.apply_output_bucket_arn == module.apply_output.bucket_arn
    error_message = "apply_output_bucket_arn が apply の出力の Bucket の ARN を返していません。"
  }

  assert {
    condition     = module.apply_output.bucket_name == "tabp-admin-tfapply-123456789012-ap-northeast-1-an"
    error_message = "apply の出力の Bucket の名前が <project>-<env>-tfapply-<account_id>-<region>-an になっていません。"
  }

  assert {
    condition     = length(toset([output.bucket_arn, output.plan_output_bucket_arn, output.apply_output_bucket_arn])) == 3
    error_message = "apply の出力の Bucket が state Bucket と plan の出力の Bucket と分かれていません。"
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

  assert {
    condition     = module.apply_output.kms_key_arn == output.state_key_arn
    error_message = "apply の出力の Bucket の既定の暗号化が state の鍵を名指していません。"
  }
}

run "ci_role_arns_role_with_path" {
  command = plan

  variables {
    ci_role_arns = {
      plan            = "arn:aws:iam::123456789012:role/ci/tabp-admin-ci-plan"
      apply           = "arn:aws:iam::123456789012:role/ci/tabp-admin-ci-apply"
      apply_identity  = "arn:aws:iam::123456789012:role/ci/tabp-admin-ci-apply-identity"
      unlock_identity = "arn:aws:iam::123456789012:role/ci/tabp-admin-ci-unlock-identity"
      unlock          = "arn:aws:iam::123456789012:role/ci/tabp-admin-ci-unlock"
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
      apply           = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply"
      apply_identity  = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan"
      unlock_identity = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"
      unlock          = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock"
    }
  }

  expect_failures = [var.ci_role_arns]
}

run "ci_role_arns_same_role_plan_unlock" {
  command = plan

  variables {
    ci_role_arns = {
      plan            = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan"
      apply           = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply"
      apply_identity  = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
      unlock_identity = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan"
      unlock          = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock"
    }
  }

  expect_failures = [var.ci_role_arns]
}

run "ci_role_arns_same_role_apply_identity_unlock" {
  command = plan

  variables {
    ci_role_arns = {
      plan            = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan"
      apply           = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply"
      apply_identity  = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
      unlock_identity = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
      unlock          = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock"
    }
  }

  expect_failures = [var.ci_role_arns]
}

run "ci_role_arns_unlock_identity_user" {
  command = plan

  variables {
    ci_role_arns = {
      plan            = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan"
      apply           = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply"
      apply_identity  = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
      unlock_identity = "arn:aws:iam::123456789012:user/tabp-admin-ci-unlock-identity"
      unlock          = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock"
    }
  }

  expect_failures = [var.ci_role_arns]
}

run "ci_role_arns_plan_user" {
  command = plan

  variables {
    ci_role_arns = {
      plan            = "arn:aws:iam::123456789012:user/tabp-admin-ci-plan"
      apply           = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply"
      apply_identity  = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
      unlock_identity = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"
      unlock          = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock"
    }
  }

  expect_failures = [var.ci_role_arns]
}

run "ci_role_arns_apply_identity_root" {
  command = plan

  variables {
    ci_role_arns = {
      plan            = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan"
      apply           = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply"
      apply_identity  = "arn:aws:iam::123456789012:root"
      unlock_identity = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"
      unlock          = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock"
    }
  }

  expect_failures = [var.ci_role_arns]
}

run "ci_role_arns_wildcard" {
  command = plan

  variables {
    ci_role_arns = {
      plan            = "arn:aws:iam::123456789012:role/*"
      apply           = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply"
      apply_identity  = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
      unlock_identity = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"
      unlock          = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock"
    }
  }

  expect_failures = [var.ci_role_arns]
}

run "ci_role_arns_apply_identity_no_name" {
  command = plan

  variables {
    ci_role_arns = {
      plan            = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan"
      apply           = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply"
      apply_identity  = "arn:aws:iam::123456789012:role/"
      unlock_identity = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"
      unlock          = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock"
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

  assert {
    condition     = module.apply_output.retention_days == { current = 3, noncurrent = 3 }
    error_message = "plan_output_retention_days が apply の出力の lifecycle の日数に届いていません。"
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

run "ci_role_arns_same_role_apply_apply_identity" {
  command = plan

  variables {
    ci_role_arns = {
      plan            = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan"
      apply           = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
      apply_identity  = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
      unlock_identity = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"
      unlock          = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock"
    }
  }

  expect_failures = [var.ci_role_arns]
}

run "ci_role_arns_same_role_apply_unlock" {
  command = plan

  variables {
    ci_role_arns = {
      plan            = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan"
      apply           = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"
      apply_identity  = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
      unlock_identity = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"
      unlock          = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock"
    }
  }

  expect_failures = [var.ci_role_arns]
}

run "ci_role_arns_apply_user" {
  command = plan

  variables {
    ci_role_arns = {
      plan            = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan"
      apply           = "arn:aws:iam::123456789012:user/tabp-admin-ci-apply"
      apply_identity  = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
      unlock_identity = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"
      unlock          = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock"
    }
  }

  expect_failures = [var.ci_role_arns]
}

# 書き込みの例外の割当は ADR-0604 決定9。
run "object_writers" {
  command = plan

  assert {
    condition = module.s3.object_writers == {
      identity = {
        principal_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
        keys          = toset(["identity/terraform.tfstate", "identity/terraform.tfstate.tflock"])
        access        = "write"
      }
      identityUnlock = {
        principal_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"
        keys          = toset(["identity/terraform.tfstate.tflock"])
        access        = "delete"
      }
      workload = {
        principal_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply"
        keys          = toset(["workload/terraform.tfstate", "workload/terraform.tfstate.tflock"])
        access        = "write"
      }
      workloadUnlock = {
        principal_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock"
        keys          = toset(["workload/terraform.tfstate.tflock"])
        access        = "delete"
      }
    }
    error_message = "ci_role_arns の apply・apply_identity・unlock_identity・unlock が、modules/s3 の object_writers へ自分の key と操作で渡っていません。"
  }
}

run "bootstrap_executor" {
  command = plan

  variables {
    bootstrap_executor_user_name = "tabp-bootstrap"
  }

  assert {
    condition = module.s3.object_writers["bootstrapExecutor"] == {
      principal_arn = "arn:aws:iam::123456789012:user/tabp-bootstrap"
      keys          = toset(["identity/terraform.tfstate", "identity/terraform.tfstate.tflock"])
      access        = "write"
    }
    error_message = "bootstrap_executor_user_name の user が、identity root の state と lock file の書き込みの例外に入っていません。"
  }
}

run "bootstrap_executor_absent" {
  command = plan

  assert {
    condition     = !contains(keys(module.s3.object_writers), "bootstrapExecutor")
    error_message = "bootstrap_executor_user_name が null なのに、書き込みの例外に user が入っています。"
  }
}

run "bootstrap_executor_with_path" {
  command = plan

  variables {
    bootstrap_executor_user_name = "bootstrap/tabp-bootstrap"
  }

  expect_failures = [var.bootstrap_executor_user_name]
}

run "bootstrap_executor_arn" {
  command = plan

  variables {
    bootstrap_executor_user_name = "arn:aws:iam::123456789012:user/tabp-bootstrap"
  }

  expect_failures = [var.bootstrap_executor_user_name]
}

run "ci_role_arns_same_role_plan_apply" {
  command = plan

  variables {
    ci_role_arns = {
      plan            = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan"
      apply           = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan"
      apply_identity  = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
      unlock_identity = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"
      unlock          = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock"
    }
  }

  expect_failures = [var.ci_role_arns]
}

# 書き手の割当は state-backend/ADR-0201 決定8。
run "output_bucket_writers" {
  command = plan

  assert {
    condition = module.plan_output.writers == toset([
      { role_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan", key_prefix = "pr/" },
      { role_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan", key_prefix = "branch/" },
      { role_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity", key_prefix = "apply/" },
    ])
    error_message = "plan の出力の Bucket の書き手が、plan の role の pr/・branch/ と apply_identity の role の apply/ になっていません。"
  }

  assert {
    condition     = module.apply_output.writers == toset([{ role_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply", key_prefix = "" }])
    error_message = "apply の出力の Bucket の書き手が、apply の role だけになっていません。"
  }
}

run "ci_role_arns_same_role_unlock_identity_unlock" {
  command = plan

  variables {
    ci_role_arns = {
      plan            = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan"
      apply           = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply"
      apply_identity  = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
      unlock_identity = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"
      unlock          = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"
    }
  }

  expect_failures = [var.ci_role_arns]
}

run "ci_role_arns_same_role_apply_unlock_workload" {
  command = plan

  variables {
    ci_role_arns = {
      plan            = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan"
      apply           = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply"
      apply_identity  = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
      unlock_identity = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"
      unlock          = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply"
    }
  }

  expect_failures = [var.ci_role_arns]
}

run "ci_role_arns_unlock_user" {
  command = plan

  variables {
    ci_role_arns = {
      plan            = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan"
      apply           = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply"
      apply_identity  = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
      unlock_identity = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"
      unlock          = "arn:aws:iam::123456789012:user/tabp-admin-ci-unlock"
    }
  }

  expect_failures = [var.ci_role_arns]
}

# Key Policy の割当は state-backend/ADR-0201 決定9。state Bucket の ARN は名前から組むので、Bucket を作る plan でも文書が決まる。
# 出力の2つの Bucket の ARN は override しない。その ARN を待つと、apply の出力の Bucket を CI が作る plan で文書が決まらない。
# object の key は backend.tf の key と lock file、scripts/tf-local の planKey・applyKey が組む key の接頭辞と同じ文字列である。
run "key_policy_apply_and_unlock" {
  command = plan

  assert {
    condition = [for s in jsondecode(module.kms.key_policy).Statement : s.Sid] == [
      "AccountKeyAdministration",
      "RolesViaS3ReadWrite0", "RolesViaS3ReadWrite1", "RolesViaS3ReadWrite2",
      "RolesViaS3Read0", "RolesViaS3Read1", "RolesViaS3Read2",
    ]
    error_message = "Key Policy が、委任と、read-write の3文（apply・apply_identity・plan）と、read の3文（plan・unlock・unlock_identity）の7文になっていません。"
  }

  assert {
    condition = [for s in jsondecode(module.kms.key_policy).Statement : s if startswith(s.Sid, "RolesViaS3ReadWrite")] == [
      {
        Sid       = "RolesViaS3ReadWrite0"
        Effect    = "Allow"
        Principal = { AWS = ["arn:aws:iam::123456789012:role/tabp-admin-ci-apply"] }
        Action    = ["kms:Decrypt", "kms:GenerateDataKey"]
        Resource  = "*"
        Condition = {
          StringEquals = { "kms:ViaService" = "s3.ap-northeast-1.amazonaws.com" }
          StringLike = { "kms:EncryptionContext:aws:s3:arn" = [
            "arn:aws:s3:::tabp-admin-tfapply-123456789012-ap-northeast-1-an/*",
            "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/workload/terraform.tfstate",
            "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/workload/terraform.tfstate.tflock",
          ] }
        }
      },
      {
        Sid       = "RolesViaS3ReadWrite1"
        Effect    = "Allow"
        Principal = { AWS = ["arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"] }
        Action    = ["kms:Decrypt", "kms:GenerateDataKey"]
        Resource  = "*"
        Condition = {
          StringEquals = { "kms:ViaService" = "s3.ap-northeast-1.amazonaws.com" }
          StringLike = { "kms:EncryptionContext:aws:s3:arn" = [
            "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an/apply/*",
            "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/identity/terraform.tfstate",
            "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/identity/terraform.tfstate.tflock",
          ] }
        }
      },
      {
        Sid       = "RolesViaS3ReadWrite2"
        Effect    = "Allow"
        Principal = { AWS = ["arn:aws:iam::123456789012:role/tabp-admin-ci-plan"] }
        Action    = ["kms:Decrypt", "kms:GenerateDataKey"]
        Resource  = "*"
        Condition = {
          StringEquals = { "kms:ViaService" = "s3.ap-northeast-1.amazonaws.com" }
          StringLike = { "kms:EncryptionContext:aws:s3:arn" = [
            "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an/branch/*",
            "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an/pr/*",
          ] }
        }
      },
    ]
    error_message = "Key Policy の read-write の文が、apply に workload root の state と lock file と apply の出力の Bucket、apply_identity に identity root の state と lock file と plan の出力の apply/、plan に plan の出力の pr/・branch/ の復号と鍵の生成だけを許していません。"
  }

  assert {
    condition = [for s in jsondecode(module.kms.key_policy).Statement : s if s.Sid == "RolesViaS3Read0"] == [{
      Sid       = "RolesViaS3Read0"
      Effect    = "Allow"
      Principal = { AWS = ["arn:aws:iam::123456789012:role/tabp-admin-ci-plan"] }
      Action    = "kms:Decrypt"
      Resource  = "*"
      Condition = { StringEquals = {
        "kms:ViaService" = "s3.ap-northeast-1.amazonaws.com"
        "kms:EncryptionContext:aws:s3:arn" = [
          "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/identity/terraform.tfstate",
          "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/workload/terraform.tfstate",
        ]
      } }
    }]
    error_message = "Key Policy が plan の role に、2つの root の state の object に限った復号だけを許していません（plan は state に書かない）。"
  }

  assert {
    condition = [for s in jsondecode(module.kms.key_policy).Statement : s if try(s.Principal.AWS, null) == ["arn:aws:iam::123456789012:role/tabp-admin-ci-unlock"]] == [{
      Sid       = "RolesViaS3Read1"
      Effect    = "Allow"
      Principal = { AWS = ["arn:aws:iam::123456789012:role/tabp-admin-ci-unlock"] }
      Action    = "kms:Decrypt"
      Resource  = "*"
      Condition = { StringEquals = {
        "kms:ViaService"                   = "s3.ap-northeast-1.amazonaws.com"
        "kms:EncryptionContext:aws:s3:arn" = ["arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/workload/terraform.tfstate.tflock"]
      } }
    }]
    error_message = "Key Policy が unlock の role に、workload root の lock file の object に限った復号だけを許していません。"
  }

  assert {
    condition = [for s in jsondecode(module.kms.key_policy).Statement : s if try(s.Principal.AWS, null) == ["arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"]] == [{
      Sid       = "RolesViaS3Read2"
      Effect    = "Allow"
      Principal = { AWS = ["arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"] }
      Action    = "kms:Decrypt"
      Resource  = "*"
      Condition = { StringEquals = {
        "kms:ViaService"                   = "s3.ap-northeast-1.amazonaws.com"
        "kms:EncryptionContext:aws:s3:arn" = ["arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/identity/terraform.tfstate.tflock"]
      } }
    }]
    error_message = "Key Policy が unlock_identity の role に、identity root の lock file の object に限った復号だけを許していません。"
  }
}

run "ci_role_arns_unlock_empty" {
  command = plan

  variables {
    ci_role_arns = {
      plan            = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan"
      apply           = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply"
      apply_identity  = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
      unlock_identity = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"
      unlock          = ""
    }
  }

  expect_failures = [var.ci_role_arns]
}

run "ci_role_arns_unlock_wildcard" {
  command = plan

  variables {
    ci_role_arns = {
      plan            = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan"
      apply           = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply"
      apply_identity  = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
      unlock_identity = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"
      unlock          = "arn:aws:iam::123456789012:role/tabp-admin-ci-*"
    }
  }

  expect_failures = [var.ci_role_arns]
}

run "ci_role_arns_unlock_no_name" {
  command = plan

  variables {
    ci_role_arns = {
      plan            = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan"
      apply           = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply"
      apply_identity  = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
      unlock_identity = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"
      unlock          = "arn:aws:iam::123456789012:role/"
    }
  }

  expect_failures = [var.ci_role_arns]
}

run "ci_role_arns_same_role_plan_unlock_workload" {
  command = plan

  variables {
    ci_role_arns = {
      plan            = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan"
      apply           = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply"
      apply_identity  = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
      unlock_identity = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"
      unlock          = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan"
    }
  }

  expect_failures = [var.ci_role_arns]
}
