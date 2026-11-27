# finops に渡す Topic は、provider の region の topic_arn である。us-east-1 の Topic は budgets と costalerts の
# 発行を許さない（ops-notification/ADR-0101 決定1）。取り違えれば、ARN でない us_east_1_topic_arn が finops の検査で落ちる。
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

override_module {
  target = module.ops_notification
  outputs = {
    topic_arn           = "arn:aws:sns:ap-northeast-1:123456789012:tabp-admin-ops-notification"
    us_east_1_topic_arn = "not-an-arn"
  }
}

run "finops_receives_the_provider_region_topic" {
  command = plan
}
