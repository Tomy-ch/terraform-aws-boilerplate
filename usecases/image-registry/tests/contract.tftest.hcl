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
  project                   = "tabp"
  env                       = "dev"
  github_oidc_provider_arn  = "arn:aws:iam::123456789012:oidc-provider/token.actions.githubusercontent.com"
  registry_manager_role_arn = "arn:aws:iam::123456789012:role/tabp-dev-ci-apply"
  repositories = {
    api = {
      retained_images = 30
      publisher = {
        owner         = "example"
        owner_id      = "1000001"
        repository    = "go-boilerplate"
        repository_id = "2000001"
        environment   = "dev-publish"
      }
    }
  }
}

run "outputs_shape" {
  command = plan

  assert {
    condition     = keys(output.repositories) == ["api"] && keys(output.repositories["api"]) == ["arn", "url"]
    error_message = "repositories の出力が、論理名ごとの arn と url になっていません。"
  }

  assert {
    condition     = keys(output.publisher_role_arns) == ["api"]
    error_message = "publisher_role_arns の出力のキーが入力の論理名になっていません。"
  }
}
