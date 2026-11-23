# kics-scan disable=e592a0c5-5bdb-414c-9066-5dba7cdea370,e38a8e0a-b88b-4902-b3fe-b0fcb17d5c10
# e592a0c5（IAM Access Analyzer）: KICS はファイルごとに analyzer の宣言を求める。analyzer はアカウント単位の統制で account-baseline が持つ（docs/v1.0-plan/scope.md 第7節）。撤回: KICS がファイルの外の宣言を数えるようになったとき。
# e38a8e0a（tag）: tag は共通の単一入力でだけ付ける（ADR-0204 決定5）。この module はその入力を持たない。撤回: 共通の tag 入力を配線したとき。

locals {
  organization_id = aws_organizations_organization.this.id
  prod_path       = "${local.organization_id}/${local.root_id}/${aws_organizations_organizational_unit.top["Workloads"].id}/${aws_organizations_organizational_unit.workloads["Prod"].id}/"

  # メンバーの CI の apply 系と lock の解除の role（deployment-identity の `<project>-<env>-ci-<役割>`）。env を固定し、
  # 名前の一部だけが一致する role を例外にしない。
  state_writer_role_patterns = flatten([
    for env in ["security", "log-archive", "sandbox", "dev", "stg", "prd"] : [
      for suffix in ["apply", "apply-identity", "unlock", "unlock-identity"] : "arn:aws:iam::*:role/${var.project}-${env}-ci-${suffix}"
    ]
  ])
  break_glass_role_arn_pattern    = "arn:aws:iam::*:role${var.break_glass_role_path}${var.break_glass_role_name}"
  stacksets_exec_role_arn_pattern = "arn:aws:iam::*:role/stacksets-exec-*"

  service_control_policies = {
    deny-leave-and-close = {
      description = "メンバーが組織から離脱することと、アカウントを閉じることを拒む。"
      statements = [{
        Sid      = "DenyLeaveOrganizationAndCloseAccount"
        Effect   = "Deny"
        Action   = ["account:CloseAccount", "organizations:LeaveOrganization"]
        Resource = "*"
      }]
    }
    deny-bootstrap-role = {
      description = "立ち上げの後、メンバーの OrganizationAccountAccessRole の使用を拒む。"
      statements = [{
        Sid       = "DenyOrganizationAccountAccessRole"
        Effect    = "Deny"
        Action    = "*"
        Resource  = "*"
        Condition = { ArnLike = { "aws:PrincipalArn" = "arn:aws:iam::*:role/OrganizationAccountAccessRole" } }
      }]
    }
    deny-state-write = {
      description = "メンバーの state Bucket の object の書き込みを、CI の apply 系と lock の解除の role と break-glass の role に限る。"
      statements = [{
        Sid      = "DenyStateWriteExceptCi"
        Effect   = "Deny"
        Action   = ["s3:DeleteObject*", "s3:PutObject*", "s3:Replicate*", "s3:RestoreObject"]
        Resource = "arn:aws:s3:::${var.project}-*-tfstate-*/*"
        Condition = {
          ArnNotLike = { "aws:PrincipalArn" = concat(local.state_writer_role_patterns, [local.break_glass_role_arn_pattern]) }
        }
      }]
    }
    protect-governance-roles = {
      description = "メンバーの break-glass の role の変更を拒む。"
      statements = [{
        Sid      = "DenyBreakGlassRoleChange"
        Effect   = "Deny"
        Action   = ["iam:Attach*", "iam:Create*", "iam:Delete*", "iam:Detach*", "iam:PassRole", "iam:Put*", "iam:Tag*", "iam:Untag*", "iam:Update*"]
        Resource = local.break_glass_role_arn_pattern
        # StackSet の service-linked role は SCP の外だが、role を作る stacksets-exec-* は SCP を受ける（organization-baseline/ADR-0202）。
        Condition = { ArnNotLike = { "aws:PrincipalArn" = local.stacksets_exec_role_arn_pattern } }
        }, {
        # 例外は名前で当たるので、その名前の role をメンバーの主体に作らせない。作るのは SCP の外の service-linked role である。
        Sid      = "DenyStackSetsExecutionRoleChange"
        Effect   = "Deny"
        Action   = ["iam:Attach*", "iam:Create*", "iam:Delete*", "iam:Detach*", "iam:PassRole", "iam:Put*", "iam:Tag*", "iam:Untag*", "iam:Update*"]
        Resource = local.stacksets_exec_role_arn_pattern
      }]
    }
    deny-guardduty-stop = {
      description = "sandbox で GuardDuty を止めることと消すことを拒む。"
      statements = [{
        Sid    = "DenyGuardDutyStop"
        Effect = "Deny"
        Action = [
          "guardduty:DeleteDetector",
          "guardduty:DisassociateFromAdministratorAccount",
          "guardduty:DisassociateFromMasterAccount",
          "guardduty:UpdateDetector",
        ]
        Resource = "*"
      }]
    }
    deny-root-user = {
      description = "メンバーの root の要求を拒む。sts:AssumeRoot の特権のセッションは外す。"
      statements = [{
        Sid      = "DenyRootUser"
        Effect   = "Deny"
        Action   = "*"
        Resource = "*"
        Condition = {
          ArnLike = { "aws:PrincipalArn" = "arn:aws:iam::*:root" }
          Null    = { "aws:AssumedRoot" = "true" }
        }
      }]
    }
    deny-iam-user = {
      description = "メンバーで IAM user と長期の資格情報を作ることを拒む。"
      statements = [{
        Sid    = "DenyIamUserCredentials"
        Effect = "Deny"
        Action = [
          "iam:CreateAccessKey",
          "iam:CreateLoginProfile",
          "iam:CreateServiceSpecificCredential",
          "iam:CreateUser",
          "iam:UploadSSHPublicKey",
          "iam:UploadSigningCertificate",
        ]
        Resource = "*"
      }]
    }
    data-perimeter-prod-resources = {
      description = "下位の環境の主体が Prod の OU に在る resource を操作すること（role の引き受けを含む）を拒む。"
      statements = [{
        Sid       = "DenyProdResources"
        Effect    = "Deny"
        Action    = "*"
        Resource  = "*"
        Condition = { "ForAnyValue:StringLike" = { "aws:ResourceOrgPaths" = "${local.prod_path}*" } }
      }]
    }
  }

  # RCP は STS の Web Identity と SAML の引き受けを対象にしない。GitHub の OIDC と Identity Center の到達がそれを使う。
  data_perimeter_actions = ["kms:*", "s3:*", "secretsmanager:*"]

  resource_control_policies = {
    data-perimeter-org-principals = {
      description = "組織の外の主体が、S3・KMS・Secrets Manager の resource と role の引き受けに届くことを拒む。"
      statements = [{
        Sid       = "DenyPrincipalsOutsideOrganization"
        Effect    = "Deny"
        Principal = "*"
        Action    = concat(local.data_perimeter_actions, ["sts:AssumeRole"])
        Resource  = "*"
        Condition = {
          StringNotEqualsIfExists = { "aws:PrincipalOrgID" = local.organization_id }
          BoolIfExists            = { "aws:PrincipalIsAWSService" = "false" }
        }
      }]
    }
    data-perimeter-prod-principals = {
      description = "Prod の OU の外の主体が、Prod の S3・KMS・Secrets Manager に届くことと、管理アカウント以外から Prod の role を引き受けることを拒む。"
      statements = [
        {
          Sid       = "DenyPrincipalsOutsideProd"
          Effect    = "Deny"
          Principal = "*"
          Action    = local.data_perimeter_actions
          Resource  = "*"
          Condition = {
            "ForAllValues:StringNotLike" = { "aws:PrincipalOrgPaths" = "${local.prod_path}*" }
            BoolIfExists                 = { "aws:PrincipalIsAWSService" = "false" }
          }
        },
        {
          Sid       = "DenyAssumeRoleOutsideProdAndManagement"
          Effect    = "Deny"
          Principal = "*"
          Action    = "sts:AssumeRole"
          Resource  = "*"
          Condition = {
            "ForAllValues:StringNotLike" = { "aws:PrincipalOrgPaths" = "${local.prod_path}*" }
            StringNotEqualsIfExists      = { "aws:PrincipalAccount" = aws_organizations_organization.this.master_account_id }
            BoolIfExists                 = { "aws:PrincipalIsAWSService" = "false" }
          }
        },
      ]
    }
  }

  ec2_declarative_policy = {
    ec2_attributes = {
      serial_console_access        = { status = { "@@assign" = "disabled" } }
      image_block_public_access    = { state = { "@@assign" = "block_new_sharing" } }
      snapshot_block_public_access = { state = { "@@assign" = "block_all_sharing" } }
      instance_metadata_defaults = {
        http_tokens                 = { "@@assign" = "required" }
        http_put_response_hop_limit = { "@@assign" = "2" }
        http_endpoint               = { "@@assign" = "no_preference" }
        instance_metadata_tags      = { "@@assign" = "no_preference" }
        http_tokens_enforced        = { "@@assign" = "enabled" }
      }
    }
  }

  policies = merge(
    { for k, v in local.service_control_policies : k => {
      type        = "SERVICE_CONTROL_POLICY"
      description = v.description
      content     = jsonencode({ Version = "2012-10-17", Statement = v.statements })
    } },
    { for k, v in local.resource_control_policies : k => {
      type        = "RESOURCE_CONTROL_POLICY"
      description = v.description
      content     = jsonencode({ Version = "2012-10-17", Statement = v.statements })
    } },
    {
      ec2-defaults = {
        type        = "DECLARATIVE_POLICY_EC2"
        description = "EC2 の serial console、AMI と snapshot の公開、IMDSv2 を組織で固定する。"
        content     = jsonencode(local.ec2_declarative_policy)
      }
    },
  )

  # 付け先。新しい policy は PolicyStaging で試してから広げる（organization-baseline/ADR-0101）。
  attachments = merge(
    { "deny-leave-and-close/root" = { policy = "deny-leave-and-close", target_id = local.root_id } },
    { for k in [
      "deny-state-write",
      "protect-governance-roles",
      "deny-guardduty-stop",
      "deny-root-user",
      "deny-iam-user",
      "data-perimeter-prod-resources",
      "data-perimeter-org-principals",
      "data-perimeter-prod-principals",
      "ec2-defaults",
    ] : "${k}/policy-staging" => { policy = k, target_id = aws_organizations_organizational_unit.top["PolicyStaging"].id } },
  )
}

resource "aws_organizations_policy" "this" {
  for_each = local.policies

  name        = "${var.project}-${each.key}"
  description = each.value.description
  type        = each.value.type
  content     = each.value.content

  # RCP と declarative policy の種類が組織で有効になってから作る。
  depends_on = [aws_organizations_organization.this]
}

resource "aws_organizations_policy_attachment" "this" {
  for_each = local.attachments

  policy_id = aws_organizations_policy.this[each.value.policy].id
  target_id = each.value.target_id
}
