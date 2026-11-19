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
  github                  = { owner = "octo-org", owner_id = "123456", repository = "octo-repo", repository_id = "456789" }
  state_bucket_arn        = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an"
  state_key_arn           = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  plan_output_bucket_arn  = "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an"
  apply_output_bucket_arn = "arn:aws:s3:::tabp-admin-tfapply-123456789012-ap-northeast-1-an"
  break_glass_arns        = ["arn:aws:iam::123456789012:user/break-glass/break-glass-1", "arn:aws:iam::123456789012:user/break-glass/break-glass-2", "arn:aws:iam::123456789012:group/break-glass/break-glass", "arn:aws:iam::123456789012:role/break-glass/tabp-break-glass"]
  # policy_length の境界が選ぶ partition と region の候補。provider が partition を解決する一覧（aws-sdk-go-base の
  # endpoints）の写しである。module は宣言しない。
  aws_partitions = {
    "aws" = [
      "af-south-1", "ap-east-1", "ap-east-2", "ap-northeast-1", "ap-northeast-2", "ap-northeast-3", "ap-south-1",
      "ap-south-2", "ap-southeast-1", "ap-southeast-2", "ap-southeast-3", "ap-southeast-4", "ap-southeast-5",
      "ap-southeast-6", "ap-southeast-7", "ca-central-1", "ca-west-1", "eu-central-1", "eu-central-2", "eu-north-1",
      "eu-south-1", "eu-south-2", "eu-west-1", "eu-west-2", "eu-west-3", "il-central-1", "me-central-1", "me-south-1",
      "mx-central-1", "sa-east-1", "us-east-1", "us-east-2", "us-west-1", "us-west-2",
    ]
    "aws-cn"     = ["cn-north-1", "cn-northwest-1"]
    "aws-eusc"   = ["eusc-de-east-1"]
    "aws-iso"    = ["us-iso-east-1", "us-iso-west-1"]
    "aws-iso-b"  = ["us-isob-east-1", "us-isob-west-1"]
    "aws-iso-e"  = ["eu-isoe-west-1"]
    "aws-iso-f"  = ["us-isof-east-1", "us-isof-south-1"]
    "aws-us-gov" = ["us-gov-east-1", "us-gov-west-1"]
  }
}

# provider の ARN は plan では決まらないので、mock の apply で確かめる。
# role の ARN は名前から組むので、mock の role の ARN ではなく literal と比べる。
run "policies" {
  command = apply

  assert {
    condition = jsondecode(aws_iam_role_policy.this["plan"].policy).Statement == [
      {
        Sid    = "ListBuckets"
        Effect = "Allow"
        Action = "s3:ListBucket"
        Resource = [
          "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an",
          "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an",
          "arn:aws:s3:::tabp-admin-tfapply-123456789012-ap-northeast-1-an",
        ]
      },
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
        Resource = [
          "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an",
          "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an",
          "arn:aws:s3:::tabp-admin-tfapply-123456789012-ap-northeast-1-an",
        ]
      },
      {
        Sid    = "ReadState"
        Effect = "Allow"
        Action = "s3:GetObject"
        Resource = [
          "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/identity/terraform.tfstate",
          "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/workload/terraform.tfstate",
        ]
      },
      {
        Sid    = "WritePlanOutput"
        Effect = "Allow"
        Action = "s3:PutObject"
        Resource = [
          "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an/pr/*",
          "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an/branch/*",
        ]
      },
      {
        Sid      = "ReadStateKey"
        Effect   = "Allow"
        Action   = ["kms:DescribeKey", "kms:GetKeyPolicy", "kms:GetKeyRotationStatus", "kms:ListResourceTags"]
        Resource = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
      },
      {
        Sid      = "ReadGitHubProvider"
        Effect   = "Allow"
        Action   = ["iam:GetOpenIDConnectProvider", "iam:ListOpenIDConnectProviderTags"]
        Resource = aws_iam_openid_connect_provider.github.arn
      },
      {
        Sid    = "ReadCiRoles"
        Effect = "Allow"
        Action = [
          "iam:GetRole",
          "iam:GetRolePolicy",
          "iam:ListAttachedRolePolicies",
          "iam:ListRolePolicies",
          "iam:ListRoleTags",
        ]
        Resource = [
          "arn:aws:iam::123456789012:role/tabp-admin-ci-apply",
          "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity",
          "arn:aws:iam::123456789012:role/tabp-admin-ci-plan",
          "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock",
          "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity",
        ]
      },
      { Sid = "ListKeyAliases", Effect = "Allow", Action = "kms:ListAliases", Resource = "*" },
      { Sid = "IamGetAccountPasswordPolicy", Effect = "Allow", Action = "iam:GetAccountPasswordPolicy", Resource = "*" },
      { Sid = "IamListOrganizationsFeatures", Effect = "Allow", Action = "iam:ListOrganizationsFeatures", Resource = "*" },
      { Sid = "OrganizationsDescribeOrganization", Effect = "Allow", Action = "organizations:DescribeOrganization", Resource = "*" },
      {
        Sid      = "OrganizationsListAWSServiceAccessForOrganization"
        Effect   = "Allow"
        Action   = "organizations:ListAWSServiceAccessForOrganization"
        Resource = "*"
      },
      { Sid = "OrganizationsListAccounts", Effect = "Allow", Action = "organizations:ListAccounts", Resource = "*" },
      { Sid = "OrganizationsListRoots", Effect = "Allow", Action = "organizations:ListRoots", Resource = "*" },
      {
        Sid    = "ReadOrganizationalUnits"
        Effect = "Allow"
        Action = [
          "organizations:DescribeOrganizationalUnit",
          "organizations:ListAccountsForParent",
          "organizations:ListParents",
          "organizations:ListTagsForResource",
        ]
        Resource = ["arn:aws:organizations::123456789012:ou/o-*/ou-*"]
      },
      {
        Sid    = "ReadOrganizationPolicies"
        Effect = "Allow"
        Action = [
          "organizations:DescribePolicy",
          "organizations:ListTagsForResource",
          "organizations:ListTargetsForPolicy",
        ]
        Resource = "arn:aws:organizations::123456789012:policy/o-*/*/p-*"
      },
      {
        Sid    = "ReadBreakGlassUsers"
        Effect = "Allow"
        Action = ["iam:GetUser", "iam:ListUserTags"]
        Resource = [
          "arn:aws:iam::123456789012:user/break-glass/break-glass-1",
          "arn:aws:iam::123456789012:user/break-glass/break-glass-2",
        ]
      },
      {
        Sid      = "ReadBreakGlassGroup"
        Effect   = "Allow"
        Action   = ["iam:GetGroup", "iam:GetGroupPolicy"]
        Resource = ["arn:aws:iam::123456789012:group/break-glass/break-glass"]
      },
      {
        Sid    = "ReadBreakGlassRole"
        Effect = "Allow"
        Action = [
          "iam:GetRole",
          "iam:GetRolePolicy",
          "iam:ListAttachedRolePolicies",
          "iam:ListRolePolicies",
          "iam:ListRoleTags",
        ]
        Resource = ["arn:aws:iam::123456789012:role/break-glass/tabp-break-glass"]
      },
      {
        Sid      = "ReadGovernancePolicy"
        Effect   = "Allow"
        Action   = ["iam:GetPolicy", "iam:GetPolicyVersion", "iam:ListPolicyTags", "iam:ListPolicyVersions"]
        Resource = "arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-identity-governance"
      },
    ]
    error_message = "plan の role の権限が、state と2つの Bucket の設定と identity root の resource と state の鍵の読み取りと、plan の出力の書き込みだけになっていません。"
  }

  assert {
    condition = jsondecode(aws_iam_role_policy.this["apply"].policy).Statement == [
      {
        Sid      = "ListStateBucket"
        Effect   = "Allow"
        Action   = "s3:ListBucket"
        Resource = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an"
      },
      {
        Sid      = "ReadStateBucketEncryption"
        Effect   = "Allow"
        Action   = "s3:GetEncryptionConfiguration"
        Resource = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an"
      },
      {
        Sid      = "WriteWorkloadState"
        Effect   = "Allow"
        Action   = ["s3:GetObject", "s3:PutObject"]
        Resource = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/workload/terraform.tfstate"
      },
      {
        Sid      = "LockWorkloadState"
        Effect   = "Allow"
        Action   = ["s3:DeleteObject", "s3:GetObject", "s3:PutObject"]
        Resource = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/workload/terraform.tfstate.tflock"
      },
      { Sid = "ListKeyAliases", Effect = "Allow", Action = "kms:ListAliases", Resource = "*" },
      {
        Sid      = "WriteWorkloadApplyOutput"
        Effect   = "Allow"
        Action   = "s3:PutObject"
        Resource = "arn:aws:s3:::tabp-admin-tfapply-123456789012-ap-northeast-1-an/*"
      },
      {
        Sid    = "DenyIdentityChange"
        Effect = "Deny"
        Action = "*"
        Resource = [
          "arn:aws:iam::123456789012:role/tabp-admin-ci-apply",
          "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity",
          "arn:aws:iam::123456789012:role/tabp-admin-ci-plan",
          "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock",
          "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity",
          aws_iam_openid_connect_provider.github.arn,
          "arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-identity-governance",
        ]
      },
      {
        Sid    = "DenyIdentityStateChange"
        Effect = "Deny"
        Action = "*"
        Resource = [
          "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/identity/terraform.tfstate",
          "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/identity/terraform.tfstate.tflock",
        ]
      },
      {
        Sid       = "DenyStateBucketChange"
        Effect    = "Deny"
        NotAction = ["s3:GetEncryptionConfiguration", "s3:ListBucket"]
        Resource  = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an"
      },
      {
        Sid      = "DenyStateVersionDeletion"
        Effect   = "Deny"
        Action   = "s3:DeleteObjectVersion"
        Resource = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/*"
      },
      {
        Sid    = "DenyPlanOutputChange"
        Effect = "Deny"
        Action = "*"
        Resource = [
          "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an",
          "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an/*",
        ]
      },
      {
        Sid      = "DenyApplyOutputBucketChange"
        Effect   = "Deny"
        Action   = "*"
        Resource = "arn:aws:s3:::tabp-admin-tfapply-123456789012-ap-northeast-1-an"
      },
      {
        Sid       = "DenyApplyOutputChange"
        Effect    = "Deny"
        NotAction = ["s3:PutObject"]
        Resource  = "arn:aws:s3:::tabp-admin-tfapply-123456789012-ap-northeast-1-an/*"
      },
      {
        Sid    = "DenyStateKeyChange"
        Effect = "Deny"
        Action = [
          "kms:CancelKeyDeletion",
          "kms:CreateAlias",
          "kms:DeleteAlias",
          "kms:DescribeKey",
          "kms:DisableKey",
          "kms:DisableKeyRotation",
          "kms:EnableKey",
          "kms:EnableKeyRotation",
          "kms:GetKeyPolicy",
          "kms:GetKeyRotationStatus",
          "kms:ListGrants",
          "kms:ListKeyPolicies",
          "kms:ListResourceTags",
          "kms:PutKeyPolicy",
          "kms:RevokeGrant",
          "kms:RotateKeyOnDemand",
          "kms:ScheduleKeyDeletion",
          "kms:TagResource",
          "kms:UntagResource",
          "kms:UpdateAlias",
          "kms:UpdateKeyDescription",
        ]
        Resource = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
      },
      {
        Sid    = "DenyGovernanceChange"
        Effect = "Deny"
        Action = [
          "organizations:Accept*",
          "organizations:Attach*",
          "organizations:Cancel*",
          "organizations:Close*",
          "organizations:Create*",
          "organizations:Decline*",
          "organizations:Delete*",
          "organizations:Deregister*",
          "organizations:Detach*",
          "organizations:Disable*",
          "organizations:Enable*",
          "organizations:Invite*",
          "organizations:Leave*",
          "organizations:Move*",
          "organizations:Put*",
          "organizations:Register*",
          "organizations:Remove*",
          "organizations:Tag*",
          "organizations:Terminate*",
          "organizations:Untag*",
          "organizations:Update*",
          "account:CloseAccount",
          "iam:DeleteAccountPasswordPolicy",
          "iam:DisableOrganizationsRoot*",
          "iam:EnableOrganizationsRoot*",
          "iam:UpdateAccountPasswordPolicy",
          "sso:AttachCustomerManagedPolicyReferenceToPermissionSet",
          "sso:AttachManagedPolicyToPermissionSet",
          "sso:CreateAccountAssignment",
          "sso:CreatePermissionSet",
          "sso:DeleteAccountAssignment",
          "sso:DeleteInlinePolicyFromPermissionSet",
          "sso:DeletePermissionSet",
          "sso:DeletePermissionsBoundaryFromPermissionSet",
          "sso:DetachCustomerManagedPolicyReferenceFromPermissionSet",
          "sso:DetachManagedPolicyFromPermissionSet",
          "sso:ProvisionPermissionSet",
          "sso:PutInlinePolicyToPermissionSet",
          "sso:PutPermissionsBoundaryToPermissionSet",
          "sso:UpdatePermissionSet",
          "sts:AssumeRoot",
        ]
        Resource = "*"
      },
      {
        Sid    = "DenyBreakGlassChange"
        Effect = "Deny"
        Action = "*"
        Resource = [
          "arn:aws:iam::123456789012:user/break-glass/break-glass-1",
          "arn:aws:iam::123456789012:user/break-glass/break-glass-2",
          "arn:aws:iam::123456789012:group/break-glass/break-glass",
          "arn:aws:iam::123456789012:role/break-glass/tabp-break-glass",
        ]
      },
    ]
    error_message = "apply の role の権限が、init の前の暗号化の設定と alias の一覧と、workload の state と lock の読み書きと、apply の出力の書き込みと、bootstrap の構成物・identity の state・3つの Bucket・plan の出力・apply の出力の書き込みのほか・state の鍵の変更の拒否になっていません。"
  }

  assert {
    condition = jsondecode(aws_iam_role_policy.apply_identity.policy).Statement == [
      {
        Sid    = "ListBuckets"
        Effect = "Allow"
        Action = "s3:ListBucket"
        Resource = [
          "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an",
          "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an",
          "arn:aws:s3:::tabp-admin-tfapply-123456789012-ap-northeast-1-an",
        ]
      },
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
        Resource = [
          "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an",
          "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an",
          "arn:aws:s3:::tabp-admin-tfapply-123456789012-ap-northeast-1-an",
        ]
      },
      {
        Sid      = "WriteIdentityState"
        Effect   = "Allow"
        Action   = ["s3:GetObject", "s3:PutObject"]
        Resource = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/identity/terraform.tfstate"
      },
      {
        Sid      = "LockIdentityState"
        Effect   = "Allow"
        Action   = ["s3:DeleteObject", "s3:GetObject", "s3:PutObject"]
        Resource = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/identity/terraform.tfstate.tflock"
      },
      {
        Sid    = "ConfigureBuckets"
        Effect = "Allow"
        Action = [
          "s3:DeleteBucketPolicy",
          "s3:PutBucketOwnershipControls",
          "s3:PutBucketPolicy",
          "s3:PutBucketPublicAccessBlock",
          "s3:PutBucketVersioning",
          "s3:PutEncryptionConfiguration",
          "s3:PutLifecycleConfiguration",
        ]
        Resource = [
          "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an",
          "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an",
          "arn:aws:s3:::tabp-admin-tfapply-123456789012-ap-northeast-1-an",
        ]
      },
      {
        Sid      = "CreateApplyOutputBucket"
        Effect   = "Allow"
        Action   = "s3:CreateBucket"
        Resource = "arn:aws:s3:::tabp-admin-tfapply-123456789012-ap-northeast-1-an"
      },
      {
        Sid    = "ManageGitHubProvider"
        Effect = "Allow"
        Action = [
          "iam:AddClientIDToOpenIDConnectProvider",
          "iam:CreateOpenIDConnectProvider",
          "iam:DeleteOpenIDConnectProvider",
          "iam:RemoveClientIDFromOpenIDConnectProvider",
          "iam:UpdateOpenIDConnectProviderThumbprint",
        ]
        Resource = aws_iam_openid_connect_provider.github.arn
      },
      {
        Sid    = "ManageCiRoles"
        Effect = "Allow"
        Action = [
          "iam:CreateRole",
          "iam:DeleteRole",
          "iam:DeleteRolePolicy",
          "iam:DetachRolePolicy",
          "iam:ListInstanceProfilesForRole",
          "iam:PutRolePolicy",
          "iam:UpdateAssumeRolePolicy",
          "iam:UpdateRole",
          "iam:UpdateRoleDescription",
        ]
        Resource = [
          "arn:aws:iam::123456789012:role/tabp-admin-ci-apply",
          "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity",
          "arn:aws:iam::123456789012:role/tabp-admin-ci-plan",
          "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock",
          "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity",
        ]
      },
      {
        Sid      = "ReadStateKey"
        Effect   = "Allow"
        Action   = ["kms:DescribeKey", "kms:GetKeyPolicy", "kms:GetKeyRotationStatus", "kms:ListResourceTags"]
        Resource = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
      },
      {
        Sid      = "ManageStateKey"
        Effect   = "Allow"
        Action   = ["kms:EnableKeyRotation", "kms:PutKeyPolicy"]
        Resource = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
      },
      {
        Sid       = "DenyLockoutSafetyBypass"
        Effect    = "Deny"
        Action    = "kms:PutKeyPolicy"
        Resource  = "*"
        Condition = { Bool = { "kms:BypassPolicyLockoutSafetyCheck" = "true" } }
      },
      {
        Sid      = "ReadGitHubProvider"
        Effect   = "Allow"
        Action   = ["iam:GetOpenIDConnectProvider", "iam:ListOpenIDConnectProviderTags"]
        Resource = aws_iam_openid_connect_provider.github.arn
      },
      {
        Sid    = "ReadCiRoles"
        Effect = "Allow"
        Action = [
          "iam:GetRole",
          "iam:GetRolePolicy",
          "iam:ListAttachedRolePolicies",
          "iam:ListRolePolicies",
          "iam:ListRoleTags",
        ]
        Resource = [
          "arn:aws:iam::123456789012:role/tabp-admin-ci-apply",
          "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity",
          "arn:aws:iam::123456789012:role/tabp-admin-ci-plan",
          "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock",
          "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity",
        ]
      },
      { Sid = "ListKeyAliases", Effect = "Allow", Action = "kms:ListAliases", Resource = "*" },
      {
        Sid      = "WriteApplyOutput"
        Effect   = "Allow"
        Action   = "s3:PutObject"
        Resource = "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an/apply/*"
      },
      { Sid = "IamGetAccountPasswordPolicy", Effect = "Allow", Action = "iam:GetAccountPasswordPolicy", Resource = "*" },
      { Sid = "IamListOrganizationsFeatures", Effect = "Allow", Action = "iam:ListOrganizationsFeatures", Resource = "*" },
      { Sid = "OrganizationsDescribeOrganization", Effect = "Allow", Action = "organizations:DescribeOrganization", Resource = "*" },
      {
        Sid      = "OrganizationsListAWSServiceAccessForOrganization"
        Effect   = "Allow"
        Action   = "organizations:ListAWSServiceAccessForOrganization"
        Resource = "*"
      },
      { Sid = "OrganizationsListAccounts", Effect = "Allow", Action = "organizations:ListAccounts", Resource = "*" },
      { Sid = "OrganizationsListRoots", Effect = "Allow", Action = "organizations:ListRoots", Resource = "*" },
      {
        Sid    = "ReadOrganizationalUnits"
        Effect = "Allow"
        Action = [
          "organizations:DescribeOrganizationalUnit",
          "organizations:ListAccountsForParent",
          "organizations:ListParents",
          "organizations:ListTagsForResource",
        ]
        Resource = ["arn:aws:organizations::123456789012:ou/o-*/ou-*"]
      },
      {
        Sid    = "ReadOrganizationPolicies"
        Effect = "Allow"
        Action = [
          "organizations:DescribePolicy",
          "organizations:ListTagsForResource",
          "organizations:ListTargetsForPolicy",
        ]
        Resource = "arn:aws:organizations::123456789012:policy/o-*/*/p-*"
      },
      {
        Sid    = "ReadBreakGlassUsers"
        Effect = "Allow"
        Action = ["iam:GetUser", "iam:ListUserTags"]
        Resource = [
          "arn:aws:iam::123456789012:user/break-glass/break-glass-1",
          "arn:aws:iam::123456789012:user/break-glass/break-glass-2",
        ]
      },
      {
        Sid      = "ReadBreakGlassGroup"
        Effect   = "Allow"
        Action   = ["iam:GetGroup", "iam:GetGroupPolicy"]
        Resource = ["arn:aws:iam::123456789012:group/break-glass/break-glass"]
      },
      {
        Sid    = "ReadBreakGlassRole"
        Effect = "Allow"
        Action = [
          "iam:GetRole",
          "iam:GetRolePolicy",
          "iam:ListAttachedRolePolicies",
          "iam:ListRolePolicies",
          "iam:ListRoleTags",
        ]
        Resource = ["arn:aws:iam::123456789012:role/break-glass/tabp-break-glass"]
      },
      {
        Sid      = "ReadGovernancePolicy"
        Effect   = "Allow"
        Action   = ["iam:GetPolicy", "iam:GetPolicyVersion", "iam:ListPolicyTags", "iam:ListPolicyVersions"]
        Resource = "arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-identity-governance"
      },
      {
        Sid    = "ManageGovernancePolicy"
        Effect = "Allow"
        Action = [
          "iam:CreatePolicy",
          "iam:CreatePolicyVersion",
          "iam:DeletePolicy",
          "iam:DeletePolicyVersion",
          "iam:SetDefaultPolicyVersion",
        ]
        Resource = "arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-identity-governance"
      },
      {
        Sid      = "AttachGovernancePolicy"
        Effect   = "Allow"
        Action   = "iam:AttachRolePolicy"
        Resource = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
        Condition = {
          ArnEquals = { "iam:PolicyARN" = "arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-identity-governance" }
        }
      },
    ]
    error_message = "apply-identity の role の権限が、identity の state と lock の読み書きと、bootstrap の構成物と state の鍵の管理と、apply の出力の書き込みになっていません。"
  }

  assert {
    condition = jsondecode(aws_iam_role_policy.this["unlock_identity"].policy).Statement == [
      {
        Sid       = "ListIdentityLock"
        Effect    = "Allow"
        Action    = "s3:ListBucket"
        Resource  = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an"
        Condition = { StringEquals = { "s3:prefix" = "identity/terraform.tfstate.tflock" } }
      },
      { Sid = "RemoveIdentityLock", Effect = "Allow", Action = ["s3:DeleteObject", "s3:GetObject"], Resource = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/identity/terraform.tfstate.tflock" },
    ]
    error_message = "unlock-identity の role の権限が、identity root の lock file の一覧・読み取り・削除だけになっていません。"
  }

  assert {
    condition = jsondecode(aws_iam_role_policy.this["unlock"].policy).Statement == [
      {
        Sid       = "ListWorkloadLock"
        Effect    = "Allow"
        Action    = "s3:ListBucket"
        Resource  = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an"
        Condition = { StringEquals = { "s3:prefix" = "workload/terraform.tfstate.tflock" } }
      },
      { Sid = "RemoveWorkloadLock", Effect = "Allow", Action = ["s3:DeleteObject", "s3:GetObject"], Resource = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/workload/terraform.tfstate.tflock" },
    ]
    error_message = "unlock の role の権限が、workload root の lock file の一覧・読み取り・削除だけになっていません。"
  }

  assert {
    condition = [
      [aws_iam_role_policy.this["plan"].name, aws_iam_role_policy.this["plan"].role],
      [aws_iam_role_policy.this["apply"].name, aws_iam_role_policy.this["apply"].role],
      [aws_iam_role_policy.apply_identity.name, aws_iam_role_policy.apply_identity.role],
      [aws_iam_role_policy.this["unlock"].name, aws_iam_role_policy.this["unlock"].role],
      [aws_iam_role_policy.this["unlock_identity"].name, aws_iam_role_policy.this["unlock_identity"].role],
      ] == [
      ["plan", aws_iam_role.this["plan"].name],
      ["apply", aws_iam_role.this["apply"].name],
      ["apply_identity", aws_iam_role.apply_identity.name],
      ["unlock", aws_iam_role.this["unlock"].name],
      ["unlock_identity", aws_iam_role.this["unlock_identity"].name],
    ]
    error_message = "inline policy が、役割の名前で、その役割の role に付いていません。"
  }

  assert {
    condition = [
      [aws_iam_role_policies_exclusive.this["plan"].role_name, aws_iam_role_policies_exclusive.this["plan"].policy_names],
      [aws_iam_role_policies_exclusive.this["apply"].role_name, aws_iam_role_policies_exclusive.this["apply"].policy_names],
      [aws_iam_role_policies_exclusive.this["apply_identity"].role_name, aws_iam_role_policies_exclusive.this["apply_identity"].policy_names],
      [aws_iam_role_policies_exclusive.this["unlock"].role_name, aws_iam_role_policies_exclusive.this["unlock"].policy_names],
      [aws_iam_role_policies_exclusive.this["unlock_identity"].role_name, aws_iam_role_policies_exclusive.this["unlock_identity"].policy_names],
      ] == [
      [aws_iam_role.this["plan"].name, toset(["plan"])],
      [aws_iam_role.this["apply"].name, toset(["apply"])],
      [aws_iam_role.apply_identity.name, toset(["apply_identity"])],
      [aws_iam_role.this["unlock"].name, toset(["unlock"])],
      [aws_iam_role.this["unlock_identity"].name, toset(["unlock_identity"])],
    ]
    error_message = "role の inline policy が、その役割の policy の1つだけに限られていません。"
  }

  assert {
    condition = [
      [aws_iam_role_policy_attachments_exclusive.this["plan"].role_name, length(aws_iam_role_policy_attachments_exclusive.this["plan"].policy_arns)],
      [aws_iam_role_policy_attachments_exclusive.this["apply"].role_name, length(aws_iam_role_policy_attachments_exclusive.this["apply"].policy_arns)],
      [aws_iam_role_policy_attachments_exclusive.this["apply_identity"].role_name, length(aws_iam_role_policy_attachments_exclusive.this["apply_identity"].policy_arns)],
      [aws_iam_role_policy_attachments_exclusive.this["unlock"].role_name, length(aws_iam_role_policy_attachments_exclusive.this["unlock"].policy_arns)],
      [aws_iam_role_policy_attachments_exclusive.this["unlock_identity"].role_name, length(aws_iam_role_policy_attachments_exclusive.this["unlock_identity"].policy_arns)],
      ] == [
      [aws_iam_role.this["plan"].name, 0],
      [aws_iam_role.this["apply"].name, 0],
      [aws_iam_role.apply_identity.name, 0],
      [aws_iam_role.this["unlock"].name, 0],
      [aws_iam_role.this["unlock_identity"].name, 0],
    ]
    error_message = "role に managed policy を付けない設定になっていません。"
  }
}

# 既定の値と違う鍵を、data source を合わせて与え、入力を読んだかを区別する。
run "key_arn_follows_input" {
  command = apply

  variables {
    state_key_arn = "arn:aws-us-gov:kms:us-gov-west-1:210987654321:key/abcdef01-2345-6789-abcd-ef0123456789"
    break_glass_arns = [
      "arn:aws-us-gov:iam::210987654321:user/break-glass/break-glass-1",
      "arn:aws-us-gov:iam::210987654321:user/break-glass/break-glass-2",
      "arn:aws-us-gov:iam::210987654321:group/break-glass/break-glass",
      "arn:aws-us-gov:iam::210987654321:role/break-glass/tabp-break-glass",
    ]
  }

  override_data {
    target = data.aws_partition.current
    values = { partition = "aws-us-gov" }
  }
  override_data {
    target = data.aws_region.current
    values = { region = "us-gov-west-1" }
  }
  override_data {
    target = data.aws_caller_identity.current
    values = { account_id = "210987654321" }
  }

  assert {
    condition = [
      one([for s in jsondecode(aws_iam_role_policy.this["plan"].policy).Statement : s.Resource if s.Sid == "ReadStateKey"]),
      one([for s in jsondecode(aws_iam_role_policy.this["apply"].policy).Statement : s.Resource if s.Sid == "DenyStateKeyChange"]),
      one([for s in jsondecode(aws_iam_role_policy.apply_identity.policy).Statement : s.Resource if s.Sid == "ReadStateKey"]),
      one([for s in jsondecode(aws_iam_role_policy.apply_identity.policy).Statement : s.Resource if s.Sid == "ManageStateKey"]),
      ] == [
      "arn:aws-us-gov:kms:us-gov-west-1:210987654321:key/abcdef01-2345-6789-abcd-ef0123456789",
      "arn:aws-us-gov:kms:us-gov-west-1:210987654321:key/abcdef01-2345-6789-abcd-ef0123456789",
      "arn:aws-us-gov:kms:us-gov-west-1:210987654321:key/abcdef01-2345-6789-abcd-ef0123456789",
      "arn:aws-us-gov:kms:us-gov-west-1:210987654321:key/abcdef01-2345-6789-abcd-ef0123456789",
    ]
    error_message = "plan の読み取り、apply の Deny、apply-identity の読み取りと管理の Resource が、入力の鍵の ARN になっていません。"
  }
}

run "key_in_other_account" {
  command = plan

  variables {
    state_key_arn = "arn:aws:kms:ap-northeast-1:210987654321:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  }

  expect_failures = [aws_iam_role_policy.apply_identity]
}

run "key_in_other_region" {
  command = plan

  variables {
    state_key_arn = "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  }

  expect_failures = [aws_iam_role_policy.apply_identity]
}

# 同じアカウントの alias の ARN は鍵ではない。root の validation の内側でも、precondition が鍵の形を見る。
run "alias_arn_as_state_key" {
  command = plan

  variables {
    state_key_arn = "arn:aws:kms:ap-northeast-1:123456789012:alias/tabp-admin-tfstate"
  }

  expect_failures = [aws_iam_role_policy.apply_identity]
}

run "key_in_other_partition" {
  command = plan

  variables {
    state_key_arn = "arn:aws-cn:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  }

  expect_failures = [aws_iam_role_policy.apply_identity]
}

# inline policy は role あたり 10,240 文字（空白を除く）が上限である（deployment-identity/ADR-0201「影響」）。
# mock の ARN は実物より短いので、入力が許す最も長い値（33文字の project と log-archive、63文字の Bucket、
# 最長の組成の break-glass）と、それに対応する長さの ARN を与えて数える。role の ARN は名前から組むので、
# partition と name_prefix から長さが決まる。partition と region は、aws_partitions の中で最も長いものを組を問わずに
# 合わせる。この組は AWS に無い。policies.tf と main.tf は partition と region を ARN の文字列に埋め込むだけなので、
# 文書はどちらの長さにも伸びる。そうでない使い方を入れたら、この組が最長である前提は崩れる。
# admin は統制の文が入り、break-glass が user・group・role を1つ以上含む。名前は IAM の上限（user・role 64文字、
# group 128文字）なので、長い group を2つにする。admin 以外は統制の文が無く組成の制約も無いので、group を4つにする。
# 長さを決める入力が違うので、2つの run で測る。どちらも role 名が最も長くなる log-archive の name_prefix を使う。
run "policy_length" {
  command = apply
  # 前の run の state を引き継ぐと、作成済みの OIDC provider に override が効かず、mock の短い ARN で数えてしまう。
  state_key = "policy_length"

  variables {
    env                     = "admin"
    name_prefix             = "abcdefghijklmnopqrstuvwxyz0123456-log-archive"
    state_bucket_arn        = format("arn:aws-us-gov:s3:::%063d", 1)
    state_key_arn           = "arn:aws-us-gov:kms:us-isof-south-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
    plan_output_bucket_arn  = format("arn:aws-us-gov:s3:::%063d", 2)
    apply_output_bucket_arn = format("arn:aws-us-gov:s3:::%063d", 3)
    break_glass_arns = [
      format("arn:aws-us-gov:iam::123456789012:user/break-glass/%064d", 1),
      format("arn:aws-us-gov:iam::123456789012:group/break-glass/%0128d", 2),
      format("arn:aws-us-gov:iam::123456789012:group/break-glass/%0128d", 3),
      format("arn:aws-us-gov:iam::123456789012:role/break-glass/%064d", 4),
    ]
  }

  override_data {
    target = data.aws_partition.current
    values = { partition = "aws-us-gov" }
  }
  override_data {
    target = data.aws_region.current
    values = { region = "us-isof-south-1" }
  }
  override_resource {
    target = aws_iam_openid_connect_provider.github
    values = { arn = "arn:aws-us-gov:iam::123456789012:oidc-provider/token.actions.githubusercontent.com" }
  }

  assert {
    condition = (
      contains(keys(var.aws_partitions), data.aws_partition.current.partition) &&
      length(data.aws_partition.current.partition) == max([for p in keys(var.aws_partitions) : length(p)]...)
    )
    error_message = "境界の入力の partition が、aws_partitions の中で最も長いものになっていません。"
  }

  assert {
    condition = (
      contains(flatten(values(var.aws_partitions)), data.aws_region.current.region) &&
      length(data.aws_region.current.region) == max([for r in flatten(values(var.aws_partitions)) : length(r)]...)
    )
    error_message = "境界の入力の region が、aws_partitions の中で最も長いものになっていません。"
  }

  assert {
    condition = alltrue([
      for a in [var.state_bucket_arn, var.plan_output_bucket_arn, var.apply_output_bucket_arn, aws_iam_openid_connect_provider.github.arn] :
      startswith(a, "arn:${data.aws_partition.current.partition}:")
    ])
    error_message = "境界の入力の Bucket か OIDC provider の ARN が、境界の partition のものになっていません。"
  }

  assert {
    condition = alltrue([
      for a in [var.state_bucket_arn, var.plan_output_bucket_arn, var.apply_output_bucket_arn] :
      length(trimprefix(a, "arn:${data.aws_partition.current.partition}:s3:::")) == 63
    ])
    error_message = "境界の入力の長さが、Bucket 名の 63 文字になっていません。"
  }

  assert {
    condition = (
      length([for a in var.break_glass_arns : a if strcontains(a, ":group/")]) == 2 &&
      length([for a in var.break_glass_arns : a if strcontains(a, ":user/")]) == 1 &&
      length([for a in var.break_glass_arns : a if strcontains(a, ":role/")]) == 1 &&
      alltrue([for a in var.break_glass_arns : length(element(split("/", a), 2)) == (strcontains(a, ":group/") ? 128 : 64)])
    )
    error_message = "境界の入力の break-glass が、admin で最も長い組成（名前が上限の group 2つと user・role）になっていません。"
  }

  assert {
    condition     = length(aws_iam_role.this["unlock_identity"].name) == 64
    error_message = "境界の入力で、最も長い role 名が IAM の上限の 64 文字になっていません。"
  }

  assert {
    condition     = length(aws_iam_role_policy.this) == 4
    error_message = "apply-identity 以外の role の inline policy の数が4つではありません。測る policy を見直してください。"
  }

  assert {
    condition     = alltrue([for p in values(aws_iam_role_policy.this) : length(p.policy) <= 10240])
    error_message = "inline policy が 10,240 文字を超えている role があります: ${join(", ", [for k, p in aws_iam_role_policy.this : k if length(p.policy) > 10240])}"
  }

  assert {
    condition     = length(aws_iam_role_policy.apply_identity.policy) <= 10240
    error_message = "apply-identity の role の inline policy が 10,240 文字を超えています。"
  }
}

run "policy_length_outside_admin" {
  command = apply
  # state_key を分ける理由は policy_length と同じ。
  state_key = "policy_length_outside_admin"

  variables {
    env                     = "log-archive"
    name_prefix             = "abcdefghijklmnopqrstuvwxyz0123456-log-archive"
    state_bucket_arn        = format("arn:aws-us-gov:s3:::%063d", 1)
    state_key_arn           = "arn:aws-us-gov:kms:us-isof-south-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
    plan_output_bucket_arn  = format("arn:aws-us-gov:s3:::%063d", 2)
    apply_output_bucket_arn = format("arn:aws-us-gov:s3:::%063d", 3)
    break_glass_arns        = [for i in range(4) : format("arn:aws-us-gov:iam::123456789012:group/break-glass/%0128d", i)]
  }

  override_data {
    target = data.aws_partition.current
    values = { partition = "aws-us-gov" }
  }
  override_data {
    target = data.aws_region.current
    values = { region = "us-isof-south-1" }
  }
  override_resource {
    target = aws_iam_openid_connect_provider.github
    values = { arn = "arn:aws-us-gov:iam::123456789012:oidc-provider/token.actions.githubusercontent.com" }
  }

  assert {
    condition = (
      contains(keys(var.aws_partitions), data.aws_partition.current.partition) &&
      length(data.aws_partition.current.partition) == max([for p in keys(var.aws_partitions) : length(p)]...)
    )
    error_message = "境界の入力の partition が、aws_partitions の中で最も長いものになっていません。"
  }

  assert {
    condition = (
      contains(flatten(values(var.aws_partitions)), data.aws_region.current.region) &&
      length(data.aws_region.current.region) == max([for r in flatten(values(var.aws_partitions)) : length(r)]...)
    )
    error_message = "境界の入力の region が、aws_partitions の中で最も長いものになっていません。"
  }

  assert {
    condition = alltrue([
      for a in [var.state_bucket_arn, var.plan_output_bucket_arn, var.apply_output_bucket_arn, aws_iam_openid_connect_provider.github.arn] :
      startswith(a, "arn:${data.aws_partition.current.partition}:")
    ])
    error_message = "境界の入力の Bucket か OIDC provider の ARN が、境界の partition のものになっていません。"
  }

  assert {
    condition = alltrue([
      for a in [var.state_bucket_arn, var.plan_output_bucket_arn, var.apply_output_bucket_arn] :
      length(trimprefix(a, "arn:${data.aws_partition.current.partition}:s3:::")) == 63
    ])
    error_message = "境界の入力の長さが、Bucket 名の 63 文字になっていません。"
  }

  assert {
    condition = (
      length(var.break_glass_arns) == 4 &&
      alltrue([for a in var.break_glass_arns : strcontains(a, ":group/") && length(element(split("/", a), 2)) == 128])
    )
    error_message = "境界の入力の break-glass が、名前が上限の group 4つになっていません。"
  }

  assert {
    condition     = length(aws_iam_role.this["unlock_identity"].name) == 64
    error_message = "境界の入力で、最も長い role 名が IAM の上限の 64 文字になっていません。"
  }

  assert {
    condition     = length(aws_iam_role_policy.this) == 4
    error_message = "apply-identity 以外の role の inline policy の数が4つではありません。測る policy を見直してください。"
  }

  assert {
    condition     = alltrue([for p in values(aws_iam_role_policy.this) : length(p.policy) <= 10240])
    error_message = "inline policy が 10,240 文字を超えている role があります: ${join(", ", [for k, p in aws_iam_role_policy.this : k if length(p.policy) > 10240])}"
  }

  assert {
    condition     = length(aws_iam_role_policy.apply_identity.policy) <= 10240
    error_message = "apply-identity の role の inline policy が 10,240 文字を超えています。"
  }
}

# 既定と違う partition とアカウントを与え、入力を読んだかを区別する。
run "role_arns_follow_data_sources" {
  command = apply

  variables {
    name_prefix   = "t4bp-dev"
    state_key_arn = "arn:aws-us-gov:kms:us-gov-west-1:210987654321:key/abcdef01-2345-6789-abcd-ef0123456789"
    break_glass_arns = [
      "arn:aws-us-gov:iam::210987654321:user/break-glass/break-glass-1",
      "arn:aws-us-gov:iam::210987654321:user/break-glass/break-glass-2",
      "arn:aws-us-gov:iam::210987654321:group/break-glass/break-glass",
      "arn:aws-us-gov:iam::210987654321:role/break-glass/tabp-break-glass",
    ]
  }

  override_data {
    target = data.aws_partition.current
    values = { partition = "aws-us-gov" }
  }
  override_data {
    target = data.aws_region.current
    values = { region = "us-gov-west-1" }
  }
  override_data {
    target = data.aws_caller_identity.current
    values = { account_id = "210987654321" }
  }

  assert {
    condition = one([for s in jsondecode(aws_iam_role_policy.apply_identity.policy).Statement : s.Resource if s.Sid == "ManageCiRoles"]) == [
      "arn:aws-us-gov:iam::210987654321:role/t4bp-dev-ci-apply",
      "arn:aws-us-gov:iam::210987654321:role/t4bp-dev-ci-apply-identity",
      "arn:aws-us-gov:iam::210987654321:role/t4bp-dev-ci-plan",
      "arn:aws-us-gov:iam::210987654321:role/t4bp-dev-ci-unlock",
      "arn:aws-us-gov:iam::210987654321:role/t4bp-dev-ci-unlock-identity",
    ]
    error_message = "apply-identity の ManageCiRoles の Resource が、partition・アカウント・role 名から組んだ ARN になっていません。"
  }
}

run "plan_output_bucket_arn_follows_input" {
  command = apply

  variables {
    plan_output_bucket_arn = "arn:aws:s3:::other-tfplan-210987654321-us-west-2-an"
  }

  assert {
    condition = [for s in jsondecode(aws_iam_role_policy.apply_identity.policy).Statement : s.Resource if s.Sid == "WriteApplyOutput"] == [
      "arn:aws:s3:::other-tfplan-210987654321-us-west-2-an/apply/*",
    ]
    error_message = "apply-identity の role の apply の出力の書き込み先が、入力の plan の出力の Bucket の apply/* になっていません。"
  }

  assert {
    condition = [for s in jsondecode(aws_iam_role_policy.this["plan"].policy).Statement : s.Resource if s.Sid == "WritePlanOutput"] == [
      ["arn:aws:s3:::other-tfplan-210987654321-us-west-2-an/pr/*", "arn:aws:s3:::other-tfplan-210987654321-us-west-2-an/branch/*"],
    ]
    error_message = "plan の role の plan の出力の書き込み先が、入力の plan の出力の Bucket の pr/* と branch/* になっていません。"
  }
}

run "apply_output_bucket_arn_follows_input" {
  command = apply

  variables {
    apply_output_bucket_arn = "arn:aws:s3:::other-tfapply-210987654321-us-west-2-an"
  }

  assert {
    condition = [
      for s in jsondecode(aws_iam_role_policy.apply_identity.policy).Statement : s.Resource
      if contains(["CreateApplyOutputBucket", "ListBuckets", "ReadBucketConfiguration", "ConfigureBuckets"], s.Sid)
      ] == [
      [
        "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an",
        "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an",
        "arn:aws:s3:::other-tfapply-210987654321-us-west-2-an",
      ],
      [
        "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an",
        "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an",
        "arn:aws:s3:::other-tfapply-210987654321-us-west-2-an",
      ],
      [
        "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an",
        "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an",
        "arn:aws:s3:::other-tfapply-210987654321-us-west-2-an",
      ],
      "arn:aws:s3:::other-tfapply-210987654321-us-west-2-an",
    ]
    error_message = "apply-identity の role の apply の出力の Bucket の作成・読み取り・設定の Resource が、入力の ARN になっていません。"
  }

  assert {
    condition = [
      for s in jsondecode(aws_iam_role_policy.this["plan"].policy).Statement : s.Resource
      if contains(["ListBuckets", "ReadBucketConfiguration"], s.Sid)
      ] == [
      [
        "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an",
        "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an",
        "arn:aws:s3:::other-tfapply-210987654321-us-west-2-an",
      ],
      [
        "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an",
        "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an",
        "arn:aws:s3:::other-tfapply-210987654321-us-west-2-an",
      ],
    ]
    error_message = "plan の role の apply の出力の Bucket の読み取りの Resource が、入力の ARN になっていません。"
  }

  assert {
    condition = [
      for s in jsondecode(aws_iam_role_policy.this["apply"].policy).Statement : s.Resource
      if contains(["WriteWorkloadApplyOutput", "DenyApplyOutputBucketChange", "DenyApplyOutputChange"], s.Sid)
      ] == [
      "arn:aws:s3:::other-tfapply-210987654321-us-west-2-an/*",
      "arn:aws:s3:::other-tfapply-210987654321-us-west-2-an",
      "arn:aws:s3:::other-tfapply-210987654321-us-west-2-an/*",
    ]
    error_message = "apply の role の apply の出力の書き込みと Deny の Resource が、入力の ARN になっていません。"
  }
}

# 統制の構成物は管理アカウントの identity root だけが持つ（deployment-identity/ADR-0201 決定3）。apply の role の Deny は env を問わない。
run "no_governance_in_dev" {
  command = apply

  variables {
    name_prefix      = "tabp-dev"
    env              = "dev"
    break_glass_arns = ["arn:aws:iam::123456789012:role/break-glass/tabp-break-glass"]
  }

  assert {
    condition     = [for s in jsondecode(aws_iam_role_policy.this["plan"].policy).Statement : s.Sid] == ["ListBuckets", "ReadBucketConfiguration", "ReadState", "WritePlanOutput", "ReadStateKey", "ReadGitHubProvider", "ReadCiRoles", "ListKeyAliases"]
    error_message = "dev の plan の role に、統制の構成物の読み取りが入っています。"
  }

  assert {
    condition = [for s in jsondecode(aws_iam_role_policy.apply_identity.policy).Statement : s.Sid] == [
      "ListBuckets",
      "ReadBucketConfiguration",
      "WriteIdentityState",
      "LockIdentityState",
      "ConfigureBuckets",
      "CreateApplyOutputBucket",
      "ManageGitHubProvider",
      "ManageCiRoles",
      "ReadStateKey",
      "ManageStateKey",
      "DenyLockoutSafetyBypass",
      "ReadGitHubProvider",
      "ReadCiRoles",
      "ListKeyAliases",
      "WriteApplyOutput",
    ]
    error_message = "dev の apply-identity の role に、統制の構成物の権限が入っています。"
  }

  assert {
    condition = [for s in jsondecode(aws_iam_role_policy.this["apply"].policy).Statement : s.Sid] == [
      "ListStateBucket",
      "ReadStateBucketEncryption",
      "WriteWorkloadState",
      "LockWorkloadState",
      "ListKeyAliases",
      "WriteWorkloadApplyOutput",
      "DenyIdentityChange",
      "DenyIdentityStateChange",
      "DenyStateBucketChange",
      "DenyStateVersionDeletion",
      "DenyPlanOutputChange",
      "DenyApplyOutputBucketChange",
      "DenyApplyOutputChange",
      "DenyStateKeyChange",
      "DenyGovernanceChange",
      "DenyBreakGlassChange",
    ]
    error_message = "dev の apply の role の文が、統制の構成物と break-glass の変更の Deny を含む一覧になっていません。"
  }

  assert {
    condition     = !strcontains(aws_iam_role_policy.this["apply"].policy, ":policy/")
    error_message = "dev の apply の role の Deny が、統制の customer managed policy を名指しています。"
  }

  assert {
    condition     = one([for s in jsondecode(aws_iam_role_policy.this["apply"].policy).Statement : s.Resource if s.Sid == "DenyBreakGlassChange"]) == ["arn:aws:iam::123456789012:role/break-glass/tabp-break-glass"]
    error_message = "dev の apply の role の break-glass の Deny が、入力の role の ARN になっていません。"
  }
}

run "no_governance_in_security" {
  command = apply

  variables {
    name_prefix      = "tabp-security"
    env              = "security"
    break_glass_arns = ["arn:aws:iam::123456789012:role/break-glass/tabp-break-glass"]
  }

  assert {
    condition     = [for s in jsondecode(aws_iam_role_policy.this["plan"].policy).Statement : s.Sid] == ["ListBuckets", "ReadBucketConfiguration", "ReadState", "WritePlanOutput", "ReadStateKey", "ReadGitHubProvider", "ReadCiRoles", "ListKeyAliases"]
    error_message = "security の plan の role に、統制の構成物の読み取りが入っています。"
  }

  assert {
    condition = [for s in jsondecode(aws_iam_role_policy.apply_identity.policy).Statement : s.Sid] == [
      "ListBuckets",
      "ReadBucketConfiguration",
      "WriteIdentityState",
      "LockIdentityState",
      "ConfigureBuckets",
      "CreateApplyOutputBucket",
      "ManageGitHubProvider",
      "ManageCiRoles",
      "ReadStateKey",
      "ManageStateKey",
      "DenyLockoutSafetyBypass",
      "ReadGitHubProvider",
      "ReadCiRoles",
      "ListKeyAliases",
      "WriteApplyOutput",
    ]
    error_message = "security の apply-identity の role に、統制の構成物の権限が入っています。"
  }

  assert {
    condition = [for s in jsondecode(aws_iam_role_policy.this["apply"].policy).Statement : s.Sid] == [
      "ListStateBucket",
      "ReadStateBucketEncryption",
      "WriteWorkloadState",
      "LockWorkloadState",
      "ListKeyAliases",
      "WriteWorkloadApplyOutput",
      "DenyIdentityChange",
      "DenyIdentityStateChange",
      "DenyStateBucketChange",
      "DenyStateVersionDeletion",
      "DenyPlanOutputChange",
      "DenyApplyOutputBucketChange",
      "DenyApplyOutputChange",
      "DenyStateKeyChange",
      "DenyGovernanceChange",
      "DenyBreakGlassChange",
    ]
    error_message = "security の apply の role の文が、統制の構成物と break-glass の変更の Deny を含む一覧になっていません。"
  }

  assert {
    condition     = !strcontains(aws_iam_role_policy.this["apply"].policy, ":policy/")
    error_message = "security の apply の role の Deny が、統制の customer managed policy を名指しています。"
  }

  assert {
    condition     = one([for s in jsondecode(aws_iam_role_policy.this["apply"].policy).Statement : s.Resource if s.Sid == "DenyBreakGlassChange"]) == ["arn:aws:iam::123456789012:role/break-glass/tabp-break-glass"]
    error_message = "security の apply の role の break-glass の Deny が、入力の role の ARN になっていません。"
  }
}

run "no_governance_in_log_archive" {
  command = apply

  variables {
    name_prefix      = "tabp-log-archive"
    env              = "log-archive"
    break_glass_arns = ["arn:aws:iam::123456789012:role/break-glass/tabp-break-glass"]
  }

  assert {
    condition     = [for s in jsondecode(aws_iam_role_policy.this["plan"].policy).Statement : s.Sid] == ["ListBuckets", "ReadBucketConfiguration", "ReadState", "WritePlanOutput", "ReadStateKey", "ReadGitHubProvider", "ReadCiRoles", "ListKeyAliases"]
    error_message = "log-archive の plan の role に、統制の構成物の読み取りが入っています。"
  }

  assert {
    condition = [for s in jsondecode(aws_iam_role_policy.apply_identity.policy).Statement : s.Sid] == [
      "ListBuckets",
      "ReadBucketConfiguration",
      "WriteIdentityState",
      "LockIdentityState",
      "ConfigureBuckets",
      "CreateApplyOutputBucket",
      "ManageGitHubProvider",
      "ManageCiRoles",
      "ReadStateKey",
      "ManageStateKey",
      "DenyLockoutSafetyBypass",
      "ReadGitHubProvider",
      "ReadCiRoles",
      "ListKeyAliases",
      "WriteApplyOutput",
    ]
    error_message = "log-archive の apply-identity の role に、統制の構成物の権限が入っています。"
  }

  assert {
    condition = [for s in jsondecode(aws_iam_role_policy.this["apply"].policy).Statement : s.Sid] == [
      "ListStateBucket",
      "ReadStateBucketEncryption",
      "WriteWorkloadState",
      "LockWorkloadState",
      "ListKeyAliases",
      "WriteWorkloadApplyOutput",
      "DenyIdentityChange",
      "DenyIdentityStateChange",
      "DenyStateBucketChange",
      "DenyStateVersionDeletion",
      "DenyPlanOutputChange",
      "DenyApplyOutputBucketChange",
      "DenyApplyOutputChange",
      "DenyStateKeyChange",
      "DenyGovernanceChange",
      "DenyBreakGlassChange",
    ]
    error_message = "log-archive の apply の role の文が、統制の構成物と break-glass の変更の Deny を含む一覧になっていません。"
  }

  assert {
    condition     = !strcontains(aws_iam_role_policy.this["apply"].policy, ":policy/")
    error_message = "log-archive の apply の role の Deny が、統制の customer managed policy を名指しています。"
  }

  assert {
    condition     = one([for s in jsondecode(aws_iam_role_policy.this["apply"].policy).Statement : s.Resource if s.Sid == "DenyBreakGlassChange"]) == ["arn:aws:iam::123456789012:role/break-glass/tabp-break-glass"]
    error_message = "log-archive の apply の role の break-glass の Deny が、入力の role の ARN になっていません。"
  }
}

run "break_glass_in_other_account" {
  command = plan

  variables {
    break_glass_arns = ["arn:aws:iam::210987654321:role/break-glass/tabp-break-glass", "arn:aws:iam::123456789012:user/break-glass/break-glass-1", "arn:aws:iam::123456789012:group/break-glass/break-glass"]
  }

  expect_failures = [aws_iam_role_policy.apply_identity]
}

run "break_glass_in_other_partition" {
  command = plan

  variables {
    break_glass_arns = ["arn:aws-cn:iam::123456789012:role/break-glass/tabp-break-glass", "arn:aws:iam::123456789012:user/break-glass/break-glass-1", "arn:aws:iam::123456789012:group/break-glass/break-glass"]
  }

  expect_failures = [aws_iam_role_policy.apply_identity]
}

run "admin_break_glass_without_user_and_group" {
  command = plan

  variables {
    break_glass_arns = ["arn:aws:iam::123456789012:role/break-glass/tabp-break-glass"]
  }

  expect_failures = [aws_iam_role_policy.apply_identity]
}

run "admin_break_glass_without_role" {
  command = plan

  variables {
    break_glass_arns = ["arn:aws:iam::123456789012:user/break-glass/break-glass-1", "arn:aws:iam::123456789012:group/break-glass/break-glass"]
  }

  expect_failures = [aws_iam_role_policy.apply_identity]
}

run "admin_break_glass_without_user" {
  command = plan

  variables {
    break_glass_arns = ["arn:aws:iam::123456789012:group/break-glass/break-glass", "arn:aws:iam::123456789012:role/break-glass/tabp-break-glass"]
  }

  expect_failures = [aws_iam_role_policy.apply_identity]
}

run "admin_break_glass_without_group" {
  command = plan

  variables {
    break_glass_arns = ["arn:aws:iam::123456789012:user/break-glass/break-glass-1", "arn:aws:iam::123456789012:role/break-glass/tabp-break-glass"]
  }

  expect_failures = [aws_iam_role_policy.apply_identity]
}

# 空の一覧では DenyBreakGlassChange の Resource が空になる。root の validation の内側でも、precondition が拒む。
run "member_break_glass_empty" {
  command = plan

  variables {
    name_prefix      = "tabp-dev"
    env              = "dev"
    break_glass_arns = []
  }

  expect_failures = [aws_iam_role_policy.apply_identity]
}

run "admin_break_glass_empty" {
  command = plan

  variables {
    break_glass_arns = []
  }

  expect_failures = [aws_iam_role_policy.apply_identity]
}
