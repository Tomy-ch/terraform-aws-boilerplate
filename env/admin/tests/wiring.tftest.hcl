# root が ops-notification に渡す permissions boundary の ARN を、deployment-identity の permissions_boundary_arns と
# 同じ名前の規則（<project>-<env>-ci-boundary-<usecase>）で固定する。
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
  mock_data "aws_iam_role" {
    defaults = {
      path = "/aws-service-role/management.chatbot.amazonaws.com/"
      arn  = "arn:aws:iam::123456789012:role/aws-service-role/management.chatbot.amazonaws.com/AWSServiceRoleForAWSChatbot"
    }
  }
}

run "boundary_arn_follows_the_naming_rule" {
  command = plan

  assert {
    condition     = output.ops_notification_permissions_boundary_arn == "arn:aws:iam::123456789012:policy/tabp-admin-ci-boundary-ops-notification"
    error_message = "permissions boundary の ARN が <project>-<env>-ci-boundary-ops-notification の規則と違います。"
  }
}
