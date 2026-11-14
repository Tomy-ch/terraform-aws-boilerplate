mock_provider "aws" {
  # state_key_arn と access_log_* の precondition は、ARN を data source の partition・region・account と比べる。
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

variables {
  name_prefix            = "tabp-admin"
  env                    = "admin"
  github                 = { owner = "octo-org", owner_id = "123456", repository = "octo-repo", repository_id = "456789" }
  state_bucket_arn       = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an"
  state_key_arn          = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  plan_output_bucket_arn = "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an"

  access_log_group_arn       = "arn:aws:logs:ap-northeast-1:123456789012:log-group:tabp-admin-state-backend-access"
  access_log_destination_arn = "arn:aws:logs:ap-northeast-1:123456789012:delivery-destination:tabp-admin-state-backend-access"
  access_log_source_arns = [
    "arn:aws:logs:ap-northeast-1:123456789012:delivery-source:tabp-admin-tfstate",
    "arn:aws:logs:ap-northeast-1:123456789012:delivery-source:tabp-admin-tfplan",
  ]
}

run "github_provider" {
  command = plan

  assert {
    condition     = aws_iam_openid_connect_provider.github.url == "https://token.actions.githubusercontent.com"
    error_message = "OIDC provider の URL が GitHub Actions の発行者になっていません。"
  }

  assert {
    condition     = aws_iam_openid_connect_provider.github.client_id_list == toset(["sts.amazonaws.com"])
    error_message = "OIDC provider の audience が sts.amazonaws.com の1つだけになっていません。"
  }
}
