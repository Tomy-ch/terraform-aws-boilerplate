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
}

variables {
  project                      = "tabp"
  bootstrap_read_principal_arn = "arn:aws:iam::111111111111:role/tabp-admin-ci-plan"
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
}
