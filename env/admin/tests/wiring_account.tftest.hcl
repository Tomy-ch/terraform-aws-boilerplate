# アカウントと partition を wiring.tftest.hcl と違う値にする。ARN をそれらの data から組んでいなければ落ちる。
mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "210987654321" }
  }
  mock_data "aws_region" {
    defaults = { region = "cn-north-1" }
  }
  mock_data "aws_partition" {
    defaults = { partition = "aws-cn" }
  }
}

# ops-notification は aws の partition の外では動かない（ops-notification/ADR-0001 決定1）ので、この root の配線だけを見る。
override_module {
  target = module.ops_notification
}

run "boundary_arn_is_built_from_the_account_and_partition" {
  command = plan

  assert {
    condition     = output.ops_notification_permissions_boundary_arn == "arn:aws-cn:iam::210987654321:policy/tabp-admin-ci-boundary-ops-notification"
    error_message = "permissions boundary の ARN が、apply するアカウントと partition から組まれていません。"
  }
}
