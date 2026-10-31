# Policy Test の入力を作る（make tf-policy）。Bucket の ARN を与える理由は conftest/plan/README.md の「入力」。
mock_provider "aws" {
  override_during = plan

  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
  mock_data "aws_region" {
    defaults = { region = "ap-northeast-1" }
  }
}

override_resource {
  target          = module.state_backend.module.s3.aws_s3_bucket.this
  override_during = plan
  values          = { arn = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an" }
}

run "policy_input" {
  command = plan
}
