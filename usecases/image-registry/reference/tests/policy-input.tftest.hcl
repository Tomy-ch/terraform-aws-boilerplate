# Policy Test の入力を作る（make tf-policy）。鍵の ARN を与える理由は conftest/plan/README.md の「入力」。
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
}

override_resource {
  target          = module.image_registry.module.kms.aws_kms_key.this
  override_during = plan
  values = {
    arn    = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
    policy = null
  }
}

run "policy_input" {
  command = plan
}
