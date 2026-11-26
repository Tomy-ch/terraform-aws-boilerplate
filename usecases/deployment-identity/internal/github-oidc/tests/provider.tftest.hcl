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

variables {
  name_prefix             = "tabp-admin"
  env                     = "admin"
  bootstrap_read_role     = { name = "tabp-bootstrap-read", path = "/bootstrap/" }
  github                  = { owner = "octo-org", owner_id = "123456", repository = "octo-repo", repository_id = "456789" }
  state_bucket_arn        = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an"
  state_key_arn           = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  plan_output_bucket_arn  = "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an"
  apply_output_bucket_arn = "arn:aws:s3:::tabp-admin-tfapply-123456789012-ap-northeast-1-an"
  break_glass_arns        = ["arn:aws:iam::123456789012:user/break-glass/break-glass-1", "arn:aws:iam::123456789012:user/break-glass/break-glass-2", "arn:aws:iam::123456789012:group/break-glass/break-glass", "arn:aws:iam::123456789012:role/break-glass/tabp-break-glass"]
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
