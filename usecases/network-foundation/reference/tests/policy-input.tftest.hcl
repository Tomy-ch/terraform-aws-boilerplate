# Policy Test の入力を作る（make tf-policy）。ARN を与える理由は conftest/plan/README.md の「入力」。
mock_provider "aws" {
  override_during = plan

  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
  mock_data "aws_region" {
    defaults = { region = "ap-northeast-1" }
  }
  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }
  mock_data "aws_availability_zones" {
    defaults = { zone_ids = ["apne1-az1", "apne1-az2", "apne1-az4"] }
  }
}

# 配送 role の permission policy と Log Group の validation が Log Group の ARN を持つ。
override_resource {
  target          = module.network_foundation.module.flow_logs.aws_cloudwatch_log_group.this
  override_during = plan
  values = {
    arn = "arn:aws:logs:ap-northeast-1:123456789012:log-group:/tabp/prd/network-foundation/vpc-flow-logs"
  }
}

# Log Group の kms_key_arn の validation と Key Policy の Resource が鍵の ARN を持つ。
override_resource {
  target          = module.network_foundation.module.kms.aws_kms_key.this
  override_during = plan
  values = {
    arn    = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
    policy = null
  }
}

run "policy_input" {
  command = plan
}
