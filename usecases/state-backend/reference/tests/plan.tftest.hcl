mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
  mock_data "aws_region" {
    defaults = { region = "ap-northeast-1" }
  }
}

run "reference_bucket_name" {
  command = plan

  assert {
    condition     = module.state_backend.bucket_name == "tabp-admin-tfstate-123456789012-ap-northeast-1-an"
    error_message = "reference の値から state Bucket の名前が組まれていません。"
  }
}
