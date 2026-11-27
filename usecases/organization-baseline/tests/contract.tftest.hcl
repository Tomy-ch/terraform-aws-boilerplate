# 公開 output の名前と、それが返す値の配線を固定する（repository-wide/ADR-0202 決定4）。
mock_provider "aws" {
  mock_data "aws_ssm_parameter" {
    defaults = { value = "{\"security\":\"security@example.com\",\"log-archive\":\"log-archive@example.com\"}" }
  }
  mock_data "aws_caller_identity" {
    defaults = { account_id = "111111111111" }
  }
  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }
  mock_data "aws_region" {
    defaults = { region = "ap-northeast-1" }
  }
  mock_resource "aws_organizations_organization" {
    defaults = {
      id                = "o-exampleorg1"
      master_account_id = "111111111111"
      roots             = [{ id = "r-exam", arn = "arn:aws:organizations::111111111111:root/o-exampleorg1/r-exam", name = "Root", policy_types = [] }]
    }
  }
  # OU の ID は子の OU の parent_id の検査を通る形にする。
  mock_resource "aws_organizations_organizational_unit" {
    defaults = { id = "ou-exam-abcdefgh" }
  }
  # 委任管理者の指定は、Provider がアカウントの ID を12桁の数字として検査する。
  mock_resource "aws_organizations_account" {
    defaults = { id = "111111111111" }
  }
  # 付与と割当は、Provider が permission set の ARN の形を検査する。
  mock_resource "aws_ssoadmin_permission_set" {
    defaults = { arn = "arn:aws:sso:::permissionSet/ssoins-0000000000000000/ps-0000000000000000" }
  }
}

variables {
  project                      = "tabp"
  bootstrap_read_principal_arn = "arn:aws:iam::111111111111:role/tabp-admin-ci-plan"
  state_bucket_arn             = "arn:aws:s3:::tabp-admin-tfstate-111111111111-ap-northeast-1-an"
  plan_output_bucket_arn       = "arn:aws:s3:::tabp-admin-tfplan-111111111111-ap-northeast-1-an"
  apply_output_bucket_arn      = "arn:aws:s3:::tabp-admin-tfapply-111111111111-ap-northeast-1-an"
  state_key_arn                = "arn:aws:kms:ap-northeast-1:111111111111:key/00000000-0000-0000-0000-000000000000"
}

override_data {
  target = module.access[0].data.aws_ssoadmin_instances.this
  values = { arns = ["arn:aws:sso:::instance/ssoins-0000000000000000"], identity_store_ids = ["d-0000000000"] }
}

override_data {
  target = module.access[0].data.aws_ssm_parameter.principals
  values = { value = "{\"users\":{\"owner\":\"00000000-0000-0000-0000-000000000001\"},\"groups\":{\"operators\":\"00000000-0000-0000-0000-000000000002\"}}" }
}

# 管理アカウントとメンバーアカウントの割当の対象を見分けるため、メンバーの ID を変える。
override_resource {
  target = module.organization.aws_organizations_account.this["security"]
  values = { id = "222222222222" }
}

override_resource {
  target = module.organization.aws_organizations_account.this["log-archive"]
  values = { id = "333333333333" }
}

override_resource {
  target = module.break_glass.aws_iam_user.this["break-glass-1"]
  values = { arn = "arn:aws:iam::111111111111:user/break-glass/break-glass-1" }
}

override_resource {
  target = module.break_glass.aws_iam_user.this["break-glass-2"]
  values = { arn = "arn:aws:iam::111111111111:user/break-glass/break-glass-2" }
}

run "outputs" {
  command = apply

  assert {
    condition     = output.break_glass_arns == module.break_glass.arns
    error_message = "break_glass_arns が、管理アカウントの break-glass の IAM の ARN を返していません。"
  }

  assert {
    condition     = [for a in output.break_glass_arns : split("/", split(":", a)[5])[0]] == ["user", "user", "group", "role"]
    error_message = "break_glass_arns は user 2つ・group・role の順の4つです。"
  }

  assert {
    condition     = output.account_ids == module.organization.account_ids && keys(output.account_ids) == ["log-archive", "security"]
    error_message = "account_ids が、メンバーアカウントの env から ID への map を返していません。"
  }

  assert {
    condition     = module.organization.delegated_admin.regions == module.default_regions.by_partition["aws"] && module.organization.delegated_admin.account_id == output.account_ids["security"]
    error_message = "セキュリティのサービスの委任管理者が、security のアカウントに、repository-wide/ADR-0209 の集合の各リージョンで指定されていません。"
  }

  assert {
    condition     = output.bootstrapping_account_ids == {}
    error_message = "bootstrapping の既定（空）で、bootstrapping_account_ids が空になっていません。"
  }

  assert {
    condition     = output.bootstrap_read_role == { name = "${var.project}-bootstrap-read", path = "/bootstrap/" }
    error_message = "bootstrap_read_role が、メンバーへ配る読み取りの role の名前と path を返していません。"
  }

  assert {
    condition     = output.member_break_glass_role == { name = "${var.project}-break-glass", path = "/break-glass/" }
    error_message = "member_break_glass_role が、メンバーへ配る break-glass の role の名前と path を返していません。"
  }

  assert {
    condition     = output.identity_center_instance_arn == null
    error_message = "identity_center が null のとき、identity_center_instance_arn は null です。"
  }
}

run "identity_center_outputs" {
  command = apply

  variables {
    identity_center = {
      management_assignments = [{ permission_set = "organization_admin", user = "owner" }]
      member_assignments     = [{ account = "security", permission_set = "read_only", group = "operators" }]
    }
  }

  assert {
    condition     = output.identity_center_instance_arn == "arn:aws:sso:::instance/ssoins-0000000000000000"
    error_message = "identity_center_instance_arn が、Identity Center のインスタンスの ARN を返していません。"
  }

  assert {
    condition = module.access[0].assignment_target_ids == {
      management = { "organization_admin/owner" = module.organization.management_account_id }
      member     = { "security/read_only/operators" = output.account_ids["security"] }
    }
    error_message = "割当の対象が、組織の管理アカウントと、env から引いたメンバーアカウントになっていません。"
  }

  assert {
    condition     = module.organization.management_account_id == "111111111111" && output.account_ids["security"] == "222222222222"
    error_message = "組織の管理アカウントとメンバーアカウントの ID が、mock の値と違います。"
  }

  assert {
    condition = (
      module.access[0].organization_admin_deny_resources.DenyBootstrapBucketChange == flatten([for b in [var.state_bucket_arn, var.plan_output_bucket_arn, var.apply_output_bucket_arn] : [b, "${b}/*"]])
      && module.access[0].organization_admin_deny_resources.DenyStateKeyChange == var.state_key_arn
      && module.access[0].organization_admin_deny_resources.DenyCiIdentityChange[0] == "arn:aws:iam::111111111111:role/tabp-admin-ci-*"
    )
    error_message = "organization_admin の Deny が、渡した3つの Bucket・state の鍵と、組織の管理アカウントの CI の role を名指していません。"
  }
}
