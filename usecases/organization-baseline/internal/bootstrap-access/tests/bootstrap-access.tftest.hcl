mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "111111111111" }
  }
  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }
  mock_data "aws_region" {
    defaults = { region = "ap-northeast-1" }
  }
}

variables {
  project               = "tabp"
  role_name             = "tabp-bootstrap-read"
  path                  = "/bootstrap/"
  trusted_principal_arn = "arn:aws:iam::111111111111:role/tabp-admin-ci-plan"
  organization_root_id  = "r-exam"
}

run "stack_set" {
  command = plan

  assert {
    condition = (
      aws_cloudformation_stack_set.this.name == "tabp-bootstrap-read"
      && aws_cloudformation_stack_set.this.permission_model == "SERVICE_MANAGED"
      && aws_cloudformation_stack_set.this.auto_deployment[0].enabled
      && aws_cloudformation_stack_set.this.auto_deployment[0].retain_stacks_on_account_removal == false
      && aws_cloudformation_stack_set.this.managed_execution[0].active
      && aws_cloudformation_stack_set.this.capabilities == toset(["CAPABILITY_NAMED_IAM"])
    )
    error_message = "StackSet は role の名前で、service-managed・自動展開・stack を残さない・managed execution・CAPABILITY_NAMED_IAM にしてください。"
  }

  assert {
    condition     = aws_cloudformation_stack_instances.this.regions == toset(["ap-northeast-1"]) && aws_cloudformation_stack_instances.this.deployment_targets[0].organizational_unit_ids == toset(["r-exam"])
    error_message = "stack instance の対象は組織の Root、region は provider の region だけにしてください。"
  }

  assert {
    condition     = keys(jsondecode(aws_cloudformation_stack_set.this.template_body).Resources) == ["BootstrapReadRole"] && jsondecode(aws_cloudformation_stack_set.this.template_body).Resources.BootstrapReadRole.Type == "AWS::IAM::Role"
    error_message = "テンプレートは AWS::IAM::Role を1つだけ持ちます。"
  }
}

run "role" {
  command = plan

  assert {
    condition = {
      for k, v in jsondecode(aws_cloudformation_stack_set.this.template_body).Resources.BootstrapReadRole.Properties : k => v if k != "Policies"
      } == {
      RoleName           = "tabp-bootstrap-read"
      Path               = "/bootstrap/"
      MaxSessionDuration = 3600
      AssumeRolePolicyDocument = {
        Version = "2012-10-17"
        Statement = [{
          Effect    = "Allow"
          Principal = { AWS = "arn:aws:iam::111111111111:root" }
          Action    = "sts:AssumeRole"
          Condition = { ArnEquals = { "aws:PrincipalArn" = "arn:aws:iam::111111111111:role/tabp-admin-ci-plan" } }
        }]
      }
    }
    error_message = "role の名前・path・セッションの長さと、信頼条件（管理アカウントと、引き受けてよい role の ARN）が違います。managed policy と tag は持たせません。"
  }

  assert {
    condition     = length(jsondecode(aws_cloudformation_stack_set.this.template_body).Resources.BootstrapReadRole.Properties.Policies) == 1
    error_message = "inline policy は1つだけにしてください。"
  }

  # 書き込みと、state の object・鍵の暗号操作を与えない（organization-baseline/ADR-0203）。
  assert {
    condition = alltrue([
      for s in jsondecode(aws_cloudformation_stack_set.this.template_body).Resources.BootstrapReadRole.Properties.Policies[0].PolicyDocument.Statement : s.Effect == "Allow" && alltrue([
        for a in flatten([s.Action]) : length(regexall("^(s3|kms|iam):(Get|List|Describe)[A-Za-z]+$", a)) == 1 && !contains(["s3:GetObject", "s3:GetObjectVersion"], a)
      ])
    ])
    error_message = "読み取りの role の inline policy は、s3・kms・iam の Get・List・Describe の Allow だけにし、state の object を読ませないでください。"
  }

  assert {
    condition = [
      for s in jsondecode(aws_cloudformation_stack_set.this.template_body).Resources.BootstrapReadRole.Properties.Policies[0].PolicyDocument.Statement : s.Resource if s.Sid == "ReadCiRoles"
    ] == [{ "Fn::Sub" = "arn:$${AWS::Partition}:iam::$${AWS::AccountId}:role/tabp-*-ci-*" }]
    error_message = "CI の role の読み取りは、置き先のアカウントの <project>-*-ci-* の role に限ってください。"
  }

  # kms:ListAliases のほかは、置き先のアカウントの名前の形に限る（organization-baseline/ADR-0203 決定4）。
  assert {
    condition = {
      for s in jsondecode(aws_cloudformation_stack_set.this.template_body).Resources.BootstrapReadRole.Properties.Policies[0].PolicyDocument.Statement : s.Sid => s.Resource if s.Sid != "ReadCiRoles"
      } == {
      ListBuckets             = [for b in ["tfstate", "tfplan", "tfapply"] : { "Fn::Sub" = "arn:$${AWS::Partition}:s3:::tabp-*-${b}-$${AWS::AccountId}-*" }]
      ReadBucketConfiguration = [for b in ["tfstate", "tfplan", "tfapply"] : { "Fn::Sub" = "arn:$${AWS::Partition}:s3:::tabp-*-${b}-$${AWS::AccountId}-*" }]
      ReadStateKey            = { "Fn::Sub" = "arn:$${AWS::Partition}:kms:$${AWS::Region}:$${AWS::AccountId}:key/*" }
      ListKeyAliases          = "*"
      ReadGitHubProvider      = { "Fn::Sub" = "arn:$${AWS::Partition}:iam::$${AWS::AccountId}:oidc-provider/token.actions.githubusercontent.com" }
      ReadCiPolicies          = { "Fn::Sub" = "arn:$${AWS::Partition}:iam::$${AWS::AccountId}:policy/tabp-*-ci-*" }
      ReadServiceLinkedRoles  = { "Fn::Sub" = "arn:$${AWS::Partition}:iam::$${AWS::AccountId}:role/aws-service-role/management.chatbot.amazonaws.com/AWSServiceRoleForAWSChatbot" }
    }
    error_message = "読み取りの Resource を、置き先のアカウントの state-backend と deployment-identity の名前の形に限ってください（kms:ListAliases だけ Resource \"*\"）。"
  }
}

run "rejects_path_without_leading_slash" {
  command = plan

  variables {
    path = "bootstrap/"
  }

  expect_failures = [var.path]
}

run "rejects_path_without_trailing_slash" {
  command = plan

  variables {
    path = "/bootstrap"
  }

  expect_failures = [var.path]
}
