mock_provider "aws" {
  # attachment の policy_arn は ARN の形を検証するので、mock の policy の ARN を ARN の形にする。
  mock_resource "aws_iam_policy" {
    defaults = { arn = "arn:aws:iam::123456789012:policy/mock" }
  }
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
  # endpoints）の写しである。module は宣言しない。上流とのずれは `make partitions-drift` が報告する。
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

  variables {
    # Service Authorization Reference「Actions, resources, and condition keys for AWS IAM Identity Center」で Access level が
    # Read か List の Action（2026-11-24 の写し）。決定5 の sso: の wildcard がどれにも当たらないことを確かめる。
    sso_read_actions = [
      "sso:DescribeAccountAssignmentCreationStatus",
      "sso:DescribeAccountAssignmentDeletionStatus",
      "sso:DescribeApplication",
      "sso:DescribeApplicationAssignment",
      "sso:DescribeApplicationProvider",
      "sso:DescribeInstance",
      "sso:DescribeInstanceAccessControlAttributeConfiguration",
      "sso:DescribePermissionSet",
      "sso:DescribePermissionSetProvisioningStatus",
      "sso:DescribeRegion",
      "sso:DescribeRegisteredRegions",
      "sso:DescribeTrustedTokenIssuer",
      "sso:GetApplicationAccessScope",
      "sso:GetApplicationAssignmentConfiguration",
      "sso:GetApplicationAuthenticationMethod",
      "sso:GetApplicationGrant",
      "sso:GetApplicationInstance",
      "sso:GetApplicationSessionConfiguration",
      "sso:GetApplicationTemplate",
      "sso:GetInlinePolicyForPermissionSet",
      "sso:GetManagedApplicationInstance",
      "sso:GetMfaDeviceManagementForDirectory",
      "sso:GetPermissionSet",
      "sso:GetPermissionsBoundaryForPermissionSet",
      "sso:GetProfile",
      "sso:GetSSOStatus",
      "sso:GetSharedSsoConfiguration",
      "sso:GetSsoConfiguration",
      "sso:GetTrust",
      "sso:ListAccountAssignmentCreationStatus",
      "sso:ListAccountAssignmentDeletionStatus",
      "sso:ListAccountAssignments",
      "sso:ListAccountAssignmentsForPrincipal",
      "sso:ListAccountsForProvisionedPermissionSet",
      "sso:ListApplicationAccessScopes",
      "sso:ListApplicationAssignments",
      "sso:ListApplicationAssignmentsForPrincipal",
      "sso:ListApplicationAuthenticationMethods",
      "sso:ListApplicationGrants",
      "sso:ListApplicationInstanceCertificates",
      "sso:ListApplicationInstances",
      "sso:ListApplicationProviders",
      "sso:ListApplicationTemplates",
      "sso:ListApplications",
      "sso:ListCustomerManagedPolicyReferencesInPermissionSet",
      "sso:ListDirectoryAssociations",
      "sso:ListInstances",
      "sso:ListManagedPoliciesInPermissionSet",
      "sso:ListPermissionSetProvisioningStatus",
      "sso:ListPermissionSets",
      "sso:ListPermissionSetsProvisionedToAccount",
      "sso:ListProfileAssociations",
      "sso:ListProfiles",
      "sso:ListRegions",
      "sso:ListTagsForResource",
      "sso:ListTrustedTokenIssuers",
      "sso:SearchGroups",
      "sso:SearchUsers",
    ]
  }

  override_resource {
    target = aws_iam_policy.workload
    values = { arn = "arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-workload" }
  }
  override_resource {
    target = aws_iam_policy.workload_read
    values = { arn = "arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-workload-read" }
  }
  override_resource {
    target = aws_iam_policy.governance[0]
    values = { arn = "arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-identity-governance" }
  }
  override_resource {
    target = aws_iam_policy.manage
    values = { arn = "arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-identity-manage" }
  }

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
      {
        Sid    = "ReadCiPolicies"
        Effect = "Allow"
        Action = ["iam:GetPolicy", "iam:GetPolicyVersion", "iam:ListPolicyTags", "iam:ListPolicyVersions"]
        Resource = [
          "arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-identity-manage",
          "arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-identity-governance",
          "arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-workload",
          "arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-workload-read",
          "arn:aws:iam::123456789012:policy/tabp-admin-ci-boundary-ops-notification",
        ]
      },
      {
        Sid      = "ReadWorkloadTopics"
        Effect   = "Allow"
        Action   = ["sns:GetTopicAttributes", "sns:ListTagsForResource"]
        Resource = ["arn:aws:sns:ap-northeast-1:123456789012:tabp-admin-*", "arn:aws:sns:us-east-1:123456789012:tabp-admin-*"]
      },
      {
        Sid      = "ReadWorkloadKeys"
        Effect   = "Allow"
        Action   = ["kms:DescribeKey", "kms:GetKeyPolicy", "kms:GetKeyRotationStatus", "kms:ListResourceTags"]
        Resource = ["arn:aws:kms:ap-northeast-1:123456789012:key/*", "arn:aws:kms:us-east-1:123456789012:key/*"]
        Condition = {
          Bool         = { "kms:MultiRegion" = "false" }
          StringEquals = { "kms:KeyOrigin" = "AWS_KMS", "kms:KeySpec" = "SYMMETRIC_DEFAULT", "kms:KeyUsage" = "ENCRYPT_DECRYPT" }
        }
      },
      {
        Sid    = "ReadWorkloadSchedules"
        Effect = "Allow"
        Action = ["scheduler:GetSchedule", "scheduler:GetScheduleGroup", "scheduler:ListTagsForResource"]
        Resource = [
          "arn:aws:scheduler:ap-northeast-1:123456789012:schedule-group/tabp-admin-*",
          "arn:aws:scheduler:ap-northeast-1:123456789012:schedule/tabp-admin-*/*",
          "arn:aws:scheduler:us-east-1:123456789012:schedule-group/tabp-admin-*",
          "arn:aws:scheduler:us-east-1:123456789012:schedule/tabp-admin-*/*",
        ]
      },
      { Sid = "DescribeChatConfigurations", Effect = "Allow", Action = "chatbot:DescribeSlackChannelConfigurations", Resource = "*" },
      {
        Sid      = "ReadWorkloadRoles"
        Effect   = "Allow"
        Action   = ["iam:GetRole", "iam:GetRolePolicy", "iam:ListAttachedRolePolicies", "iam:ListRolePolicies"]
        Resource = "arn:aws:iam::123456789012:role/tabp-admin-*"
      },
      {
        Sid      = "ReadWorkloadPolicies"
        Effect   = "Allow"
        Action   = ["iam:GetPolicy", "iam:GetPolicyVersion"]
        Resource = "arn:aws:iam::123456789012:policy/tabp-admin-*"
      },
      {
        Sid      = "ReadWorkloadBudgets"
        Effect   = "Allow"
        Action   = ["budgets:ListTagsForResource", "budgets:ViewBudget"]
        Resource = "arn:aws:budgets::123456789012:budget/tabp-admin-*"
      },
      {
        Sid      = "ReadAnomalyDetection"
        Effect   = "Allow"
        Action   = ["ce:GetAnomalyMonitors", "ce:GetAnomalySubscriptions", "ce:ListTagsForResource"]
        Resource = ["arn:aws:ce::123456789012:anomalymonitor/*", "arn:aws:ce::123456789012:anomalysubscription/*"]
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
        Sid    = "ReadBreakGlassStackSet"
        Effect = "Allow"
        Action = [
          "cloudformation:DescribeStackInstance",
          "cloudformation:DescribeStackSet",
          "cloudformation:DescribeStackSetOperation",
          "cloudformation:ListStackInstances",
          "cloudformation:ListStackSetOperationResults",
        ]
        Resource = ["arn:aws:cloudformation:ap-northeast-1:123456789012:stackset/tabp-break-glass:*"]
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
          "arn:aws:iam::123456789012:role/tabp-admin-ci-*",
          "arn:aws:iam::123456789012:policy/tabp-admin-ci-*",
          aws_iam_openid_connect_provider.github.arn,
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
          "sso:*AccountAssignment",
          "sso:*FromPermissionSet",
          "sso:*ToPermissionSet",
          "sso:CreatePermissionSet",
          "sso:DeletePermissionSet",
          "sso:ProvisionPermissionSet",
          "sso:PutPermissionsPolicy",
          "sso:UpdatePermissionSet",
          "cloudformation:ActivateOrganizationsAccess",
          "cloudformation:CreateStackInstances",
          "cloudformation:CreateStackSet",
          "cloudformation:DeactivateOrganizationsAccess",
          "cloudformation:DeleteStackInstances",
          "cloudformation:DeleteStackSet",
          "cloudformation:ImportStacksToStackSet",
          "cloudformation:StopStackSetOperation",
          "cloudformation:UpdateStackInstances",
          "cloudformation:UpdateStackSet",
          "sts:AssumeRoot",
        ]
        Resource = "*"
      },
      {
        Sid    = "DenyBreakGlassChange"
        Effect = "Deny"
        Action = "*"
        Resource = [
          "arn:aws:iam::123456789012:group/break-glass/*",
          "arn:aws:iam::123456789012:role/break-glass/*",
          "arn:aws:iam::123456789012:user/break-glass/*",
        ]
      },
      { Sid = "DenyBoundaryRemoval", Effect = "Deny", Action = "iam:DeleteRolePermissionsBoundary", Resource = "*" },
      {
        Sid       = "DenyLockoutSafetyBypass"
        Effect    = "Deny"
        Action    = ["kms:CreateKey", "kms:PutKeyPolicy"]
        Resource  = "*"
        Condition = { Bool = { "kms:BypassPolicyLockoutSafetyCheck" = "true" } }
      },
      {
        Sid       = "DenyBootstrapAnomalyDetectionChange"
        Effect    = "Deny"
        Action    = ["ce:DeleteAnomalyMonitor", "ce:DeleteAnomalySubscription", "ce:UntagResource", "ce:UpdateAnomalyMonitor", "ce:UpdateAnomalySubscription"]
        Resource  = ["arn:aws:ce::123456789012:anomalymonitor/*", "arn:aws:ce::123456789012:anomalysubscription/*"]
        Condition = { Null = { "aws:ResourceTag/tabp:bootstrap" = "false" } }
      },
    ]
    error_message = "apply の role の権限が、init の前の暗号化の設定と alias の一覧と、workload の state と lock の読み書きと、apply の出力の書き込みと、bootstrap の構成物・identity の state・3つの Bucket・plan の出力・apply の出力の書き込みのほか・state の鍵の変更と、tag を持つ bootstrap の異常検知の変更の拒否になっていません。"
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
        Sid       = "DenyLockoutSafetyBypass"
        Effect    = "Deny"
        Action    = ["kms:CreateKey", "kms:PutKeyPolicy"]
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
        Sid    = "ReadCiPolicies"
        Effect = "Allow"
        Action = ["iam:GetPolicy", "iam:GetPolicyVersion", "iam:ListPolicyTags", "iam:ListPolicyVersions"]
        Resource = [
          "arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-identity-manage",
          "arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-identity-governance",
          "arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-workload",
          "arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-workload-read",
          "arn:aws:iam::123456789012:policy/tabp-admin-ci-boundary-ops-notification",
        ]
      },
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
        Sid    = "ReadBreakGlassStackSet"
        Effect = "Allow"
        Action = [
          "cloudformation:DescribeStackInstance",
          "cloudformation:DescribeStackSet",
          "cloudformation:DescribeStackSetOperation",
          "cloudformation:ListStackInstances",
          "cloudformation:ListStackSetOperationResults",
        ]
        Resource = ["arn:aws:cloudformation:ap-northeast-1:123456789012:stackset/tabp-break-glass:*"]
      },
      {
        Sid    = "ManageCiPolicies"
        Effect = "Allow"
        Action = [
          "iam:CreatePolicy",
          "iam:CreatePolicyVersion",
          "iam:DeletePolicy",
          "iam:DeletePolicyVersion",
          "iam:SetDefaultPolicyVersion",
        ]
        Resource = [
          "arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-identity-manage",
          "arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-identity-governance",
          "arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-workload",
          "arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-workload-read",
          "arn:aws:iam::123456789012:policy/tabp-admin-ci-boundary-ops-notification",
        ]
      },
      {
        Sid       = "AttachApplyWorkloadPolicy"
        Effect    = "Allow"
        Action    = "iam:AttachRolePolicy"
        Resource  = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply"
        Condition = { ArnEquals = { "iam:PolicyARN" = ["arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-workload", "arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-workload-read"] } }
      },
      {
        Sid      = "AttachApplyIdentityPolicies"
        Effect   = "Allow"
        Action   = "iam:AttachRolePolicy"
        Resource = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
        Condition = {
          ArnEquals = { "iam:PolicyARN" = ["arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-identity-manage", "arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-identity-governance"] }
        }
      },
    ]
    error_message = "apply-identity の role の inline policy が、決定21 の核（identity の state と lock の読み書き、CI の role と policy の管理、identity root の構成物の読み取り、apply の出力の書き込み）だけになっていません。"
  }

  assert {
    condition     = aws_iam_policy.manage.name == "tabp-admin-ci-apply-identity-manage"
    error_message = "apply-identity の role の manage の policy の名前が <name_prefix>-ci-apply-identity-manage になっていません。"
  }

  # deployment-identity/ADR-0201 決定21。
  assert {
    condition = jsondecode(aws_iam_policy.manage.policy).Statement == [
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
        Sid      = "ManageStateKey"
        Effect   = "Allow"
        Action   = ["kms:EnableKeyRotation", "kms:PutKeyPolicy"]
        Resource = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
      },
    ]
    error_message = "apply-identity の role の manage の policy の文書が、3つの Bucket の設定、apply の出力の Bucket の作成、OIDC provider の管理、state の鍵の rotation と Key Policy の設定だけになっていません。"
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
    condition = jsonencode([
      [aws_iam_role_policy_attachments_exclusive.this["plan"].role_name, sort(tolist(aws_iam_role_policy_attachments_exclusive.this["plan"].policy_arns))],
      [aws_iam_role_policy_attachments_exclusive.this["apply"].role_name, sort(tolist(aws_iam_role_policy_attachments_exclusive.this["apply"].policy_arns))],
      [aws_iam_role_policy_attachments_exclusive.this["apply_identity"].role_name, sort(tolist(aws_iam_role_policy_attachments_exclusive.this["apply_identity"].policy_arns))],
      [aws_iam_role_policy_attachments_exclusive.this["unlock"].role_name, sort(tolist(aws_iam_role_policy_attachments_exclusive.this["unlock"].policy_arns))],
      [aws_iam_role_policy_attachments_exclusive.this["unlock_identity"].role_name, sort(tolist(aws_iam_role_policy_attachments_exclusive.this["unlock_identity"].policy_arns))],
      ]) == jsonencode([
      [aws_iam_role.this["plan"].name, []],
      [aws_iam_role.this["apply"].name, ["arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-workload", "arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-workload-read"]],
      [aws_iam_role.apply_identity.name, ["arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-identity-governance", "arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-identity-manage"]],
      [aws_iam_role.this["unlock"].name, []],
      [aws_iam_role.this["unlock_identity"].name, []],
    ])
    error_message = "managed policy が、apply の role の workload の2つの policy と、apply-identity の role の manage の policy と、管理アカウントの apply-identity の role の統制の policy だけになっていません。"
  }

  assert {
    condition = jsonencode([
      [aws_iam_role_policy_attachment.workload.role, aws_iam_role_policy_attachment.workload.policy_arn],
      [aws_iam_role_policy_attachment.workload_read.role, aws_iam_role_policy_attachment.workload_read.policy_arn],
      [aws_iam_role_policy_attachment.manage.role, aws_iam_role_policy_attachment.manage.policy_arn],
      [aws_iam_role_policy_attachment.governance[0].role, aws_iam_role_policy_attachment.governance[0].policy_arn],
      ]) == jsonencode([
      [aws_iam_role.this["apply"].name, "arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-workload"],
      [aws_iam_role.this["apply"].name, "arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-workload-read"],
      [aws_iam_role.apply_identity.name, "arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-identity-manage"],
      [aws_iam_role.apply_identity.name, "arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-identity-governance"],
    ])
    error_message = "workload の2つの policy と manage と統制の policy を付けるのが、それぞれの role への attachment になっていません。"
  }

  assert {
    condition = alltrue(concat(
      [for r in concat(values(aws_iam_role.this), [aws_iam_role.apply_identity]) : startswith(r.name, "${var.name_prefix}-ci-")],
      [for a in one([for s in jsondecode(aws_iam_role_policy.apply_identity.policy).Statement : s.Resource if s.Sid == "ManageCiPolicies"]) : startswith(a, "arn:aws:iam::123456789012:policy/${var.name_prefix}-ci-")],
    ))
    error_message = "CI の role か customer managed policy の名前が `<name_prefix>-ci-` で始まらず、apply の role の DenyIdentityChange の pattern に入りません。"
  }

  # deployment-identity/ADR-0201 決定13・14・17。
  assert {
    condition     = jsondecode(aws_iam_policy.workload_read.policy).Statement == [for s in jsondecode(aws_iam_role_policy.this["plan"].policy).Statement : s if contains(["ReadWorkloadTopics", "ReadWorkloadKeys", "ReadWorkloadSchedules", "DescribeChatConfigurations", "ReadWorkloadRoles", "ReadWorkloadPolicies", "ReadWorkloadBudgets", "ReadAnomalyDetection"], s.Sid)]
    error_message = "apply の role の workload の読み取りの policy が、plan の role の workload の読み取りと同じ文になっていません。"
  }

  assert {
    condition = jsondecode(aws_iam_policy.workload.policy).Statement == concat(
      [
        {
          Sid      = "CreateWorkloadKeys"
          Effect   = "Allow"
          Action   = "kms:CreateKey"
          Resource = "*"
          Condition = {
            StringEquals         = { "kms:KeySpec" = "SYMMETRIC_DEFAULT", "kms:KeyUsage" = "ENCRYPT_DECRYPT" }
            StringEqualsIfExists = { "kms:KeyOrigin" = "AWS_KMS" }
            BoolIfExists         = { "kms:MultiRegion" = "false" }
          }
        },
        {
          Sid      = "ManageWorkloadKeys"
          Effect   = "Allow"
          Action   = ["kms:EnableKeyRotation", "kms:PutKeyPolicy"]
          Resource = ["arn:aws:kms:ap-northeast-1:123456789012:key/*", "arn:aws:kms:us-east-1:123456789012:key/*"]
          Condition = {
            Bool         = { "kms:MultiRegion" = "false" }
            StringEquals = { "kms:KeyOrigin" = "AWS_KMS", "kms:KeySpec" = "SYMMETRIC_DEFAULT", "kms:KeyUsage" = "ENCRYPT_DECRYPT" }
          }
        },
        {
          Sid      = "ManageWorkloadKeyAliases"
          Effect   = "Allow"
          Action   = ["kms:CreateAlias", "kms:DeleteAlias", "kms:UpdateAlias"]
          Resource = ["arn:aws:kms:ap-northeast-1:123456789012:alias/tabp-admin-*", "arn:aws:kms:us-east-1:123456789012:alias/tabp-admin-*", "arn:aws:kms:ap-northeast-1:123456789012:key/*", "arn:aws:kms:us-east-1:123456789012:key/*"]
        },
        {
          Sid      = "ManageWorkloadTopics"
          Effect   = "Allow"
          Action   = ["sns:CreateTopic", "sns:SetTopicAttributes"]
          Resource = ["arn:aws:sns:ap-northeast-1:123456789012:tabp-admin-*", "arn:aws:sns:us-east-1:123456789012:tabp-admin-*"]
        },
        {
          Sid    = "ManageWorkloadSchedules"
          Effect = "Allow"
          Action = ["scheduler:CreateSchedule", "scheduler:CreateScheduleGroup", "scheduler:DeleteSchedule", "scheduler:DeleteScheduleGroup", "scheduler:UpdateSchedule"]
          Resource = [
            "arn:aws:scheduler:ap-northeast-1:123456789012:schedule-group/tabp-admin-*",
            "arn:aws:scheduler:ap-northeast-1:123456789012:schedule/tabp-admin-*/*",
            "arn:aws:scheduler:us-east-1:123456789012:schedule-group/tabp-admin-*",
            "arn:aws:scheduler:us-east-1:123456789012:schedule/tabp-admin-*/*",
          ]
        },
        { Sid = "CreateChatConfigurations", Effect = "Allow", Action = "chatbot:CreateSlackChannelConfiguration", Resource = "*" },
        {
          Sid      = "ManageChatConfigurations"
          Effect   = "Allow"
          Action   = ["chatbot:DeleteSlackChannelConfiguration", "chatbot:UpdateSlackChannelConfiguration"]
          Resource = "arn:aws:chatbot::123456789012:chat-configuration/slack-channel/tabp-admin-*"
        },
        {
          Sid    = "ManageBoundedRoles"
          Effect = "Allow"
          Action = [
            "iam:CreateRole",
            "iam:DeleteRole",
            "iam:DeleteRolePolicy",
            "iam:DetachRolePolicy",
            "iam:PutRolePermissionsBoundary",
            "iam:PutRolePolicy",
            "iam:UpdateAssumeRolePolicy",
            "iam:UpdateRole",
            "iam:UpdateRoleDescription",
          ]
          Resource  = ["arn:aws:iam::123456789012:role/tabp-admin-ops-*"]
          Condition = { StringEquals = { "iam:PermissionsBoundary" = ["arn:aws:iam::123456789012:policy/tabp-admin-ci-boundary-ops-notification"] } }
        },
        {
          Sid      = "AttachWorkloadPolicies"
          Effect   = "Allow"
          Action   = "iam:AttachRolePolicy"
          Resource = ["arn:aws:iam::123456789012:role/tabp-admin-ops-*"]
          Condition = {
            StringEquals = { "iam:PermissionsBoundary" = ["arn:aws:iam::123456789012:policy/tabp-admin-ci-boundary-ops-notification"] }
            ArnLike      = { "iam:PolicyARN" = ["arn:aws:iam::123456789012:policy/tabp-admin-ops-*"] }
          }
        },
        {
          Sid      = "ListWorkloadRoleInstanceProfiles"
          Effect   = "Allow"
          Action   = "iam:ListInstanceProfilesForRole"
          Resource = "arn:aws:iam::123456789012:role/tabp-admin-*"
        },
        {
          Sid    = "ManageWorkloadPolicies"
          Effect = "Allow"
          Action = [
            "iam:CreatePolicy",
            "iam:CreatePolicyVersion",
            "iam:DeletePolicy",
            "iam:DeletePolicyVersion",
            "iam:ListPolicyVersions",
            "iam:SetDefaultPolicyVersion",
          ]
          Resource = ["arn:aws:iam::123456789012:policy/tabp-admin-ops-*"]
        },
        {
          Sid       = "PassWorkloadRoles"
          Effect    = "Allow"
          Action    = "iam:PassRole"
          Resource  = ["arn:aws:iam::123456789012:role/tabp-admin-ops-*"]
          Condition = { StringEquals = { "iam:PassedToService" = ["chatbot.amazonaws.com", "scheduler.amazonaws.com"] } }
        },
        {
          Sid       = "CreateChatbotServiceLinkedRole"
          Effect    = "Allow"
          Action    = "iam:CreateServiceLinkedRole"
          Resource  = "arn:aws:iam::123456789012:role/aws-service-role/management.chatbot.amazonaws.com/AWSServiceRoleForAWSChatbot"
          Condition = { StringEquals = { "iam:AWSServiceName" = "management.chatbot.amazonaws.com" } }
        },
        {
          Sid      = "ManageWorkloadBudgets"
          Effect   = "Allow"
          Action   = "budgets:ModifyBudget"
          Resource = "arn:aws:budgets::123456789012:budget/tabp-admin-*"
        },
        { Sid = "CreateAnomalyMonitors", Effect = "Allow", Action = "ce:CreateAnomalyMonitor", Resource = "*" },
        { Sid = "CreateAnomalySubscriptions", Effect = "Allow", Action = "ce:CreateAnomalySubscription", Resource = "*" },
        {
          Sid      = "ManageAnomalyDetection"
          Effect   = "Allow"
          Action   = ["ce:DeleteAnomalyMonitor", "ce:DeleteAnomalySubscription", "ce:UpdateAnomalyMonitor", "ce:UpdateAnomalySubscription"]
          Resource = ["arn:aws:ce::123456789012:anomalymonitor/*", "arn:aws:ce::123456789012:anomalysubscription/*"]
        },
      ],
    )
    error_message = "apply の role の workload の policy の文書が、決定13・14・18 の管理になっていません。"
  }

  assert {
    condition     = aws_iam_policy.workload.name == "tabp-admin-ci-apply-workload" && aws_iam_policy.workload_read.name == "tabp-admin-ci-apply-workload-read" && [for k, p in aws_iam_policy.boundary : [k, p.name]] == [["ops-notification", "tabp-admin-ci-boundary-ops-notification"]]
    error_message = "apply の role の workload の policy と boundary の名前が、<name_prefix>-ci-apply-workload・<name_prefix>-ci-apply-workload-read と <name_prefix>-ci-boundary-<usecase> になっていません。"
  }

  # deployment-identity/ADR-0201 決定5・15・16。
  assert {
    condition = jsondecode(aws_iam_policy.boundary["ops-notification"].policy).Statement == concat(
      [
        { Sid = "PublishTopics", Effect = "Allow", Action = "sns:Publish", Resource = ["arn:aws:sns:ap-northeast-1:123456789012:tabp-admin-ops-*", "arn:aws:sns:us-east-1:123456789012:tabp-admin-ops-*"] },
        {
          Sid       = "UseKeysViaSns"
          Effect    = "Allow"
          Action    = ["kms:Decrypt", "kms:GenerateDataKey"]
          Resource  = ["arn:aws:kms:ap-northeast-1:123456789012:key/*", "arn:aws:kms:us-east-1:123456789012:key/*"]
          Condition = { StringEquals = { "kms:ViaService" = ["sns.ap-northeast-1.amazonaws.com", "sns.us-east-1.amazonaws.com"] } }
        },
        {
          Sid      = "ReadAlarms"
          Effect   = "Allow"
          Action   = ["cloudwatch:DescribeAlarmHistory", "cloudwatch:DescribeAlarms", "cloudwatch:ListTagsForResource"]
          Resource = ["arn:aws:cloudwatch:ap-northeast-1:123456789012:alarm:*", "arn:aws:cloudwatch:us-east-1:123456789012:alarm:*"]
        },
        { Sid = "DescribeAlarmsForMetric", Effect = "Allow", Action = "cloudwatch:DescribeAlarmsForMetric", Resource = "*" },
        { Sid = "GetMetricData", Effect = "Allow", Action = "cloudwatch:GetMetricData", Resource = "*" },
        { Sid = "GetMetricStatistics", Effect = "Allow", Action = "cloudwatch:GetMetricStatistics", Resource = "*" },
        { Sid = "GetMetricWidgetImage", Effect = "Allow", Action = "cloudwatch:GetMetricWidgetImage", Resource = "*" },
        { Sid = "ListMetrics", Effect = "Allow", Action = "cloudwatch:ListMetrics", Resource = "*" },
      ],
      [for s in jsondecode(aws_iam_role_policy.this["apply"].policy).Statement : s if s.Effect == "Deny" && !contains(["DenyBoundaryRemoval", "DenyLockoutSafetyBypass", "DenyBootstrapAnomalyDetectionChange"], s.Sid)],
    )
    error_message = "ops-notification の boundary の文書が、心拍と Chatbot の role の要る Allow と、apply の role の bootstrap の Deny になっていません。"
  }

  assert {
    condition     = length([for s in jsondecode(aws_iam_policy.boundary["ops-notification"].policy).Statement : s if s.Effect == "Deny"]) == 10
    error_message = "boundary の Deny の文が、apply の role の bootstrap の Deny の10文になっていません。"
  }

  # IAM の wildcard を正規表現へ訳し（metacharacter を escape してから * を .*、? を . に）、サービスの欄が大文字小文字を
  # 問わず sso に当たりうる pattern をすべて、Read・List の Action に当てる。
  assert {
    condition = alltrue(flatten([
      for p in flatten([one([for s in jsondecode(aws_iam_role_policy.this["apply"].policy).Statement : s.Action if s.Sid == "DenyGovernanceChange"])]) : [
        for r in var.sso_read_actions : !can(regex("^(?i)${replace(replace(replace(p, "/([.+^$()\\[\\]{}|\\\\])/", "\\$1"), "*", ".*"), "?", ".")}$", r))
      ] if can(regex("^(?i)${replace(replace(replace(split(":", p)[0], "/([.+^$()\\[\\]{}|\\\\])/", "\\$1"), "*", ".*"), "?", ".")}$", "sso"))
    ]))
    error_message = "DenyGovernanceChange の Action が、Identity Center の Read・List の Action に当たります（決定5）。"
  }

  assert {
    condition     = length([for p in flatten([one([for s in jsondecode(aws_iam_role_policy.this["apply"].policy).Statement : s.Action if s.Sid == "DenyGovernanceChange"])]) : p if can(regex("^(?i)${replace(replace(replace(split(":", p)[0], "/([.+^$()\\[\\]{}|\\\\])/", "\\$1"), "*", ".*"), "?", ".")}$", "sso"))]) > 0
    error_message = "DenyGovernanceChange が sso の Action を持たず、上の Read・List の assert が何も見ていません。"
  }

  assert {
    condition     = length(var.sso_read_actions) == 58
    error_message = "Identity Center の Read・List の Action の写しが 58 件ではありません。"
  }

  assert {
    condition     = aws_iam_policy.governance[0].name == "tabp-admin-ci-apply-identity-governance"
    error_message = "統制の customer managed policy の名前が <name_prefix>-ci-apply-identity-governance になっていません。"
  }

  assert {
    condition = jsondecode(aws_iam_policy.governance[0].policy).Statement == [
      { Sid = "IamDeleteAccountPasswordPolicy", Effect = "Allow", Action = "iam:DeleteAccountPasswordPolicy", Resource = "*" },
      {
        Sid      = "IamDisableOrganizationsRootCredentialsManagement"
        Effect   = "Allow"
        Action   = "iam:DisableOrganizationsRootCredentialsManagement"
        Resource = "*"
      },
      { Sid = "IamDisableOrganizationsRootSessions", Effect = "Allow", Action = "iam:DisableOrganizationsRootSessions", Resource = "*" },
      {
        Sid      = "IamEnableOrganizationsRootCredentialsManagement"
        Effect   = "Allow"
        Action   = "iam:EnableOrganizationsRootCredentialsManagement"
        Resource = "*"
      },
      { Sid = "IamEnableOrganizationsRootSessions", Effect = "Allow", Action = "iam:EnableOrganizationsRootSessions", Resource = "*" },
      { Sid = "IamUpdateAccountPasswordPolicy", Effect = "Allow", Action = "iam:UpdateAccountPasswordPolicy", Resource = "*" },
      { Sid = "OrganizationsCreateOrganization", Effect = "Allow", Action = "organizations:CreateOrganization", Resource = "*" },
      {
        Sid       = "CloudformationCreateStackSet"
        Effect    = "Allow"
        Action    = "cloudformation:CreateStackSet"
        Resource  = "*"
        Condition = { Null = { "aws:TagKeys" = "true" } }
      },
      {
        Sid       = "OrganizationsDisableAWSServiceAccess"
        Effect    = "Allow"
        Action    = "organizations:DisableAWSServiceAccess"
        Resource  = "*"
        Condition = { StringEquals = { "organizations:ServicePrincipal" = "iam.amazonaws.com" } }
      },
      {
        Sid       = "OrganizationsEnableAWSServiceAccess"
        Effect    = "Allow"
        Action    = "organizations:EnableAWSServiceAccess"
        Resource  = "*"
        Condition = { StringEquals = { "organizations:ServicePrincipal" = "iam.amazonaws.com" } }
      },
      {
        Sid       = "CreateOrganizationsServiceLinkedRole"
        Effect    = "Allow"
        Action    = "iam:CreateServiceLinkedRole"
        Resource  = "arn:aws:iam::123456789012:role/aws-service-role/organizations.amazonaws.com/AWSServiceRoleForOrganizations"
        Condition = { StringEquals = { "iam:AWSServiceName" = "organizations.amazonaws.com" } }
      },
      {
        Sid      = "ManagePolicyTypes"
        Effect   = "Allow"
        Action   = ["organizations:DisablePolicyType", "organizations:EnablePolicyType"]
        Resource = ["arn:aws:organizations::123456789012:root/o-*/r-*"]
      },
      {
        Sid    = "CreateOrganizationalUnits"
        Effect = "Allow"
        Action = "organizations:CreateOrganizationalUnit"
        Resource = [
          "arn:aws:organizations::123456789012:root/o-*/r-*",
          "arn:aws:organizations::123456789012:ou/o-*/ou-*",
        ]
      },
      {
        Sid      = "UpdateOrganizationalUnits"
        Effect   = "Allow"
        Action   = "organizations:UpdateOrganizationalUnit"
        Resource = ["arn:aws:organizations::123456789012:ou/o-*/ou-*"]
      },
      {
        Sid      = "ManageOrganizationPolicies"
        Effect   = "Allow"
        Action   = ["organizations:CreatePolicy", "organizations:DeletePolicy", "organizations:UpdatePolicy"]
        Resource = "arn:aws:organizations::123456789012:policy/o-*/*/p-*"
      },
      {
        Sid    = "AttachOrganizationPolicies"
        Effect = "Allow"
        Action = ["organizations:AttachPolicy", "organizations:DetachPolicy"]
        Resource = [
          "arn:aws:organizations::123456789012:root/o-*/r-*",
          "arn:aws:organizations::123456789012:ou/o-*/ou-*",
          "arn:aws:organizations::123456789012:policy/o-*/*/p-*",
        ]
      },
      {
        Sid    = "ManageBreakGlassUsers"
        Effect = "Allow"
        Action = ["iam:CreateUser", "iam:UpdateUser"]
        Resource = [
          "arn:aws:iam::123456789012:user/break-glass/break-glass-1",
          "arn:aws:iam::123456789012:user/break-glass/break-glass-2",
        ]
      },
      {
        Sid    = "ManageBreakGlassGroup"
        Effect = "Allow"
        Action = [
          "iam:AddUserToGroup",
          "iam:CreateGroup",
          "iam:DeleteGroup",
          "iam:DeleteGroupPolicy",
          "iam:ListAttachedGroupPolicies",
          "iam:ListGroupPolicies",
          "iam:PutGroupPolicy",
          "iam:RemoveUserFromGroup",
          "iam:UpdateGroup",
        ]
        Resource = ["arn:aws:iam::123456789012:group/break-glass/break-glass"]
      },
      {
        Sid      = "ManageBreakGlassRole"
        Effect   = "Allow"
        Action   = ["iam:CreateRole", "iam:UpdateAssumeRolePolicy", "iam:UpdateRole", "iam:UpdateRoleDescription"]
        Resource = ["arn:aws:iam::123456789012:role/break-glass/tabp-break-glass"]
      },
      {
        Sid       = "AttachBreakGlassAdministratorAccess"
        Effect    = "Allow"
        Action    = ["iam:AttachRolePolicy", "iam:DetachRolePolicy"]
        Resource  = ["arn:aws:iam::123456789012:role/break-glass/tabp-break-glass"]
        Condition = { ArnEquals = { "iam:PolicyARN" = "arn:aws:iam::aws:policy/AdministratorAccess" } }
      },
      {
        Sid    = "UpdateBreakGlassStackSet"
        Effect = "Allow"
        Action = "cloudformation:UpdateStackSet"
        Resource = [
          "arn:aws:cloudformation:ap-northeast-1:123456789012:stackset/tabp-break-glass:*",
          "arn:aws:cloudformation:ap-northeast-1:123456789012:stackset-target/tabp-break-glass:*",
          "arn:aws:cloudformation:ap-northeast-1:*:type/resource/AWS-IAM-Role",
        ]
      },
      {
        Sid    = "ManageBreakGlassStackInstances"
        Effect = "Allow"
        Action = ["cloudformation:CreateStackInstances", "cloudformation:UpdateStackInstances"]
        Resource = [
          "arn:aws:cloudformation:ap-northeast-1:123456789012:stackset/tabp-break-glass:*",
          "arn:aws:cloudformation:ap-northeast-1:123456789012:stackset-target/tabp-break-glass:*",
          "arn:aws:cloudformation:ap-northeast-1:*:type/resource/AWS-IAM-Role",
        ]
        Condition = { StringEquals = { "cloudformation:TargetRegion" = "ap-northeast-1" } }
      },
    ]
    error_message = "統制の customer managed policy の文書が、統制の構成物の作成と設定だけになっていません。"
  }

  # deployment-identity/ADR-0201 決定20 と「与えないもの」。
  assert {
    condition = alltrue(flatten([
      for d in [aws_iam_policy.workload.policy, aws_iam_policy.workload_read.policy, aws_iam_role_policy.this["plan"].policy] : [
        for s in jsondecode(d).Statement : [
          for a in flatten([try(s.Action, [])]) : !can(regex("^((budgets|ce):(tag|untag)resource|aws-portal:.*|budgets:(createbudgetaction|executebudgetaction))$", lower(a)))
        ]
      ]
    ]))
    error_message = "workload の2つの policy か plan の role の policy が、budgets・ce の tag の書き込み、aws-portal、予算の action のどれかを許しています。"
  }

  assert {
    condition = [
      [for s in jsondecode(aws_iam_policy.workload.policy).Statement : s.Sid if s.Resource == "*"],
      [for s in jsondecode(aws_iam_policy.workload_read.policy).Statement : s.Sid if s.Resource == "*"],
      ] == [
      ["CreateWorkloadKeys", "CreateChatConfigurations", "CreateAnomalyMonitors", "CreateAnomalySubscriptions"],
      ["DescribeChatConfigurations"],
    ]
    error_message = "workload の2つの policy の Resource \"*\" の文が、resource-level 権限を持たない Action の文だけになっていません。"
  }

  # deployment-identity/ADR-0201 決定13。
  assert {
    condition = one([
      for s in jsondecode(aws_iam_role_policy.this["apply"].policy).Statement : [s.Effect, s.Condition, contains(s.Action, "ce:UntagResource"), contains(s.Action, "ce:TagResource")]
      if s.Sid == "DenyBootstrapAnomalyDetectionChange"
    ]) == ["Deny", { Null = { "aws:ResourceTag/tabp:bootstrap" = "false" } }, true, false]
    error_message = "apply の role に、tag tabp:bootstrap の key を持つ anomaly monitor と subscription の変更と tag の除去を拒む Deny がありません。"
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
      one([for s in jsondecode(aws_iam_policy.manage.policy).Statement : s.Resource if s.Sid == "ManageStateKey"]),
      ] == [
      "arn:aws-us-gov:kms:us-gov-west-1:210987654321:key/abcdef01-2345-6789-abcd-ef0123456789",
      "arn:aws-us-gov:kms:us-gov-west-1:210987654321:key/abcdef01-2345-6789-abcd-ef0123456789",
      "arn:aws-us-gov:kms:us-gov-west-1:210987654321:key/abcdef01-2345-6789-abcd-ef0123456789",
      "arn:aws-us-gov:kms:us-gov-west-1:210987654321:key/abcdef01-2345-6789-abcd-ef0123456789",
    ]
    error_message = "plan の読み取り、apply の Deny、apply-identity の読み取りと manage の policy の管理の Resource が、入力の鍵の ARN になっていません。"
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

run "rejects_break_glass_role_name_with_underscore_in_admin" {
  command = plan

  variables {
    break_glass_arns = ["arn:aws:iam::123456789012:user/break-glass/break-glass-1", "arn:aws:iam::123456789012:group/break-glass/break-glass", "arn:aws:iam::123456789012:role/break-glass/tabp_break_glass"]
  }

  expect_failures = [aws_iam_role_policy.apply_identity]
}

run "rejects_break_glass_role_name_with_leading_digit_in_admin" {
  command = plan

  variables {
    break_glass_arns = ["arn:aws:iam::123456789012:user/break-glass/break-glass-1", "arn:aws:iam::123456789012:group/break-glass/break-glass", "arn:aws:iam::123456789012:role/break-glass/1tabp-break-glass"]
  }

  expect_failures = [aws_iam_role_policy.apply_identity]
}

# StackSet を持たない env では、role の名前を StackSet の名前として読まない。
run "accepts_break_glass_role_name_with_underscore_outside_admin" {
  command = plan

  variables {
    env              = "dev"
    name_prefix      = "tabp-dev"
    break_glass_arns = ["arn:aws:iam::123456789012:role/break-glass/tabp_break_glass"]
  }
}

run "accepts_single_letter_break_glass_role_name_in_admin" {
  command = plan

  variables {
    break_glass_arns = ["arn:aws:iam::123456789012:user/break-glass/break-glass-1", "arn:aws:iam::123456789012:group/break-glass/break-glass", "arn:aws:iam::123456789012:role/break-glass/a"]
  }
}

# mock の ARN は実物より短いので、入力が許す最も長い値（33文字の project と log-archive、63文字の Bucket、
# 最長の組成の break-glass）と、それに対応する長さの ARN を与えて数える。role の ARN は名前から組むので、
# partition と name_prefix から長さが決まる。partition と region は、aws_partitions の中で最も長いものを組を問わずに
# 合わせる。この組は AWS に無い。policies.tf と main.tf は partition と region を ARN の文字列に埋め込むだけなので、
# 文書はどちらの長さにも伸びる。そうでない使い方を入れたら、この組が最長である前提は崩れる。
# 名前は IAM の上限（user・role 64文字、group 128文字）。組成は README の break_glass_arns の行が持つ。
# どの run も role 名が最も長くなる log-archive の name_prefix を使う。
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
      format("arn:aws-us-gov:iam::123456789012:role/break-glass/r%063d", 4),
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
    condition = (
      strcontains(aws_iam_policy.governance[0].policy, format("stackset-target/r%063d:*", 4)) &&
      strcontains(aws_iam_role_policy.apply_identity.policy, format("stackset/r%063d:*", 4))
    )
    error_message = "長さを測る文書に、break-glass の StackSet の文が入っていません。"
  }

  assert {
    condition     = length(aws_iam_role.this["unlock_identity"].name) == 64
    error_message = "境界の入力で、最も長い role 名が IAM の上限の 64 文字になっていません。"
  }

  assert {
    condition     = alltrue([for a in one([for s in jsondecode(aws_iam_role_policy.apply_identity.policy).Statement : s.Resource if s.Sid == "ManageCiPolicies"]) : length(element(split("/", a), 1)) <= 128])
    error_message = "境界の入力で、CI の customer managed policy の名前が IAM の上限の 128 文字を超えています。"
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

  assert {
    condition     = length(aws_iam_policy.boundary) == 1 && length(aws_iam_policy.workload.policy) <= 6144 && length(aws_iam_policy.workload_read.policy) <= 6144
    error_message = "apply の role の workload の policy が 6,144 文字を超えているか、boundary の数が1つではありません。測る policy を見直してください。"
  }

  assert {
    condition     = alltrue([for p in values(aws_iam_policy.boundary) : length(p.policy) <= 6144])
    error_message = "boundary が 6,144 文字を超えています: ${join(", ", [for k, p in aws_iam_policy.boundary : k if length(p.policy) > 6144])}"
  }

  assert {
    condition     = length(aws_iam_policy.governance) == 1
    error_message = "admin の統制の customer managed policy の数が1つではありません。測る policy を見直してください。"
  }

  assert {
    condition     = alltrue([for p in aws_iam_policy.governance : length(p.policy) <= 6144])
    error_message = "統制の customer managed policy が 6,144 文字を超えています。"
  }

  assert {
    condition = alltrue([
      for m in [{
        governance    = aws_iam_policy.governance
        manage        = [aws_iam_policy.manage]
        workload      = [aws_iam_policy.workload]
        workload_read = [aws_iam_policy.workload_read]
        boundary      = values(aws_iam_policy.boundary)
      }] :
      toset(keys(m)) == toset([for a in jsondecode(file("../../../../conftest/plan/decl.json")).policy_plan.deployment_identity.customer_managed_policies : trimprefix(a, "module.deployment_identity.module.github_oidc.aws_iam_policy.")]) &&
      alltrue([for p in flatten(values(m)) : length(p.policy) <= 6144])
    ])
    error_message = "customer managed policy が 6,144 文字を超えているか、長さを測る policy の集合が conftest/plan/decl.json の deployment_identity.customer_managed_policies と一致していません。"
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
    condition     = alltrue([for a in one([for s in jsondecode(aws_iam_role_policy.apply_identity.policy).Statement : s.Resource if s.Sid == "ManageCiPolicies"]) : length(element(split("/", a), 1)) <= 128])
    error_message = "境界の入力で、CI の customer managed policy の名前が IAM の上限の 128 文字を超えています。"
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

  assert {
    condition     = length(aws_iam_policy.boundary) == 1 && length(aws_iam_policy.workload.policy) <= 6144 && length(aws_iam_policy.workload_read.policy) <= 6144
    error_message = "apply の role の workload の policy が 6,144 文字を超えているか、boundary の数が1つではありません。測る policy を見直してください。"
  }

  assert {
    condition     = alltrue([for p in values(aws_iam_policy.boundary) : length(p.policy) <= 6144])
    error_message = "boundary が 6,144 文字を超えています: ${join(", ", [for k, p in aws_iam_policy.boundary : k if length(p.policy) > 6144])}"
  }

  assert {
    condition = alltrue([
      for m in [{
        governance    = aws_iam_policy.governance
        manage        = [aws_iam_policy.manage]
        workload      = [aws_iam_policy.workload]
        workload_read = [aws_iam_policy.workload_read]
        boundary      = values(aws_iam_policy.boundary)
      }] :
      toset(keys(m)) == toset([for a in jsondecode(file("../../../../conftest/plan/decl.json")).policy_plan.deployment_identity.customer_managed_policies : trimprefix(a, "module.deployment_identity.module.github_oidc.aws_iam_policy.")]) &&
      alltrue([for p in flatten(values(m)) : length(p.policy) <= 6144])
    ])
    error_message = "customer managed policy が 6,144 文字を超えているか、長さを測る policy の集合が conftest/plan/decl.json の deployment_identity.customer_managed_policies と一致していません。"
  }
}

# 既定と違う partition とアカウントを与え、入力を読んだかを区別する。
run "policy_length_aws" {
  command = apply
  # state_key を分ける理由は policy_length と同じ。
  state_key = "policy_length_aws"

  variables {
    env                     = "admin"
    name_prefix             = "abcdefghijklmnopqrstuvwxyz0123456-log-archive"
    state_bucket_arn        = format("arn:aws:s3:::%063d", 1)
    state_key_arn           = "arn:aws:kms:ap-southeast-5:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
    plan_output_bucket_arn  = format("arn:aws:s3:::%063d", 2)
    apply_output_bucket_arn = format("arn:aws:s3:::%063d", 3)
    break_glass_arns = [
      format("arn:aws:iam::123456789012:user/break-glass/%064d", 1),
      format("arn:aws:iam::123456789012:group/break-glass/%0128d", 2),
      format("arn:aws:iam::123456789012:group/break-glass/%0128d", 3),
      format("arn:aws:iam::123456789012:role/break-glass/r%063d", 4),
    ]
  }

  override_data {
    target = data.aws_partition.current
    values = { partition = "aws" }
  }
  override_data {
    target = data.aws_region.current
    values = { region = "ap-southeast-5" }
  }
  override_resource {
    target = aws_iam_openid_connect_provider.github
    values = { arn = "arn:aws:iam::123456789012:oidc-provider/token.actions.githubusercontent.com" }
  }

  assert {
    condition = (
      data.aws_partition.current.partition == "aws"
    )
    error_message = "境界の入力の partition が aws ではありません。"
  }

  assert {
    condition = (
      contains(var.aws_partitions["aws"], data.aws_region.current.region) &&
      length(data.aws_region.current.region) == max([for r in var.aws_partitions["aws"] : length(r)]...)
    )
    error_message = "境界の入力の region が、aws の partition の中で最も長いものになっていません。"
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
    condition = (
      strcontains(aws_iam_policy.governance[0].policy, format("stackset-target/r%063d:*", 4)) &&
      strcontains(aws_iam_role_policy.apply_identity.policy, format("stackset/r%063d:*", 4))
    )
    error_message = "長さを測る文書に、break-glass の StackSet の文が入っていません。"
  }

  assert {
    condition     = length(aws_iam_role.this["unlock_identity"].name) == 64
    error_message = "境界の入力で、最も長い role 名が IAM の上限の 64 文字になっていません。"
  }

  assert {
    condition     = alltrue([for a in one([for s in jsondecode(aws_iam_role_policy.apply_identity.policy).Statement : s.Resource if s.Sid == "ManageCiPolicies"]) : length(element(split("/", a), 1)) <= 128])
    error_message = "境界の入力で、CI の customer managed policy の名前が IAM の上限の 128 文字を超えています。"
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

  assert {
    condition     = length(aws_iam_policy.boundary) == 1 && length(aws_iam_policy.workload.policy) <= 6144 && length(aws_iam_policy.workload_read.policy) <= 6144
    error_message = "apply の role の workload の policy が 6,144 文字を超えているか、boundary の数が1つではありません。測る policy を見直してください。"
  }

  assert {
    condition     = alltrue([for p in values(aws_iam_policy.boundary) : length(p.policy) <= 6144])
    error_message = "boundary が 6,144 文字を超えています: ${join(", ", [for k, p in aws_iam_policy.boundary : k if length(p.policy) > 6144])}"
  }

  assert {
    condition     = length(aws_iam_policy.governance) == 1
    error_message = "admin の統制の customer managed policy の数が1つではありません。測る policy を見直してください。"
  }

  assert {
    condition     = alltrue([for p in aws_iam_policy.governance : length(p.policy) <= 6144])
    error_message = "統制の customer managed policy が 6,144 文字を超えています。"
  }

  assert {
    condition     = one([for st in jsondecode(aws_iam_policy.boundary["ops-notification"].policy).Statement : st.Resource if st.Sid == "PublishTopics"]) == ["arn:aws:sns:ap-southeast-5:123456789012:abcdefghijklmnopqrstuvwxyz0123456-log-archive-ops-*", "arn:aws:sns:us-east-1:123456789012:abcdefghijklmnopqrstuvwxyz0123456-log-archive-ops-*"]
    error_message = "aws の partition の境界の入力で、us-east-1 の ARN が足されていません。"
  }

  assert {
    condition = (
      length(one([for st in jsondecode(aws_iam_policy.workload_read.policy).Statement : st.Resource if st.Sid == "ReadWorkloadTopics"])) == 2 &&
      length(one([for st in jsondecode(aws_iam_policy.workload.policy).Statement : st.Resource if st.Sid == "ManageWorkloadSchedules"])) == 4 &&
      length(one([for st in jsondecode(aws_iam_policy.boundary["ops-notification"].policy).Statement : st.Condition.StringEquals["kms:ViaService"] if st.Sid == "UseKeysViaSns"])) == 2
    )
    error_message = "aws の partition の境界の入力で、workload の2つの policy と boundary が2つの region の形になっていません。"
  }

  assert {
    condition = [for st in jsondecode(aws_iam_policy.workload_read.policy).Statement : st.Resource if contains(["ReadWorkloadBudgets", "ReadAnomalyDetection"], st.Sid)] == [
      "arn:aws:budgets::123456789012:budget/abcdefghijklmnopqrstuvwxyz0123456-log-archive-*",
      ["arn:aws:ce::123456789012:anomalymonitor/*", "arn:aws:ce::123456789012:anomalysubscription/*"],
    ]
    error_message = "aws の partition の境界の入力で、Budgets と Cost Explorer の Resource が region を持つか、us-east-1 の分だけ増えています。"
  }

  assert {
    condition = alltrue([
      for m in [{
        governance    = aws_iam_policy.governance
        manage        = [aws_iam_policy.manage]
        workload      = [aws_iam_policy.workload]
        workload_read = [aws_iam_policy.workload_read]
        boundary      = values(aws_iam_policy.boundary)
      }] :
      toset(keys(m)) == toset([for a in jsondecode(file("../../../../conftest/plan/decl.json")).policy_plan.deployment_identity.customer_managed_policies : trimprefix(a, "module.deployment_identity.module.github_oidc.aws_iam_policy.")]) &&
      alltrue([for p in flatten(values(m)) : length(p.policy) <= 6144])
    ])
    error_message = "customer managed policy が 6,144 文字を超えているか、長さを測る policy の集合が conftest/plan/decl.json の deployment_identity.customer_managed_policies と一致していません。"
  }
}

run "policy_length_outside_admin_aws" {
  command = apply
  # state_key を分ける理由は policy_length と同じ。
  state_key = "policy_length_outside_admin_aws"

  variables {
    env                     = "log-archive"
    name_prefix             = "abcdefghijklmnopqrstuvwxyz0123456-log-archive"
    state_bucket_arn        = format("arn:aws:s3:::%063d", 1)
    state_key_arn           = "arn:aws:kms:ap-southeast-5:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
    plan_output_bucket_arn  = format("arn:aws:s3:::%063d", 2)
    apply_output_bucket_arn = format("arn:aws:s3:::%063d", 3)
    break_glass_arns        = [for i in range(4) : format("arn:aws:iam::123456789012:group/break-glass/%0128d", i)]
  }

  override_data {
    target = data.aws_partition.current
    values = { partition = "aws" }
  }
  override_data {
    target = data.aws_region.current
    values = { region = "ap-southeast-5" }
  }
  override_resource {
    target = aws_iam_openid_connect_provider.github
    values = { arn = "arn:aws:iam::123456789012:oidc-provider/token.actions.githubusercontent.com" }
  }

  assert {
    condition = (
      data.aws_partition.current.partition == "aws"
    )
    error_message = "境界の入力の partition が aws ではありません。"
  }

  assert {
    condition = (
      contains(var.aws_partitions["aws"], data.aws_region.current.region) &&
      length(data.aws_region.current.region) == max([for r in var.aws_partitions["aws"] : length(r)]...)
    )
    error_message = "境界の入力の region が、aws の partition の中で最も長いものになっていません。"
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
    condition     = alltrue([for a in one([for s in jsondecode(aws_iam_role_policy.apply_identity.policy).Statement : s.Resource if s.Sid == "ManageCiPolicies"]) : length(element(split("/", a), 1)) <= 128])
    error_message = "境界の入力で、CI の customer managed policy の名前が IAM の上限の 128 文字を超えています。"
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

  assert {
    condition     = length(aws_iam_policy.boundary) == 1 && length(aws_iam_policy.workload.policy) <= 6144 && length(aws_iam_policy.workload_read.policy) <= 6144
    error_message = "apply の role の workload の policy が 6,144 文字を超えているか、boundary の数が1つではありません。測る policy を見直してください。"
  }

  assert {
    condition     = alltrue([for p in values(aws_iam_policy.boundary) : length(p.policy) <= 6144])
    error_message = "boundary が 6,144 文字を超えています: ${join(", ", [for k, p in aws_iam_policy.boundary : k if length(p.policy) > 6144])}"
  }

  assert {
    condition     = one([for st in jsondecode(aws_iam_policy.boundary["ops-notification"].policy).Statement : st.Resource if st.Sid == "PublishTopics"]) == ["arn:aws:sns:ap-southeast-5:123456789012:abcdefghijklmnopqrstuvwxyz0123456-log-archive-ops-*", "arn:aws:sns:us-east-1:123456789012:abcdefghijklmnopqrstuvwxyz0123456-log-archive-ops-*"]
    error_message = "aws の partition の境界の入力で、us-east-1 の ARN が足されていません。"
  }

  assert {
    condition = (
      length(one([for st in jsondecode(aws_iam_policy.workload_read.policy).Statement : st.Resource if st.Sid == "ReadWorkloadTopics"])) == 2 &&
      length(one([for st in jsondecode(aws_iam_policy.workload.policy).Statement : st.Resource if st.Sid == "ManageWorkloadSchedules"])) == 4 &&
      length(one([for st in jsondecode(aws_iam_policy.boundary["ops-notification"].policy).Statement : st.Condition.StringEquals["kms:ViaService"] if st.Sid == "UseKeysViaSns"])) == 2
    )
    error_message = "aws の partition の境界の入力で、workload の2つの policy と boundary が2つの region の形になっていません。"
  }

  assert {
    condition = alltrue([
      for m in [{
        governance    = aws_iam_policy.governance
        manage        = [aws_iam_policy.manage]
        workload      = [aws_iam_policy.workload]
        workload_read = [aws_iam_policy.workload_read]
        boundary      = values(aws_iam_policy.boundary)
      }] :
      toset(keys(m)) == toset([for a in jsondecode(file("../../../../conftest/plan/decl.json")).policy_plan.deployment_identity.customer_managed_policies : trimprefix(a, "module.deployment_identity.module.github_oidc.aws_iam_policy.")]) &&
      alltrue([for p in flatten(values(m)) : length(p.policy) <= 6144])
    ])
    error_message = "customer managed policy が 6,144 文字を超えているか、長さを測る policy の集合が conftest/plan/decl.json の deployment_identity.customer_managed_policies と一致していません。"
  }
}

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

  assert {
    condition = one([for s in jsondecode(aws_iam_role_policy.this["apply"].policy).Statement : s.Resource if s.Sid == "DenyIdentityChange"]) == [
      "arn:aws-us-gov:iam::210987654321:role/t4bp-dev-ci-*",
      "arn:aws-us-gov:iam::210987654321:policy/t4bp-dev-ci-*",
      aws_iam_openid_connect_provider.github.arn,
    ]
    error_message = "apply の role の DenyIdentityChange の Resource が、partition・アカウント・name_prefix から組んだ CI の role と policy の pattern になっていません。"
  }

  assert {
    condition = [for s in jsondecode(aws_iam_role_policy.apply_identity.policy).Statement : s.Resource if contains(["ReadCiPolicies", "ManageCiPolicies"], s.Sid)] == [
      for _ in range(2) : [
        "arn:aws-us-gov:iam::210987654321:policy/t4bp-dev-ci-apply-identity-manage",
        "arn:aws-us-gov:iam::210987654321:policy/t4bp-dev-ci-apply-identity-governance",
        "arn:aws-us-gov:iam::210987654321:policy/t4bp-dev-ci-apply-workload",
        "arn:aws-us-gov:iam::210987654321:policy/t4bp-dev-ci-apply-workload-read",
        "arn:aws-us-gov:iam::210987654321:policy/t4bp-dev-ci-boundary-ops-notification",
      ]
    ]
    error_message = "apply-identity の role の CI の policy の読み取りと管理の Resource が、apply-identity の role の2つの policy・apply の role の workload の policy・boundary の、partition・アカウント・name_prefix から組んだ ARN になっていません。"
  }

  assert {
    condition = [for s in jsondecode(aws_iam_role_policy.this["plan"].policy).Statement : s.Resource if contains(["ReadWorkloadTopics", "ReadWorkloadKeys", "ReadWorkloadSchedules", "ReadWorkloadRoles", "ReadWorkloadPolicies"], s.Sid)] == [
      ["arn:aws-us-gov:sns:us-gov-west-1:210987654321:t4bp-dev-*"],
      ["arn:aws-us-gov:kms:us-gov-west-1:210987654321:key/*"],
      [
        "arn:aws-us-gov:scheduler:us-gov-west-1:210987654321:schedule-group/t4bp-dev-*",
        "arn:aws-us-gov:scheduler:us-gov-west-1:210987654321:schedule/t4bp-dev-*/*",
      ],
      "arn:aws-us-gov:iam::210987654321:role/t4bp-dev-*",
      "arn:aws-us-gov:iam::210987654321:policy/t4bp-dev-*",
    ]
    error_message = "plan の role の workload の読み取りの Resource が、partition・リージョン・アカウント・name_prefix から組んだ pattern になっていません。"
  }

  assert {
    condition     = one([for s in jsondecode(aws_iam_role_policy.apply_identity.policy).Statement : s.Condition if s.Sid == "AttachApplyWorkloadPolicy"]) == { ArnEquals = { "iam:PolicyARN" = ["arn:aws-us-gov:iam::210987654321:policy/t4bp-dev-ci-apply-workload", "arn:aws-us-gov:iam::210987654321:policy/t4bp-dev-ci-apply-workload-read"] } }
    error_message = "apply-identity が apply の role に付けてよい policy が、apply の role の workload の2つの policy に限られていません。"
  }
  assert {
    condition = { for s in jsondecode(aws_iam_policy.workload.policy).Statement : s.Sid => s.Resource if contains(["ManageWorkloadKeys", "ManageWorkloadKeyAliases", "ManageWorkloadTopics", "ManageChatConfigurations", "ManageBoundedRoles", "ManageWorkloadPolicies", "PassWorkloadRoles", "CreateChatbotServiceLinkedRole"], s.Sid) } == {
      ManageWorkloadKeys             = ["arn:aws-us-gov:kms:us-gov-west-1:210987654321:key/*"]
      ManageWorkloadKeyAliases       = ["arn:aws-us-gov:kms:us-gov-west-1:210987654321:alias/t4bp-dev-*", "arn:aws-us-gov:kms:us-gov-west-1:210987654321:key/*"]
      ManageWorkloadTopics           = ["arn:aws-us-gov:sns:us-gov-west-1:210987654321:t4bp-dev-*"]
      ManageChatConfigurations       = "arn:aws-us-gov:chatbot::210987654321:chat-configuration/slack-channel/t4bp-dev-*"
      ManageBoundedRoles             = ["arn:aws-us-gov:iam::210987654321:role/t4bp-dev-ops-*"]
      ManageWorkloadPolicies         = ["arn:aws-us-gov:iam::210987654321:policy/t4bp-dev-ops-*"]
      PassWorkloadRoles              = ["arn:aws-us-gov:iam::210987654321:role/t4bp-dev-ops-*"]
      CreateChatbotServiceLinkedRole = "arn:aws-us-gov:iam::210987654321:role/aws-service-role/management.chatbot.amazonaws.com/AWSServiceRoleForAWSChatbot"
    }
    error_message = "apply の role の workload の policy の Resource が、partition・リージョン・アカウント・name_prefix から組まれていません。"
  }

  assert {
    condition     = [for s in jsondecode(aws_iam_policy.workload.policy).Statement : s.Condition if s.Sid == "ManageBoundedRoles"] == [{ StringEquals = { "iam:PermissionsBoundary" = ["arn:aws-us-gov:iam::210987654321:policy/t4bp-dev-ci-boundary-ops-notification"] } }]
    error_message = "role の書き込みの boundary の条件が、partition・アカウント・name_prefix から組んだ boundary の ARN になっていません。"
  }

  assert {
    condition = { for s in jsondecode(aws_iam_policy.workload.policy).Statement : s.Sid => [s.Resource, try(s.Condition, null)] if contains(["ManageWorkloadSchedules", "ListWorkloadRoleInstanceProfiles", "AttachWorkloadPolicies", "PassWorkloadRoles", "CreateChatbotServiceLinkedRole"], s.Sid) } == {
      ManageWorkloadSchedules = [[
        "arn:aws-us-gov:scheduler:us-gov-west-1:210987654321:schedule-group/t4bp-dev-*",
        "arn:aws-us-gov:scheduler:us-gov-west-1:210987654321:schedule/t4bp-dev-*/*",
      ], null]
      ListWorkloadRoleInstanceProfiles = ["arn:aws-us-gov:iam::210987654321:role/t4bp-dev-*", null]
      AttachWorkloadPolicies = [["arn:aws-us-gov:iam::210987654321:role/t4bp-dev-ops-*"], {
        StringEquals = { "iam:PermissionsBoundary" = ["arn:aws-us-gov:iam::210987654321:policy/t4bp-dev-ci-boundary-ops-notification"] }
        ArnLike      = { "iam:PolicyARN" = ["arn:aws-us-gov:iam::210987654321:policy/t4bp-dev-ops-*"] }
      }]
      PassWorkloadRoles              = [["arn:aws-us-gov:iam::210987654321:role/t4bp-dev-ops-*"], { StringEquals = { "iam:PassedToService" = ["chatbot.amazonaws.com", "scheduler.amazonaws.com"] } }]
      CreateChatbotServiceLinkedRole = ["arn:aws-us-gov:iam::210987654321:role/aws-service-role/management.chatbot.amazonaws.com/AWSServiceRoleForAWSChatbot", { StringEquals = { "iam:AWSServiceName" = "management.chatbot.amazonaws.com" } }]
    }
    error_message = "apply の role の workload の policy の Resource と条件が、partition・リージョン・アカウント・name_prefix から組まれていません。"
  }

  assert {
    condition     = jsonencode(tolist(aws_iam_role_policy_attachments_exclusive.this["apply"].policy_arns)) == jsonencode(["arn:aws-us-gov:iam::210987654321:policy/t4bp-dev-ci-apply-workload", "arn:aws-us-gov:iam::210987654321:policy/t4bp-dev-ci-apply-workload-read"])
    error_message = "apply の role に付く workload の policy の ARN が、partition・アカウント・name_prefix から組まれていません。"
  }

  assert {
    condition = [for s in jsondecode(aws_iam_policy.boundary["ops-notification"].policy).Statement : [s.Resource, try(s.Condition, null)] if contains(["PublishTopics", "UseKeysViaSns", "ReadAlarms"], s.Sid)] == [
      [["arn:aws-us-gov:sns:us-gov-west-1:210987654321:t4bp-dev-ops-*"], null],
      [["arn:aws-us-gov:kms:us-gov-west-1:210987654321:key/*"], { StringEquals = { "kms:ViaService" = ["sns.us-gov-west-1.amazonaws.com"] } }],
      [["arn:aws-us-gov:cloudwatch:us-gov-west-1:210987654321:alarm:*"], null],
    ]
    error_message = "boundary の Allow の Resource と kms:ViaService が、partition・リージョン・アカウント・name_prefix から組まれていません。"
  }

  assert {
    condition = [
      one([for s in jsondecode(aws_iam_role_policy.this["plan"].policy).Statement : s.Resource if s.Sid == "ReadWorkloadBudgets"]),
      one([for s in jsondecode(aws_iam_policy.workload.policy).Statement : s.Resource if s.Sid == "ManageWorkloadBudgets"]),
    ] == [for _ in range(2) : "arn:aws-us-gov:budgets::210987654321:budget/t4bp-dev-*"]
    error_message = "予算の読み取りと管理の Resource が、partition・アカウント・name_prefix から組まれていません。"
  }

  assert {
    condition = [
      one([for s in jsondecode(aws_iam_role_policy.this["plan"].policy).Statement : s.Resource if s.Sid == "ReadAnomalyDetection"]),
      one([for s in jsondecode(aws_iam_policy.workload.policy).Statement : s.Resource if s.Sid == "ManageAnomalyDetection"]),
      one([for s in jsondecode(aws_iam_role_policy.this["apply"].policy).Statement : s.Resource if s.Sid == "DenyBootstrapAnomalyDetectionChange"]),
    ] == [for _ in range(3) : ["arn:aws-us-gov:ce::210987654321:anomalymonitor/*", "arn:aws-us-gov:ce::210987654321:anomalysubscription/*"]]
    error_message = "anomaly monitor と subscription の読み取り・管理・Deny の Resource が、partition・アカウントから組まれていません。"
  }

  assert {
    condition     = one([for s in jsondecode(aws_iam_role_policy.this["apply"].policy).Statement : s.Condition if s.Sid == "DenyBootstrapAnomalyDetectionChange"]) == { Null = { "aws:ResourceTag/t4bp:bootstrap" = "false" } }
    error_message = "apply の role の異常検知の Deny の tag の key が、name_prefix の project から組まれていません。"
  }

  assert {
    condition     = one([for s in jsondecode(aws_iam_role_policy.this["apply"].policy).Statement : s.Resource if s.Sid == "DenyBreakGlassChange"]) == ["arn:aws-us-gov:iam::210987654321:group/break-glass/*", "arn:aws-us-gov:iam::210987654321:role/break-glass/*", "arn:aws-us-gov:iam::210987654321:user/break-glass/*"]
    error_message = "apply の role の break-glass の Deny の pattern が、partition・アカウントから組まれていません。"
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
      for s in concat(jsondecode(aws_iam_role_policy.apply_identity.policy).Statement, jsondecode(aws_iam_policy.manage.policy).Statement) : s.Resource
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
    error_message = "apply-identity の role の apply の出力の Bucket の作成・読み取り・設定（inline policy と manage の policy）の Resource が、入力の ARN になっていません。"
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
    condition     = [for s in jsondecode(aws_iam_role_policy.this["plan"].policy).Statement : s.Sid] == ["ListBuckets", "ReadBucketConfiguration", "ReadState", "WritePlanOutput", "ReadStateKey", "ReadGitHubProvider", "ReadCiRoles", "ListKeyAliases", "ReadCiPolicies", "ReadWorkloadTopics", "ReadWorkloadKeys", "ReadWorkloadSchedules", "DescribeChatConfigurations", "ReadWorkloadRoles", "ReadWorkloadPolicies", "ReadWorkloadBudgets", "ReadAnomalyDetection"]
    error_message = "dev の plan の role に、統制の構成物の読み取りが入っています。"
  }

  assert {
    condition = [for s in jsondecode(aws_iam_role_policy.apply_identity.policy).Statement : s.Sid] == [
      "ListBuckets",
      "ReadBucketConfiguration",
      "WriteIdentityState",
      "LockIdentityState",
      "ManageCiRoles",
      "ReadStateKey",
      "DenyLockoutSafetyBypass",
      "ReadGitHubProvider",
      "ReadCiRoles",
      "ListKeyAliases",
      "ReadCiPolicies",
      "WriteApplyOutput",
      "ManageCiPolicies",
      "AttachApplyWorkloadPolicy",
      "AttachApplyIdentityPolicies",
    ]
    error_message = "dev の apply-identity の role の Sid が、統制の文を含まない固定の一覧（manage の policy の付与を含む）と一致していません。"
  }

  assert {
    condition     = [for s in jsondecode(aws_iam_policy.manage.policy).Statement : s.Sid] == ["ConfigureBuckets", "CreateApplyOutputBucket", "ManageGitHubProvider", "ManageStateKey"]
    error_message = "dev の apply-identity の role の manage の policy の Sid が、admin と同じ4つの文になっていません。"
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
      "DenyBoundaryRemoval",
      "DenyLockoutSafetyBypass",
      "DenyBootstrapAnomalyDetectionChange",
    ]
    error_message = "dev の apply の role の文が、統制の構成物と break-glass の変更の Deny を含む一覧になっていません。"
  }

  assert {
    condition     = !strcontains(aws_iam_role_policy.this["apply"].policy, "-ci-apply-identity-governance")
    error_message = "dev の apply の role の Deny が、統制の customer managed policy を名指しています。"
  }

  assert {
    condition     = !strcontains(aws_iam_role_policy.this["plan"].policy, "-ci-apply-identity-governance") && !strcontains(aws_iam_role_policy.apply_identity.policy, "-ci-apply-identity-governance")
    error_message = "dev の plan か apply-identity の role の policy が、統制の customer managed policy を名指しています。"
  }

  assert {
    condition = [
      one([for s in jsondecode(aws_iam_role_policy.this["plan"].policy).Statement : s.Resource if s.Sid == "ReadCiPolicies"]),
      one([for s in jsondecode(aws_iam_role_policy.apply_identity.policy).Statement : s.Resource if s.Sid == "ReadCiPolicies"]),
      one([for s in jsondecode(aws_iam_role_policy.apply_identity.policy).Statement : s.Resource if s.Sid == "ManageCiPolicies"]),
      ] == [for _ in range(3) : [
        "arn:aws:iam::123456789012:policy/tabp-dev-ci-apply-identity-manage",
        "arn:aws:iam::123456789012:policy/tabp-dev-ci-apply-workload",
        "arn:aws:iam::123456789012:policy/tabp-dev-ci-apply-workload-read",
        "arn:aws:iam::123456789012:policy/tabp-dev-ci-boundary-ops-notification",
    ]]
    error_message = "dev の CI の policy の読み取りと管理の Resource が、apply-identity の role の manage の policy・apply の role の workload の policy・boundary になっていません。"
  }

  assert {
    condition     = one([for s in jsondecode(aws_iam_role_policy.apply_identity.policy).Statement : s.Condition if s.Sid == "AttachApplyIdentityPolicies"]) == { ArnEquals = { "iam:PolicyARN" = ["arn:aws:iam::123456789012:policy/tabp-dev-ci-apply-identity-manage"] } }
    error_message = "dev で apply-identity が自分の role に付けてよい policy が、manage の policy に限られていません。"
  }

  assert {
    condition     = one([for s in jsondecode(aws_iam_role_policy.this["apply"].policy).Statement : s.Resource if s.Sid == "DenyBreakGlassChange"]) == ["arn:aws:iam::123456789012:group/break-glass/*", "arn:aws:iam::123456789012:role/break-glass/*", "arn:aws:iam::123456789012:user/break-glass/*"]
    error_message = "dev の apply の role の break-glass の Deny が、path が /break-glass/ の user・group・role の pattern になっていません。"
  }

  assert {
    condition     = length(aws_iam_policy.governance) == 0
    error_message = "dev に、統制の customer managed policy が作られています。"
  }

  assert {
    condition     = jsonencode(tolist(aws_iam_role_policy_attachments_exclusive.this["apply_identity"].policy_arns)) == jsonencode(["arn:aws:iam::123456789012:policy/${var.name_prefix}-ci-apply-identity-manage"]) && jsonencode(tolist(aws_iam_role_policy_attachments_exclusive.this["apply"].policy_arns)) == jsonencode(["arn:aws:iam::123456789012:policy/${var.name_prefix}-ci-apply-workload", "arn:aws:iam::123456789012:policy/${var.name_prefix}-ci-apply-workload-read"])
    error_message = "dev の apply-identity の role の managed policy が manage の policy だけでないか、apply の role の managed policy が workload の2つの policy だけになっていません。"
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
    condition     = [for s in jsondecode(aws_iam_role_policy.this["plan"].policy).Statement : s.Sid] == ["ListBuckets", "ReadBucketConfiguration", "ReadState", "WritePlanOutput", "ReadStateKey", "ReadGitHubProvider", "ReadCiRoles", "ListKeyAliases", "ReadCiPolicies", "ReadWorkloadTopics", "ReadWorkloadKeys", "ReadWorkloadSchedules", "DescribeChatConfigurations", "ReadWorkloadRoles", "ReadWorkloadPolicies", "ReadWorkloadBudgets", "ReadAnomalyDetection"]
    error_message = "security の plan の role に、統制の構成物の読み取りが入っています。"
  }

  assert {
    condition = [for s in jsondecode(aws_iam_role_policy.apply_identity.policy).Statement : s.Sid] == [
      "ListBuckets",
      "ReadBucketConfiguration",
      "WriteIdentityState",
      "LockIdentityState",
      "ManageCiRoles",
      "ReadStateKey",
      "DenyLockoutSafetyBypass",
      "ReadGitHubProvider",
      "ReadCiRoles",
      "ListKeyAliases",
      "ReadCiPolicies",
      "WriteApplyOutput",
      "ManageCiPolicies",
      "AttachApplyWorkloadPolicy",
      "AttachApplyIdentityPolicies",
    ]
    error_message = "security の apply-identity の role の Sid が、統制の文を含まない固定の一覧（manage の policy の付与を含む）と一致していません。"
  }

  assert {
    condition     = [for s in jsondecode(aws_iam_policy.manage.policy).Statement : s.Sid] == ["ConfigureBuckets", "CreateApplyOutputBucket", "ManageGitHubProvider", "ManageStateKey"]
    error_message = "security の apply-identity の role の manage の policy の Sid が、admin と同じ4つの文になっていません。"
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
      "DenyBoundaryRemoval",
      "DenyLockoutSafetyBypass",
      "DenyBootstrapAnomalyDetectionChange",
    ]
    error_message = "security の apply の role の文が、統制の構成物と break-glass の変更の Deny を含む一覧になっていません。"
  }

  assert {
    condition     = !strcontains(aws_iam_role_policy.this["apply"].policy, "-ci-apply-identity-governance")
    error_message = "security の apply の role の Deny が、統制の customer managed policy を名指しています。"
  }

  assert {
    condition     = !strcontains(aws_iam_role_policy.this["plan"].policy, "-ci-apply-identity-governance") && !strcontains(aws_iam_role_policy.apply_identity.policy, "-ci-apply-identity-governance")
    error_message = "security の plan か apply-identity の role の policy が、統制の customer managed policy を名指しています。"
  }

  assert {
    condition = [
      one([for s in jsondecode(aws_iam_role_policy.this["plan"].policy).Statement : s.Resource if s.Sid == "ReadCiPolicies"]),
      one([for s in jsondecode(aws_iam_role_policy.apply_identity.policy).Statement : s.Resource if s.Sid == "ReadCiPolicies"]),
      one([for s in jsondecode(aws_iam_role_policy.apply_identity.policy).Statement : s.Resource if s.Sid == "ManageCiPolicies"]),
      ] == [for _ in range(3) : [
        "arn:aws:iam::123456789012:policy/tabp-security-ci-apply-identity-manage",
        "arn:aws:iam::123456789012:policy/tabp-security-ci-apply-workload",
        "arn:aws:iam::123456789012:policy/tabp-security-ci-apply-workload-read",
        "arn:aws:iam::123456789012:policy/tabp-security-ci-boundary-ops-notification",
    ]]
    error_message = "security の CI の policy の読み取りと管理の Resource が、apply-identity の role の manage の policy・apply の role の workload の policy・boundary になっていません。"
  }

  assert {
    condition     = one([for s in jsondecode(aws_iam_role_policy.apply_identity.policy).Statement : s.Condition if s.Sid == "AttachApplyIdentityPolicies"]) == { ArnEquals = { "iam:PolicyARN" = ["arn:aws:iam::123456789012:policy/tabp-security-ci-apply-identity-manage"] } }
    error_message = "security で apply-identity が自分の role に付けてよい policy が、manage の policy に限られていません。"
  }

  assert {
    condition     = one([for s in jsondecode(aws_iam_role_policy.this["apply"].policy).Statement : s.Resource if s.Sid == "DenyBreakGlassChange"]) == ["arn:aws:iam::123456789012:group/break-glass/*", "arn:aws:iam::123456789012:role/break-glass/*", "arn:aws:iam::123456789012:user/break-glass/*"]
    error_message = "security の apply の role の break-glass の Deny が、path が /break-glass/ の user・group・role の pattern になっていません。"
  }

  assert {
    condition     = length(aws_iam_policy.governance) == 0
    error_message = "security に、統制の customer managed policy が作られています。"
  }

  assert {
    condition     = jsonencode(tolist(aws_iam_role_policy_attachments_exclusive.this["apply_identity"].policy_arns)) == jsonencode(["arn:aws:iam::123456789012:policy/${var.name_prefix}-ci-apply-identity-manage"]) && jsonencode(tolist(aws_iam_role_policy_attachments_exclusive.this["apply"].policy_arns)) == jsonencode(["arn:aws:iam::123456789012:policy/${var.name_prefix}-ci-apply-workload", "arn:aws:iam::123456789012:policy/${var.name_prefix}-ci-apply-workload-read"])
    error_message = "security の apply-identity の role の managed policy が manage の policy だけでないか、apply の role の managed policy が workload の2つの policy だけになっていません。"
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
    condition     = [for s in jsondecode(aws_iam_role_policy.this["plan"].policy).Statement : s.Sid] == ["ListBuckets", "ReadBucketConfiguration", "ReadState", "WritePlanOutput", "ReadStateKey", "ReadGitHubProvider", "ReadCiRoles", "ListKeyAliases", "ReadCiPolicies", "ReadWorkloadTopics", "ReadWorkloadKeys", "ReadWorkloadSchedules", "DescribeChatConfigurations", "ReadWorkloadRoles", "ReadWorkloadPolicies", "ReadWorkloadBudgets", "ReadAnomalyDetection"]
    error_message = "log-archive の plan の role に、統制の構成物の読み取りが入っています。"
  }

  assert {
    condition = [for s in jsondecode(aws_iam_role_policy.apply_identity.policy).Statement : s.Sid] == [
      "ListBuckets",
      "ReadBucketConfiguration",
      "WriteIdentityState",
      "LockIdentityState",
      "ManageCiRoles",
      "ReadStateKey",
      "DenyLockoutSafetyBypass",
      "ReadGitHubProvider",
      "ReadCiRoles",
      "ListKeyAliases",
      "ReadCiPolicies",
      "WriteApplyOutput",
      "ManageCiPolicies",
      "AttachApplyWorkloadPolicy",
      "AttachApplyIdentityPolicies",
    ]
    error_message = "log-archive の apply-identity の role の Sid が、統制の文を含まない固定の一覧（manage の policy の付与を含む）と一致していません。"
  }

  assert {
    condition     = [for s in jsondecode(aws_iam_policy.manage.policy).Statement : s.Sid] == ["ConfigureBuckets", "CreateApplyOutputBucket", "ManageGitHubProvider", "ManageStateKey"]
    error_message = "log-archive の apply-identity の role の manage の policy の Sid が、admin と同じ4つの文になっていません。"
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
      "DenyBoundaryRemoval",
      "DenyLockoutSafetyBypass",
      "DenyBootstrapAnomalyDetectionChange",
    ]
    error_message = "log-archive の apply の role の文が、統制の構成物と break-glass の変更の Deny を含む一覧になっていません。"
  }

  assert {
    condition     = !strcontains(aws_iam_role_policy.this["apply"].policy, "-ci-apply-identity-governance")
    error_message = "log-archive の apply の role の Deny が、統制の customer managed policy を名指しています。"
  }

  assert {
    condition     = !strcontains(aws_iam_role_policy.this["plan"].policy, "-ci-apply-identity-governance") && !strcontains(aws_iam_role_policy.apply_identity.policy, "-ci-apply-identity-governance")
    error_message = "log-archive の plan か apply-identity の role の policy が、統制の customer managed policy を名指しています。"
  }

  assert {
    condition = [
      one([for s in jsondecode(aws_iam_role_policy.this["plan"].policy).Statement : s.Resource if s.Sid == "ReadCiPolicies"]),
      one([for s in jsondecode(aws_iam_role_policy.apply_identity.policy).Statement : s.Resource if s.Sid == "ReadCiPolicies"]),
      one([for s in jsondecode(aws_iam_role_policy.apply_identity.policy).Statement : s.Resource if s.Sid == "ManageCiPolicies"]),
      ] == [for _ in range(3) : [
        "arn:aws:iam::123456789012:policy/tabp-log-archive-ci-apply-identity-manage",
        "arn:aws:iam::123456789012:policy/tabp-log-archive-ci-apply-workload",
        "arn:aws:iam::123456789012:policy/tabp-log-archive-ci-apply-workload-read",
        "arn:aws:iam::123456789012:policy/tabp-log-archive-ci-boundary-ops-notification",
    ]]
    error_message = "log-archive の CI の policy の読み取りと管理の Resource が、apply-identity の role の manage の policy・apply の role の workload の policy・boundary になっていません。"
  }

  assert {
    condition     = one([for s in jsondecode(aws_iam_role_policy.apply_identity.policy).Statement : s.Condition if s.Sid == "AttachApplyIdentityPolicies"]) == { ArnEquals = { "iam:PolicyARN" = ["arn:aws:iam::123456789012:policy/tabp-log-archive-ci-apply-identity-manage"] } }
    error_message = "log-archive で apply-identity が自分の role に付けてよい policy が、manage の policy に限られていません。"
  }

  assert {
    condition     = one([for s in jsondecode(aws_iam_role_policy.this["apply"].policy).Statement : s.Resource if s.Sid == "DenyBreakGlassChange"]) == ["arn:aws:iam::123456789012:group/break-glass/*", "arn:aws:iam::123456789012:role/break-glass/*", "arn:aws:iam::123456789012:user/break-glass/*"]
    error_message = "log-archive の apply の role の break-glass の Deny が、path が /break-glass/ の user・group・role の pattern になっていません。"
  }

  assert {
    condition     = length(aws_iam_policy.governance) == 0
    error_message = "log-archive に、統制の customer managed policy が作られています。"
  }

  assert {
    condition     = jsonencode(tolist(aws_iam_role_policy_attachments_exclusive.this["apply_identity"].policy_arns)) == jsonencode(["arn:aws:iam::123456789012:policy/${var.name_prefix}-ci-apply-identity-manage"]) && jsonencode(tolist(aws_iam_role_policy_attachments_exclusive.this["apply"].policy_arns)) == jsonencode(["arn:aws:iam::123456789012:policy/${var.name_prefix}-ci-apply-workload", "arn:aws:iam::123456789012:policy/${var.name_prefix}-ci-apply-workload-read"])
    error_message = "log-archive の apply-identity の role の managed policy が manage の policy だけでないか、apply の role の managed policy が workload の2つの policy だけになっていません。"
  }
}

run "break_glass_in_other_account" {
  command = plan

  variables {
    break_glass_arns = ["arn:aws:iam::210987654321:role/break-glass/tabp-break-glass", "arn:aws:iam::123456789012:user/break-glass/break-glass-1", "arn:aws:iam::123456789012:group/break-glass/break-glass"]
  }

  expect_failures = [aws_iam_role_policy.apply_identity]
}

run "break_glass_outside_path" {
  command = plan

  variables {
    break_glass_arns = ["arn:aws:iam::123456789012:role/tabp-break-glass", "arn:aws:iam::123456789012:user/break-glass/break-glass-1", "arn:aws:iam::123456789012:group/break-glass/break-glass"]
  }

  expect_failures = [aws_iam_role_policy.apply_identity]
}

# 種類ごとに path の内の ARN も含め、管理アカウントの precondition ではなく path の precondition で落ちることを見る。
run "break_glass_user_outside_path" {
  command = plan

  variables {
    break_glass_arns = ["arn:aws:iam::123456789012:role/break-glass/tabp-break-glass", "arn:aws:iam::123456789012:user/break-glass/break-glass-1", "arn:aws:iam::123456789012:user/break-glass-2", "arn:aws:iam::123456789012:group/break-glass/break-glass"]
  }

  expect_failures = [aws_iam_role_policy.apply_identity]
}

run "break_glass_group_outside_path" {
  command = plan

  variables {
    break_glass_arns = ["arn:aws:iam::123456789012:role/break-glass/tabp-break-glass", "arn:aws:iam::123456789012:user/break-glass/break-glass-1", "arn:aws:iam::123456789012:group/break-glass/break-glass", "arn:aws:iam::123456789012:group/break-glass"]
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

# 管理アカウントでも、partition・アカウント・name_prefix を既定と変え、統制の ARN が data source と入力から組まれることを見る。
run "governance_follows_data_sources" {
  command = apply

  variables {
    name_prefix = "t4bp-admin"
    break_glass_arns = [
      "arn:aws-us-gov:iam::210987654321:user/break-glass/break-glass-1",
      "arn:aws-us-gov:iam::210987654321:group/break-glass/break-glass",
      "arn:aws-us-gov:iam::210987654321:role/break-glass/t4bp-break-glass",
    ]
    state_key_arn = "arn:aws-us-gov:kms:us-gov-west-1:210987654321:key/abcdef01-2345-6789-abcd-ef0123456789"
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
    condition     = jsonencode(sort(tolist(aws_iam_role_policy_attachments_exclusive.this["apply_identity"].policy_arns))) == jsonencode(["arn:aws-us-gov:iam::210987654321:policy/t4bp-admin-ci-apply-identity-governance", "arn:aws-us-gov:iam::210987654321:policy/t4bp-admin-ci-apply-identity-manage"])
    error_message = "apply-identity の role に付く manage と統制の policy の ARN が、partition・アカウント・name_prefix から組まれていません。"
  }

  assert {
    condition     = contains(one([for s in jsondecode(aws_iam_role_policy.this["apply"].policy).Statement : s.Resource if s.Sid == "DenyIdentityChange"]), "arn:aws-us-gov:iam::210987654321:policy/t4bp-admin-ci-*") && startswith(aws_iam_policy.governance[0].name, "t4bp-admin-ci-")
    error_message = "apply の role の DenyIdentityChange の CI の policy の pattern が、partition・アカウントから組まれていないか、統制の policy の名前を覆っていません。"
  }

  assert {
    condition = [for s in jsondecode(aws_iam_policy.governance[0].policy).Statement : [s.Resource, try(s.Condition, null)] if contains(["CreateOrganizationsServiceLinkedRole", "ManagePolicyTypes", "AttachBreakGlassAdministratorAccess"], s.Sid)] == [
      ["arn:aws-us-gov:iam::210987654321:role/aws-service-role/organizations.amazonaws.com/AWSServiceRoleForOrganizations", { StringEquals = { "iam:AWSServiceName" = "organizations.amazonaws.com" } }],
      [["arn:aws-us-gov:organizations::210987654321:root/o-*/r-*"], null],
      [["arn:aws-us-gov:iam::210987654321:role/break-glass/t4bp-break-glass"], { ArnEquals = { "iam:PolicyARN" = "arn:aws-us-gov:iam::aws:policy/AdministratorAccess" } }],
    ]
    error_message = "統制の policy の Resource と条件が、partition・アカウントから組まれていません。"
  }

  assert {
    condition     = alltrue([for s in jsondecode(aws_iam_role_policy.apply_identity.policy).Statement : contains(s.Resource, "arn:aws-us-gov:iam::210987654321:policy/t4bp-admin-ci-apply-identity-governance") if contains(["ReadCiPolicies", "ManageCiPolicies"], s.Sid)]) && length([for s in jsondecode(aws_iam_role_policy.apply_identity.policy).Statement : s if contains(["ReadCiPolicies", "ManageCiPolicies"], s.Sid)]) == 2
    error_message = "apply-identity の統制の policy の読み取りと管理の Resource が、partition・アカウント・name_prefix から組まれていません。"
  }

  assert {
    condition     = one([for s in jsondecode(aws_iam_role_policy.apply_identity.policy).Statement : s.Condition if s.Sid == "AttachApplyIdentityPolicies"]) == { ArnEquals = { "iam:PolicyARN" = ["arn:aws-us-gov:iam::210987654321:policy/t4bp-admin-ci-apply-identity-manage", "arn:aws-us-gov:iam::210987654321:policy/t4bp-admin-ci-apply-identity-governance"] } }
    error_message = "apply-identity が自分の role に付けてよい policy の ARN が、partition・アカウント・name_prefix から組んだ manage と統制の policy になっていません。"
  }

  assert {
    condition = [for s in jsondecode(aws_iam_policy.governance[0].policy).Statement : [s.Resource, try(s.Condition, null)] if contains(["UpdateBreakGlassStackSet", "ManageBreakGlassStackInstances"], s.Sid)] == [
      for c in [null, { StringEquals = { "cloudformation:TargetRegion" = "us-gov-west-1" } }] : [[
        "arn:aws-us-gov:cloudformation:us-gov-west-1:210987654321:stackset/t4bp-break-glass:*",
        "arn:aws-us-gov:cloudformation:us-gov-west-1:210987654321:stackset-target/t4bp-break-glass:*",
        "arn:aws-us-gov:cloudformation:us-gov-west-1:*:type/resource/AWS-IAM-Role",
      ], c]
    ]
    error_message = "break-glass の StackSet の書き込みの Resource と条件が、partition・region・アカウントと role の名前から組まれていません。"
  }

  assert {
    condition     = alltrue([for p in [aws_iam_role_policy.this["plan"].policy, aws_iam_role_policy.apply_identity.policy] : one([for s in jsondecode(p).Statement : s.Resource if s.Sid == "ReadBreakGlassStackSet"]) == ["arn:aws-us-gov:cloudformation:us-gov-west-1:210987654321:stackset/t4bp-break-glass:*"]])
    error_message = "plan と apply-identity の break-glass の StackSet の読み取りの Resource が、partition・region・アカウントと role の名前から組まれていません。"
  }
}

# us-east-1 は provider の region と重ねず、aws の partition の外では足さない（repository-wide/ADR-0208 決定4・5）。
run "workload_regions_in_us_east_1" {
  command   = apply
  state_key = "workload_regions_in_us_east_1"

  variables {
    state_key_arn = "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  }
  override_data {
    target = data.aws_region.current
    values = { region = "us-east-1" }
  }

  assert {
    condition = [for st in jsondecode(aws_iam_policy.boundary["ops-notification"].policy).Statement : [st.Resource, try(st.Condition, null)] if contains(["PublishTopics", "UseKeysViaSns"], st.Sid)] == [
      [["arn:aws:sns:us-east-1:123456789012:tabp-admin-ops-*"], null],
      [["arn:aws:kms:us-east-1:123456789012:key/*"], { StringEquals = { "kms:ViaService" = ["sns.us-east-1.amazonaws.com"] } }],
    ]
    error_message = "provider の region が us-east-1 のとき、boundary の Resource と kms:ViaService が us-east-1 の1つになっていません。"
  }
}

run "workload_regions_in_aws_cn" {
  command   = apply
  state_key = "workload_regions_in_aws_cn"

  variables {
    state_bucket_arn        = "arn:aws-cn:s3:::tabp-admin-tfstate-123456789012-cn-north-1-an"
    state_key_arn           = "arn:aws-cn:kms:cn-north-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
    plan_output_bucket_arn  = "arn:aws-cn:s3:::tabp-admin-tfplan-123456789012-cn-north-1-an"
    apply_output_bucket_arn = "arn:aws-cn:s3:::tabp-admin-tfapply-123456789012-cn-north-1-an"
    break_glass_arns        = ["arn:aws-cn:iam::123456789012:user/break-glass/break-glass-1", "arn:aws-cn:iam::123456789012:group/break-glass/break-glass", "arn:aws-cn:iam::123456789012:role/break-glass/tabp-break-glass"]
  }
  override_data {
    target = data.aws_partition.current
    values = { partition = "aws-cn" }
  }
  override_data {
    target = data.aws_region.current
    values = { region = "cn-north-1" }
  }
  override_resource {
    target = aws_iam_openid_connect_provider.github
    values = { arn = "arn:aws-cn:iam::123456789012:oidc-provider/token.actions.githubusercontent.com" }
  }

  assert {
    condition     = one([for st in jsondecode(aws_iam_policy.workload_read.policy).Statement : st.Resource if st.Sid == "ReadWorkloadTopics"]) == ["arn:aws-cn:sns:cn-north-1:123456789012:tabp-admin-*"]
    error_message = "aws の外の partition で、workload の Resource に us-east-1 が足されています。"
  }

  assert {
    condition = [for st in jsondecode(aws_iam_policy.workload_read.policy).Statement : st.Resource if contains(["ReadWorkloadBudgets", "ReadAnomalyDetection"], st.Sid)] == [
      "arn:aws-cn:budgets::123456789012:budget/tabp-admin-*",
      ["arn:aws-cn:ce::123456789012:anomalymonitor/*", "arn:aws-cn:ce::123456789012:anomalysubscription/*"],
    ]
    error_message = "Budgets と Cost Explorer の Resource が、data source の partition から組まれていません。"
  }
}

run "workload_regions_in_aws_iso_b" {
  command   = apply
  state_key = "workload_regions_in_aws_iso_b"

  variables {
    state_bucket_arn        = "arn:aws-iso-b:s3:::tabp-admin-tfstate-123456789012-us-isob-east-1-an"
    state_key_arn           = "arn:aws-iso-b:kms:us-isob-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
    plan_output_bucket_arn  = "arn:aws-iso-b:s3:::tabp-admin-tfplan-123456789012-us-isob-east-1-an"
    apply_output_bucket_arn = "arn:aws-iso-b:s3:::tabp-admin-tfapply-123456789012-us-isob-east-1-an"
    break_glass_arns        = ["arn:aws-iso-b:iam::123456789012:user/break-glass/break-glass-1", "arn:aws-iso-b:iam::123456789012:group/break-glass/break-glass", "arn:aws-iso-b:iam::123456789012:role/break-glass/tabp-break-glass"]
  }
  override_data {
    target = data.aws_partition.current
    values = { partition = "aws-iso-b" }
  }
  override_data {
    target = data.aws_region.current
    values = { region = "us-isob-east-1" }
  }
  override_resource {
    target = aws_iam_openid_connect_provider.github
    values = { arn = "arn:aws-iso-b:iam::123456789012:oidc-provider/token.actions.githubusercontent.com" }
  }

  assert {
    condition     = one([for st in jsondecode(aws_iam_policy.workload_read.policy).Statement : st.Resource if st.Sid == "ReadWorkloadTopics"]) == ["arn:aws-iso-b:sns:us-isob-east-1:123456789012:tabp-admin-*"]
    error_message = "aws の外の partition で、workload の Resource に us-east-1 が足されています。"
  }

  assert {
    condition = [for st in jsondecode(aws_iam_policy.workload_read.policy).Statement : st.Resource if contains(["ReadWorkloadBudgets", "ReadAnomalyDetection"], st.Sid)] == [
      "arn:aws-iso-b:budgets::123456789012:budget/tabp-admin-*",
      ["arn:aws-iso-b:ce::123456789012:anomalymonitor/*", "arn:aws-iso-b:ce::123456789012:anomalysubscription/*"],
    ]
    error_message = "Budgets と Cost Explorer の Resource が、data source の partition から組まれていません。"
  }
}

run "rejects_empty_name_prefix" {
  command = plan

  variables {
    name_prefix = ""
  }

  expect_failures = [var.name_prefix]
}

run "rejects_name_prefix_without_env" {
  command = plan

  variables {
    name_prefix = "tabp"
  }

  expect_failures = [var.name_prefix]
}

run "rejects_name_prefix_with_empty_env" {
  command = plan

  variables {
    name_prefix = "tabp-"
  }

  expect_failures = [var.name_prefix]
}

run "rejects_name_prefix_with_uppercase_project" {
  command = plan

  variables {
    name_prefix = "TABP-dev"
  }

  expect_failures = [var.name_prefix]
}
