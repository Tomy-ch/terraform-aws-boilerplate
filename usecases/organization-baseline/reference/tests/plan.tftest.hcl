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

override_data {
  target = module.organization_baseline.module.access[0].data.aws_ssoadmin_instances.this
  values = { arns = ["arn:aws:sso:::instance/ssoins-0000000000000000"], identity_store_ids = ["d-0000000000"] }
}

override_data {
  target = module.organization_baseline.module.access[0].data.aws_ssm_parameter.principals
  values = { value = "{\"users\":{\"owner\":\"00000000-0000-0000-0000-000000000001\"},\"groups\":{\"operators\":\"00000000-0000-0000-0000-000000000002\"}}" }
}

run "reference" {
  command = apply

  assert {
    condition     = length(module.organization_baseline.break_glass_arns) == 4
    error_message = "reference の値から break-glass の IAM が組まれていません。"
  }

  assert {
    condition     = module.organization_baseline.identity_center_instance_arn == "arn:aws:sso:::instance/ssoins-0000000000000000"
    error_message = "reference の値から Identity Center の permission set と割当が組まれていません。"
  }

  assert {
    condition     = length(local.identity_center.management_assignments) == 5 && length(local.identity_center.member_assignments) == 8
    error_message = "reference の割当は、管理アカウントへ permission set 5つ、メンバー2つへ organization_admin の外の4つずつです。"
  }
}
