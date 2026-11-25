data "aws_caller_identity" "current" {}

data "aws_partition" "current" {}

data "aws_region" "current" {}

locals {
  # CloudFormation が stack の置き先のアカウントと partition で埋める。
  iam_prefix = "arn:$${AWS::Partition}:iam::$${AWS::AccountId}"

  # メンバーの state-backend と deployment-identity の refresh に要る読み取りだけを持つ。state の object と鍵の暗号操作は与えない（organization-baseline/ADR-0203）。
  # 管理アカウントの plan の role の読み取りを覆うことは plan.organization_baseline_bootstrap_read が見る。
  bucket_arns = [for s in ["tfstate", "tfplan", "tfapply"] : { "Fn::Sub" = "arn:$${AWS::Partition}:s3:::${var.project}-*-${s}-$${AWS::AccountId}-*" }]
  read_statements = [
    { Sid = "ListBuckets", Effect = "Allow", Action = "s3:ListBucket", Resource = local.bucket_arns },
    {
      Sid    = "ReadBucketConfiguration"
      Effect = "Allow"
      Action = [
        "s3:GetAccelerateConfiguration",
        "s3:GetBucketAcl",
        "s3:GetBucketCORS",
        "s3:GetBucketLogging",
        "s3:GetBucketObjectLockConfiguration",
        "s3:GetBucketOwnershipControls",
        "s3:GetBucketPolicy",
        "s3:GetBucketPublicAccessBlock",
        "s3:GetBucketRequestPayment",
        "s3:GetBucketTagging",
        "s3:GetBucketVersioning",
        "s3:GetBucketWebsite",
        "s3:GetEncryptionConfiguration",
        "s3:GetLifecycleConfiguration",
        "s3:GetReplicationConfiguration",
      ]
      Resource = local.bucket_arns
    },
    {
      Sid      = "ReadStateKey"
      Effect   = "Allow"
      Action   = ["kms:DescribeKey", "kms:GetKeyPolicy", "kms:GetKeyRotationStatus", "kms:ListResourceTags"]
      Resource = { "Fn::Sub" = "arn:$${AWS::Partition}:kms:$${AWS::Region}:$${AWS::AccountId}:key/*" }
    },
    { Sid = "ListKeyAliases", Effect = "Allow", Action = "kms:ListAliases", Resource = "*" },
    {
      Sid      = "ReadGitHubProvider"
      Effect   = "Allow"
      Action   = ["iam:GetOpenIDConnectProvider", "iam:ListOpenIDConnectProviderTags"]
      Resource = { "Fn::Sub" = "${local.iam_prefix}:oidc-provider/token.actions.githubusercontent.com" }
    },
    {
      Sid      = "ReadCiRoles"
      Effect   = "Allow"
      Action   = ["iam:GetRole", "iam:GetRolePolicy", "iam:ListAttachedRolePolicies", "iam:ListRolePolicies", "iam:ListRoleTags"]
      Resource = { "Fn::Sub" = "${local.iam_prefix}:role/${var.project}-*-ci-*" }
    },
    {
      Sid      = "ReadCiPolicies"
      Effect   = "Allow"
      Action   = ["iam:GetPolicy", "iam:GetPolicyVersion", "iam:ListPolicyTags", "iam:ListPolicyVersions"]
      Resource = { "Fn::Sub" = "${local.iam_prefix}:policy/${var.project}-*-ci-*" }
    },
  ]

  template = {
    AWSTemplateFormatVersion = "2010-09-09"
    Resources = {
      BootstrapReadRole = {
        Type = "AWS::IAM::Role"
        Properties = {
          RoleName           = var.role_name
          Path               = var.path
          MaxSessionDuration = 3600
          # Principal に role の ARN を書くと、role を作り直したときに一意 ID がずれて信頼が壊れる。アカウントの root と aws:PrincipalArn で限る。
          AssumeRolePolicyDocument = {
            Version = "2012-10-17"
            Statement = [{
              Effect    = "Allow"
              Principal = { AWS = "arn:${data.aws_partition.current.partition}:iam::${data.aws_caller_identity.current.account_id}:root" }
              Action    = "sts:AssumeRole"
              Condition = { ArnEquals = { "aws:PrincipalArn" = var.trusted_principal_arn } }
            }]
          }
          Policies = [{
            PolicyName     = "bootstrap-read"
            PolicyDocument = { Version = "2012-10-17", Statement = local.read_statements }
          }]
        }
      }
    }
  }
}

# メンバーアカウントの立ち上げの読み取りの role（organization-baseline/ADR-0203）。description は ASCII に限る。
resource "aws_cloudformation_stack_set" "this" {
  name             = var.role_name
  description      = "Read-only role for bootstrapping member accounts"
  permission_model = "SERVICE_MANAGED"
  capabilities     = ["CAPABILITY_NAMED_IAM"]

  auto_deployment {
    enabled                          = true
    retain_stacks_on_account_removal = false
  }

  managed_execution {
    active = true
  }

  template_body = jsonencode(local.template)

  lifecycle {
    prevent_destroy = true
  }
}

resource "aws_cloudformation_stack_instances" "this" {
  stack_set_name = aws_cloudformation_stack_set.this.name
  regions        = [data.aws_region.current.region]

  deployment_targets {
    organizational_unit_ids = [var.organization_root_id]
  }

  lifecycle {
    prevent_destroy = true
  }
}
