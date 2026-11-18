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

run "names_follow_project_env" {
  command = plan

  assert {
    condition     = module.repository["api"].repository.arn == "arn:aws:ecr:ap-northeast-1:123456789012:repository/tabp-dev/api"
    error_message = "repository の名前が <project>-<env>/<論理名> になっていません。"
  }

  assert {
    condition     = output.publisher_role_arns["api"] == "arn:aws:iam::123456789012:role/tabp-dev-ecr-publish-api"
    error_message = "push の role の名前が <project>-<env>-ecr-publish-<論理名> になっていません。"
  }

  assert {
    condition     = module.kms.alias_name == "alias/tabp-dev-ecr"
    error_message = "鍵の alias が alias/<project>-<env>-ecr になっていません。"
  }
}

# role の名前の上限（64 文字）に当たるのは project と論理名の長さの和で、上限の組み合わせでも収まる。
run "longest_names_fit" {
  command = plan

  variables {
    project = "abcdefghijklmnopqrst"
    env     = "sandbox"
    repositories = {
      abcdefghijklmnopqrst = {
        retained_images = 2
        publisher = {
          owner         = "example"
          owner_id      = "1000001"
          repository    = "go-boilerplate"
          repository_id = "2000001"
          environment   = "sandbox-publish"
        }
      }
    }
  }

  assert {
    condition     = length(output.publisher_role_arns["abcdefghijklmnopqrst"]) > 0
    error_message = "上限の長さの project と論理名で plan が通りません。"
  }
}
