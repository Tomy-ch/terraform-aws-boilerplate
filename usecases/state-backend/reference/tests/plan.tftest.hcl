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

run "reference_bucket_name" {
  command = plan

  assert {
    condition     = module.state_backend.bucket_name == "tabp-admin-tfstate-123456789012-ap-northeast-1-an"
    error_message = "reference の値から state Bucket の名前が組まれていません。"
  }

  assert {
    condition     = module.state_backend.plan_output_bucket_name == "tabp-admin-tfplan-123456789012-ap-northeast-1-an"
    error_message = "reference の値から plan の出力の Bucket の名前が組まれていません。"
  }

  assert {
    condition     = module.state_backend.state_key_alias_name == "alias/tabp-admin-tfstate"
    error_message = "reference の値から state の鍵の alias の名前が組まれていません。"
  }
}
