# Policy Test の入力を作る（make tf-policy）。
mock_provider "aws" {
  override_during = plan

  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }
  mock_data "aws_region" {
    defaults = { region = "ap-northeast-1" }
  }
  mock_data "aws_regions" {
    defaults = { names = ["ap-northeast-1", "us-east-1"] }
  }
  mock_resource "aws_iam_service_linked_role" {
    defaults = { arn = "arn:aws:iam::123456789012:role/aws-service-role/config.amazonaws.com/AWSServiceRoleForConfig" }
  }
}

run "policy_input" {
  command = plan
}
