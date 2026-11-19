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
  name                     = "tabp-dev/api"
  publisher_role_name      = "tabp-dev-ecr-publish-api"
  kms_key_arn              = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  retained_images          = 30
  github_oidc_provider_arn = "arn:aws:iam::123456789012:oidc-provider/token.actions.githubusercontent.com"
  publisher = {
    owner         = "example"
    owner_id      = "1000001"
    repository    = "go-boilerplate"
    repository_id = "2000001"
    environment   = "dev-publish"
  }
}

run "lifecycle_keeps_retained_tagged_images" {
  command = plan

  assert {
    condition = jsondecode(aws_ecr_lifecycle_policy.this.policy) == {
      rules = [{
        rulePriority = 1
        description  = "keep the newest tagged images"
        selection = {
          tagStatus      = "tagged"
          tagPatternList = ["*"]
          countType      = "imageCountMoreThan"
          countNumber    = 30
        }
        action = { type = "expire" }
      }]
    }
    error_message = "lifecycle の規則が、タグ付きのイメージを retained_images 件残す1つの規則になっていません。"
  }
}

run "repository_policy_names_this_repository_and_publisher" {
  command = plan

  assert {
    condition = jsondecode(aws_ecr_repository_policy.this.policy) == {
      Version = "2012-10-17"
      Statement = [
        {
          Sid       = "DenyWriteExceptPublisher"
          Effect    = "Deny"
          Principal = { AWS = "arn:aws:iam::123456789012:root" }
          Action    = ["ecr:CompleteLayerUpload", "ecr:InitiateLayerUpload", "ecr:PutImage", "ecr:UploadLayerPart"]
          Resource  = "arn:aws:ecr:ap-northeast-1:123456789012:repository/tabp-dev/api"
          Condition = { ArnNotEquals = { "aws:PrincipalArn" = "arn:aws:iam::123456789012:role/tabp-dev-ecr-publish-api" } }
        },
        {
          Sid       = "DenyImageDeletion"
          Effect    = "Deny"
          Principal = { AWS = "arn:aws:iam::123456789012:root" }
          Action    = "ecr:BatchDeleteImage"
          Resource  = "arn:aws:ecr:ap-northeast-1:123456789012:repository/tabp-dev/api"
        },
      ]
    }
    error_message = "repository policy が、この repository の ARN と push の role の ARN を名指していません。"
  }
}

run "publisher_trust_is_immutable_environment_sub" {
  command = plan

  assert {
    condition = jsondecode(aws_iam_role.publisher.assume_role_policy).Statement[0].Condition.StringEquals == {
      "token.actions.githubusercontent.com:aud"                 = "sts.amazonaws.com"
      "token.actions.githubusercontent.com:sub"                 = "repo:example@1000001/go-boilerplate@2000001:environment:dev-publish"
      "token.actions.githubusercontent.com:repository_id"       = "2000001"
      "token.actions.githubusercontent.com:repository_owner_id" = "1000001"
    }
    error_message = "push の role の信頼条件が、入力の publisher の immutable 形式の sub と ID になっていません。"
  }

  assert {
    condition     = jsondecode(aws_iam_role.publisher.assume_role_policy).Statement[0].Principal == { Federated = var.github_oidc_provider_arn }
    error_message = "push の role の信頼条件の Principal が入力の OIDC provider ではありません。"
  }
}

run "publisher_policy_targets_this_repository" {
  command = plan

  assert {
    condition     = jsondecode(aws_iam_role_policy.publisher.policy).Statement[0].Resource == "arn:aws:ecr:ap-northeast-1:123456789012:repository/tabp-dev/api"
    error_message = "push の role の Publish の文の Resource が、この repository の ARN ではありません。"
  }
}

run "lifecycle_follows_retained_images" {
  command = plan

  variables {
    retained_images = 2
  }

  assert {
    condition     = jsondecode(aws_ecr_lifecycle_policy.this.policy).rules[0].selection.countNumber == 2
    error_message = "lifecycle の countNumber が retained_images に従っていません。"
  }
}
