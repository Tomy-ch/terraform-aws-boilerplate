# root が組んだ permissions boundary の ARN が、送り側の deployment-identity が公開する
# permissions_boundary_arns["ops-notification"] と一致することを、両方を同じテストで走らせて確かめる
# （ADR-0102 決定16 のただし書き）。deployment-identity の入力は identity root（env/admin/identity）と同じ project と env にする。
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

run "sender" {
  command = plan

  module {
    source = "../../usecases/deployment-identity"
  }

  variables {
    project = "tabp"
    env     = "admin"
    github = {
      owner         = "Tomy-ch"
      owner_id      = "182787471"
      repository    = "terraform-aws-boilerplate"
      repository_id = "1370808564"
    }
    state_bucket_arn        = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an"
    state_key_arn           = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
    plan_output_bucket_arn  = "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an"
    apply_output_bucket_arn = "arn:aws:s3:::tabp-admin-tfapply-123456789012-ap-northeast-1-an"
    break_glass_arns        = ["arn:aws:iam::123456789012:user/break-glass/break-glass-1", "arn:aws:iam::123456789012:group/break-glass/break-glass", "arn:aws:iam::123456789012:role/break-glass/tabp-break-glass"]
    bootstrap_read_role     = { name = "tabp-bootstrap-read", path = "/bootstrap/" }
  }
}

run "receiver_matches_the_sender" {
  command = plan

  assert {
    condition     = output.ops_notification_permissions_boundary_arn == run.sender.permissions_boundary_arns["ops-notification"]
    error_message = "root が組んだ permissions boundary の ARN が、deployment-identity の permissions_boundary_arns[\"ops-notification\"] と一致しません。"
  }
}
