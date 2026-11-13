# Policy Test の入力を作る（make tf-policy）。Bucket の ARN を与える理由は conftest/plan/README.md の「入力」。
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
  target          = module.state_backend.module.s3.aws_s3_bucket.this
  override_during = plan
  values          = { arn = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an" }
}

override_resource {
  target          = module.state_backend.module.plan_output.aws_s3_bucket.this
  override_during = plan
  values          = { arn = "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an" }
}

# state Bucket の Bucket Policy は state の鍵の ARN を条件の値に持つので、鍵の ARN を与えないと文書が plan で決まらない。
override_resource {
  target          = module.state_backend.module.kms.aws_kms_key.this
  override_during = plan
  values          = { arn = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab" }
}

run "policy_input" {
  command = plan
}
