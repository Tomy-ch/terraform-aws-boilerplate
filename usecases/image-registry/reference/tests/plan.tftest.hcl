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

run "reference_repository" {
  command = plan

  assert {
    condition     = module.image_registry.repositories["api"].arn == "arn:aws:ecr:ap-northeast-1:123456789012:repository/tabp-dev/api"
    error_message = "reference の repository が tabp-dev/api になっていません。"
  }

  assert {
    condition     = module.image_registry.publisher_role_arns["api"] == "arn:aws:iam::123456789012:role/tabp-dev-ecr-publish-api"
    error_message = "reference の push の role が tabp-dev-ecr-publish-api になっていません。"
  }
}
