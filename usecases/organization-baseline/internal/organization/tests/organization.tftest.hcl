mock_provider "aws" {
  mock_resource "aws_organizations_organization" {
    defaults = {
      id                = "o-exampleorg1"
      master_account_id = "111111111111"
      roots             = [{ id = "r-exam", arn = "arn:aws:organizations::111111111111:root/o-exampleorg1/r-exam", name = "Root", policy_types = [] }]
    }
  }
}

variables {
  project               = "tabp"
  break_glass_role_name = "tabp-break-glass"
  break_glass_role_path = "/break-glass/"
}

override_resource {
  target = aws_organizations_organizational_unit.top["Workloads"]
  values = { id = "ou-exam-workload" }
}

override_resource {
  target = aws_organizations_organizational_unit.workloads["Prod"]
  values = { id = "ou-exam-prod" }
}

override_resource {
  target = aws_organizations_organizational_unit.top["PolicyStaging"]
  values = { id = "ou-exam-staging" }
}

run "organization" {
  command = plan

  assert {
    condition     = aws_organizations_organization.this.feature_set == "ALL"
    error_message = "組織は all features で作ってください。"
  }

  assert {
    condition     = toset(aws_organizations_organization.this.enabled_policy_types) == toset(["DECLARATIVE_POLICY_EC2", "RESOURCE_CONTROL_POLICY", "SERVICE_CONTROL_POLICY"])
    error_message = "有効にする policy の種類は SCP・RCP・EC2 の declarative policy の3つです。"
  }

  assert {
    condition     = aws_organizations_aws_service_access.iam.service_principal == "iam.amazonaws.com"
    error_message = "Terraform が持つ信頼アクセスは IAM だけです。"
  }

  assert {
    condition     = toset(aws_iam_organizations_features.this.enabled_features) == toset(["RootCredentialsManagement", "RootSessions"])
    error_message = "root の資格情報の中央管理と特権のセッションの両方を有効にしてください。"
  }
}

run "organizational_units" {
  command = apply

  assert {
    condition     = toset(keys(aws_organizations_organizational_unit.top)) == toset(["Security", "Workloads", "Sandbox", "PolicyStaging"])
    error_message = "Root の直下の OU は Security・Workloads・Sandbox・PolicyStaging です。"
  }

  assert {
    condition     = alltrue([for ou in aws_organizations_organizational_unit.top : ou.parent_id == "r-exam"])
    error_message = "Root の直下の OU の親は Root です。"
  }

  assert {
    condition     = toset(keys(aws_organizations_organizational_unit.workloads)) == toset(["Prod", "NonProd"])
    error_message = "Workloads の下の OU は Prod と NonProd です。"
  }

  assert {
    condition     = alltrue([for ou in aws_organizations_organizational_unit.workloads : ou.parent_id == "ou-exam-workload"])
    error_message = "Prod と NonProd の親は Workloads です。"
  }
}

run "policies" {
  command = apply

  assert {
    condition = { for k, p in aws_organizations_policy.this : k => p.type } == {
      deny-leave-and-close           = "SERVICE_CONTROL_POLICY"
      deny-bootstrap-role            = "SERVICE_CONTROL_POLICY"
      deny-state-write               = "SERVICE_CONTROL_POLICY"
      protect-governance-roles       = "SERVICE_CONTROL_POLICY"
      deny-guardduty-stop            = "SERVICE_CONTROL_POLICY"
      deny-root-user                 = "SERVICE_CONTROL_POLICY"
      deny-iam-user                  = "SERVICE_CONTROL_POLICY"
      deny-baseline-stop             = "SERVICE_CONTROL_POLICY"
      deny-enable-region             = "SERVICE_CONTROL_POLICY"
      data-perimeter-prod-resources  = "SERVICE_CONTROL_POLICY"
      data-perimeter-org-principals  = "RESOURCE_CONTROL_POLICY"
      data-perimeter-prod-principals = "RESOURCE_CONTROL_POLICY"
      ec2-defaults                   = "DECLARATIVE_POLICY_EC2"
    }
    error_message = "policy の一覧と種類が計画と違います。"
  }

  assert {
    condition     = alltrue([for k, p in aws_organizations_policy.this : p.name == "tabp-${k}"])
    error_message = "policy の名前は <project>-<policy> です。"
  }

  assert {
    condition = jsondecode(aws_organizations_policy.this["deny-leave-and-close"].content).Statement == [{
      Sid      = "DenyLeaveOrganizationAndCloseAccount"
      Effect   = "Deny"
      Action   = ["account:CloseAccount", "organizations:LeaveOrganization"]
      Resource = "*"
    }]
    error_message = "離脱と閉鎖の拒否の文書が違います。"
  }

  assert {
    condition = jsondecode(aws_organizations_policy.this["deny-root-user"].content).Statement[0].Condition == {
      ArnLike = { "aws:PrincipalArn" = "arn:aws:iam::*:root" }
      Null    = { "aws:AssumedRoot" = "true" }
    }
    error_message = "root の拒否は、sts:AssumeRoot の特権のセッション（aws:AssumedRoot が在る要求）を外してください。"
  }

  assert {
    condition = toset(jsondecode(aws_organizations_policy.this["deny-state-write"].content).Statement[0].Condition.ArnNotLike["aws:PrincipalArn"]) == toset(concat(
      flatten([for env in ["security", "log-archive", "sandbox", "dev", "stg", "prd"] : [
        for suffix in ["apply", "apply-identity", "unlock", "unlock-identity"] : "arn:aws:iam::*:role/tabp-${env}-ci-${suffix}"
      ]]),
      ["arn:aws:iam::*:role/break-glass/tabp-break-glass"],
    ))
    error_message = "state の書き込みの拒否から外すのは、メンバーの env の CI の apply 系と lock の解除の role と break-glass の role だけです。"
  }

  assert {
    condition = (
      jsondecode(aws_organizations_policy.this["protect-governance-roles"].content).Statement[0].Resource == "arn:aws:iam::*:role/break-glass/tabp-break-glass" &&
      jsondecode(aws_organizations_policy.this["protect-governance-roles"].content).Statement[0].Condition == { ArnNotLike = { "aws:PrincipalArn" = "arn:aws:iam::*:role/stacksets-exec-*" } }
    )
    error_message = "統制の role の保護の対象は break-glass の role で、外すのは StackSet の実行の role（stacksets-exec-*）だけです。"
  }

  assert {
    condition = jsondecode(aws_organizations_policy.this["protect-governance-roles"].content).Statement[1] == {
      Sid      = "DenyStackSetsExecutionRoleChange"
      Effect   = "Deny"
      Action   = ["iam:Attach*", "iam:Create*", "iam:Delete*", "iam:Detach*", "iam:PassRole", "iam:Put*", "iam:Tag*", "iam:Untag*", "iam:Update*"]
      Resource = "arn:aws:iam::*:role/stacksets-exec-*"
    }
    error_message = "例外にした StackSet の実行の role の名前を、メンバーの主体が作ったり変えたりすることを拒んでください。"
  }

  assert {
    condition     = output.root_id == "r-exam"
    error_message = "root_id は組織の Root の ID です。"
  }

  assert {
    condition     = jsondecode(aws_organizations_policy.this["data-perimeter-prod-resources"].content).Statement[0].Condition["ForAnyValue:StringLike"]["aws:ResourceOrgPaths"] == "o-exampleorg1/r-exam/ou-exam-workload/ou-exam-prod/*"
    error_message = "下位の環境の SCP は Prod の OU の path の resource を拒んでください。"
  }

  assert {
    condition     = jsondecode(aws_organizations_policy.this["data-perimeter-org-principals"].content).Statement[0].Condition.StringNotEqualsIfExists["aws:PrincipalOrgID"] == "o-exampleorg1"
    error_message = "組織の境界の RCP は組織の ID で限ってください。"
  }

  assert {
    condition     = jsondecode(aws_organizations_policy.this["data-perimeter-prod-principals"].content).Statement[1].Condition.StringNotEqualsIfExists["aws:PrincipalAccount"] == "111111111111"
    error_message = "Prod の RCP は、role の引き受けだけを管理アカウントの主体に開けてください。"
  }

  assert {
    condition = jsondecode(aws_organizations_policy.this["ec2-defaults"].content) == {
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
    error_message = "EC2 の declarative policy の値が計画と違います。"
  }

  assert {
    condition = jsondecode(aws_organizations_policy.this["deny-bootstrap-role"].content).Statement == [{
      Sid       = "DenyOrganizationAccountAccessRole"
      Effect    = "Deny"
      Action    = "*"
      Resource  = "*"
      Condition = { ArnLike = { "aws:PrincipalArn" = "arn:aws:iam::*:role/OrganizationAccountAccessRole" } }
    }]
    error_message = "立ち上げの role の拒否の文書が違います。"
  }

  assert {
    condition = jsondecode(aws_organizations_policy.this["deny-iam-user"].content).Statement[0].Action == [
      "iam:CreateAccessKey",
      "iam:CreateLoginProfile",
      "iam:CreateServiceSpecificCredential",
      "iam:CreateUser",
      "iam:UploadSSHPublicKey",
      "iam:UploadSigningCertificate",
    ]
    error_message = "IAM user と長期の資格情報の作成の拒否が、Action をすべて持っていません。"
  }

  assert {
    condition = jsondecode(aws_organizations_policy.this["deny-guardduty-stop"].content).Statement[0].Action == [
      "guardduty:DeleteDetector",
      "guardduty:DisassociateFromAdministratorAccount",
      "guardduty:DisassociateFromMasterAccount",
      "guardduty:UpdateDetector",
    ]
    error_message = "sandbox の GuardDuty の拒否の Action が違います。"
  }

  assert {
    condition = (
      jsondecode(aws_organizations_policy.this["deny-state-write"].content).Statement[0].Action == ["s3:DeleteObject*", "s3:PutObject*", "s3:Replicate*", "s3:RestoreObject"] &&
      jsondecode(aws_organizations_policy.this["deny-state-write"].content).Statement[0].Resource == "arn:aws:s3:::tabp-*-tfstate-*/*"
    )
    error_message = "state の書き込みの拒否の Action か Resource が違います。"
  }

  assert {
    condition     = jsondecode(aws_organizations_policy.this["protect-governance-roles"].content).Statement[0].Action == ["iam:Attach*", "iam:Create*", "iam:Delete*", "iam:Detach*", "iam:PassRole", "iam:Put*", "iam:Tag*", "iam:Untag*", "iam:Update*"]
    error_message = "break-glass の role の保護の Action が違います。"
  }

  assert {
    condition = jsondecode(aws_organizations_policy.this["data-perimeter-prod-principals"].content).Statement[0] == {
      Sid       = "DenyPrincipalsOutsideProd"
      Effect    = "Deny"
      Principal = "*"
      Action    = ["kms:*", "s3:*", "secretsmanager:*"]
      Resource  = "*"
      Condition = {
        "ForAllValues:StringNotLike" = { "aws:PrincipalOrgPaths" = "o-exampleorg1/r-exam/ou-exam-workload/ou-exam-prod/*" }
        BoolIfExists                 = { "aws:PrincipalIsAWSService" = "false" }
      }
    }
    error_message = "Prod の RCP の1つ目の文が違います。"
  }

  assert {
    condition     = jsondecode(aws_organizations_policy.this["data-perimeter-org-principals"].content).Statement[0].Action == ["kms:*", "s3:*", "secretsmanager:*", "sts:AssumeRole"]
    error_message = "組織の境界の RCP の Action が違います。"
  }

  assert {
    condition     = alltrue([for k, p in aws_organizations_policy.this : length(p.content) <= (p.type == "RESOURCE_CONTROL_POLICY" ? 5120 : 10240)])
    error_message = "policy の文書が Organizations の上限（SCP 10,240 文字、RCP 5,120 文字）を超えます。"
  }
}

run "attachments" {
  command = apply

  assert {
    condition     = aws_organizations_policy_attachment.this["deny-leave-and-close/root"].target_id == "r-exam"
    error_message = "Root に付けるのは離脱と閉鎖の拒否です。"
  }

  assert {
    condition     = [for k, a in aws_organizations_policy_attachment.this : k if a.target_id == "r-exam"] == ["deny-leave-and-close/root"]
    error_message = "Root に付けてよいのは離脱と閉鎖の拒否だけです。新しい policy は PolicyStaging で試してから広げます。"
  }

  assert {
    condition = toset([for k, a in aws_organizations_policy_attachment.this : split("/", k)[0] if a.target_id == "ou-exam-staging"]) == toset([
      "deny-state-write",
      "protect-governance-roles",
      "deny-guardduty-stop",
      "deny-root-user",
      "deny-iam-user",
      "deny-baseline-stop",
      "deny-enable-region",
      "data-perimeter-prod-resources",
      "data-perimeter-org-principals",
      "data-perimeter-prod-principals",
      "ec2-defaults",
    ])
    error_message = "PolicyStaging に付けるのは、試している policy のすべてです。"
  }

  assert {
    condition     = !contains([for k, a in aws_organizations_policy_attachment.this : split("/", k)[0]], "deny-bootstrap-role")
    error_message = "OrganizationAccountAccessRole の拒否は、アカウントを作る変更まで付けません。"
  }
}

run "baseline_scps" {
  command = plan

  assert {
    condition = jsondecode(aws_organizations_policy.this["deny-baseline-stop"].content).Statement[0].Action == [
      "access-analyzer:CreateArchiveRule",
      "access-analyzer:DeleteAnalyzer",
      "access-analyzer:UpdateArchiveRule",
      "config:DeleteConfigurationRecorder",
      "config:DeleteDeliveryChannel",
      "config:DeleteRetentionConfiguration",
      "config:PutConfigurationRecorder",
      "config:PutDeliveryChannel",
      "config:PutRetentionConfiguration",
      "config:StopConfigurationRecorder",
      "ec2:CreateVpcBlockPublicAccessExclusion",
      "ec2:DisableEbsEncryptionByDefault",
      "ec2:ModifyVpcBlockPublicAccessExclusion",
      "ec2:ModifyVpcBlockPublicAccessOptions",
      "elasticmapreduce:PutBlockPublicAccessConfiguration",
      "s3:PutAccountPublicAccessBlock",
      "ssm:ResetServiceSetting",
      "ssm:UpdateServiceSetting",
    ]
    error_message = "deny-baseline-stop が拒む Action が、account-baseline/ADR-0201 の一覧と違います。"
  }

  assert {
    condition = jsondecode(aws_organizations_policy.this["deny-baseline-stop"].content).Statement[0].Condition == {
      ArnNotLike = { "aws:PrincipalArn" = [for env in ["security", "log-archive", "sandbox", "dev", "stg", "prd"] : "arn:aws:iam::*:role/tabp-${env}-ci-apply"] }
    }
    error_message = "deny-baseline-stop の例外が、メンバーの CI の apply の role だけになっていません（break-glass は例外にしない。PLAN-ORG-05）。"
  }

  assert {
    condition     = jsondecode(aws_organizations_policy.this["deny-enable-region"].content).Statement == [{ Sid = "DenyEnableRegion", Effect = "Deny", Action = "account:EnableRegion", Resource = "*" }]
    error_message = "deny-enable-region が、例外なしに account:EnableRegion だけを拒む形になっていません。"
  }
}
