# Action の一覧の根拠（Provider の各 resource が呼ぶ API）は usecases/deployment-identity/docs/adr/0201 が持つ。
locals {
  identity_state_key = "${var.state_bucket_arn}/identity/terraform.tfstate"
  workload_state_key = "${var.state_bucket_arn}/workload/terraform.tfstate"
  # pr/ は Pull Request の plan、branch/ は保護ブランチの plan（deployment-identity/ADR-0201 決定8）。
  plan_output_keys = ["${var.plan_output_bucket_arn}/pr/*", "${var.plan_output_bucket_arn}/branch/*"]

  state_key_read_actions = ["kms:DescribeKey", "kms:GetKeyPolicy", "kms:GetKeyRotationStatus", "kms:ListResourceTags"]
  # deployment-identity/ADR-0201 決定5。
  state_key_administration_actions = [
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
  state_key_arn_prefix = "arn:${data.aws_partition.current.partition}:kms:${data.aws_region.current.region}:${data.aws_caller_identity.current.account_id}:key/"

  identity_role_arns = [for k in keys(local.roles) : local.role_arns[k]]
  identity_arns      = concat(local.identity_role_arns, [aws_iam_openid_connect_provider.github.arn])

  bucket_read_actions = [
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

  bucket_configure_actions = [
    "s3:DeleteBucketPolicy",
    "s3:PutBucketOwnershipControls",
    "s3:PutBucketPolicy",
    "s3:PutBucketPublicAccessBlock",
    "s3:PutBucketVersioning",
    "s3:PutEncryptionConfiguration",
  ]

  # 3つの Bucket に同じ Action を与える文は、Resource を3つにした1つの文にする（inline policy の長さ）。
  bucket_arns = [var.state_bucket_arn, var.plan_output_bucket_arn, var.apply_output_bucket_arn]

  bucket_read_statements = [
    { Sid = "ListBuckets", Effect = "Allow", Action = "s3:ListBucket", Resource = local.bucket_arns },
    { Sid = "ReadBucketConfiguration", Effect = "Allow", Action = local.bucket_read_actions, Resource = local.bucket_arns },
  ]

  identity_read_statements = [
    {
      Sid    = "ReadGitHubProvider"
      Effect = "Allow"
      Action = [
        "iam:GetOpenIDConnectProvider",
        "iam:ListOpenIDConnectProviderTags",
      ]
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
      Resource = local.identity_role_arns
    },
    { Sid = "ListKeyAliases", Effect = "Allow", Action = "kms:ListAliases", Resource = "*" },
  ]

  # 統制の構成物（organization-baseline）は管理アカウントの identity root だけが持つ（deployment-identity/ADR-0201 決定3）。
  management = var.env == "admin"

  # 組織の ID は作る前に決まらないので、Organizations の ARN は pattern で書く（deployment-identity/ADR-0201 決定4）。
  org_arn_prefix = "arn:${data.aws_partition.current.partition}:organizations::${data.aws_caller_identity.current.account_id}"
  org_root_arns  = ["${local.org_arn_prefix}:root/o-*/r-*"]
  org_ou_arns    = ["${local.org_arn_prefix}:ou/o-*/ou-*"]
  org_policy_arn = "${local.org_arn_prefix}:policy/o-*/*/p-*"

  break_glass_user_arns  = [for a in var.break_glass_arns : a if strcontains(a, ":user/")]
  break_glass_group_arns = [for a in var.break_glass_arns : a if strcontains(a, ":group/")]
  break_glass_role_arns  = [for a in var.break_glass_arns : a if strcontains(a, ":role/")]
  administrator_access   = "arn:${data.aws_partition.current.partition}:iam::aws:policy/AdministratorAccess"

  governance_policy_arn  = "arn:${data.aws_partition.current.partition}:iam::${data.aws_caller_identity.current.account_id}:policy/${var.name_prefix}-ci-apply-identity-governance"
  governance_policy_arns = [for a in [local.governance_policy_arn] : a if local.management]

  # Resource "*" でしか与えられない Action は、Action ごとの別の文にする（deployment-identity/ADR-0201 決定1）。
  governance_wildcard_read_actions = [
    "iam:GetAccountPasswordPolicy",
    "iam:ListOrganizationsFeatures",
    "organizations:DescribeOrganization",
    "organizations:ListAWSServiceAccessForOrganization",
    "organizations:ListAccounts",
    "organizations:ListRoots",
  ]
  governance_read_statements = [for s in concat(
    [for a in local.governance_wildcard_read_actions : { Sid = replace(title(replace(a, ":", " ")), " ", ""), Effect = "Allow", Action = a, Resource = "*" }],
    [
      {
        Sid      = "ReadOrganizationalUnits"
        Effect   = "Allow"
        Action   = ["organizations:DescribeOrganizationalUnit", "organizations:ListAccountsForParent", "organizations:ListParents", "organizations:ListTagsForResource"]
        Resource = local.org_ou_arns
      },
      {
        Sid      = "ReadOrganizationPolicies"
        Effect   = "Allow"
        Action   = ["organizations:DescribePolicy", "organizations:ListTagsForResource", "organizations:ListTargetsForPolicy"]
        Resource = local.org_policy_arn
      },
      { Sid = "ReadBreakGlassUsers", Effect = "Allow", Action = ["iam:GetUser", "iam:ListUserTags"], Resource = local.break_glass_user_arns },
      { Sid = "ReadBreakGlassGroup", Effect = "Allow", Action = ["iam:GetGroup", "iam:GetGroupPolicy"], Resource = local.break_glass_group_arns },
      {
        Sid      = "ReadBreakGlassRole"
        Effect   = "Allow"
        Action   = ["iam:GetRole", "iam:GetRolePolicy", "iam:ListAttachedRolePolicies", "iam:ListRolePolicies", "iam:ListRoleTags"]
        Resource = local.break_glass_role_arns
      },
      {
        Sid      = "ReadGovernancePolicy"
        Effect   = "Allow"
        Action   = ["iam:GetPolicy", "iam:GetPolicyVersion", "iam:ListPolicyTags", "iam:ListPolicyVersions"]
        Resource = local.governance_policy_arn
      },
    ],
  ) : s if local.management]

  governance_wildcard_write_actions = [
    "iam:DeleteAccountPasswordPolicy",
    "iam:DisableOrganizationsRootCredentialsManagement",
    "iam:DisableOrganizationsRootSessions",
    "iam:EnableOrganizationsRootCredentialsManagement",
    "iam:EnableOrganizationsRootSessions",
    "iam:UpdateAccountPasswordPolicy",
    "organizations:CreateOrganization",
  ]

  # 組織の削除・メンバーの閉鎖・組織からの除外と、prevent_destroy を持つ構成物の削除は与えない（deployment-identity/ADR-0201 決定3）。
  governance_manage_statements = concat(
    [for a in local.governance_wildcard_write_actions : { Sid = replace(title(replace(a, ":", " ")), " ", ""), Effect = "Allow", Action = a, Resource = "*" }],
    [for a in ["organizations:DisableAWSServiceAccess", "organizations:EnableAWSServiceAccess"] : {
      Sid       = replace(title(replace(a, ":", " ")), " ", "")
      Effect    = "Allow"
      Action    = a
      Resource  = "*"
      Condition = { StringEquals = { "organizations:ServicePrincipal" = "iam.amazonaws.com" } }
    }],
    [
      {
        Sid       = "CreateOrganizationsServiceLinkedRole"
        Effect    = "Allow"
        Action    = "iam:CreateServiceLinkedRole"
        Resource  = "arn:${data.aws_partition.current.partition}:iam::${data.aws_caller_identity.current.account_id}:role/aws-service-role/organizations.amazonaws.com/AWSServiceRoleForOrganizations"
        Condition = { StringEquals = { "iam:AWSServiceName" = "organizations.amazonaws.com" } }
      },
      { Sid = "ManagePolicyTypes", Effect = "Allow", Action = ["organizations:DisablePolicyType", "organizations:EnablePolicyType"], Resource = local.org_root_arns },
      { Sid = "CreateOrganizationalUnits", Effect = "Allow", Action = "organizations:CreateOrganizationalUnit", Resource = concat(local.org_root_arns, local.org_ou_arns) },
      { Sid = "UpdateOrganizationalUnits", Effect = "Allow", Action = "organizations:UpdateOrganizationalUnit", Resource = local.org_ou_arns },
      {
        Sid      = "ManageOrganizationPolicies"
        Effect   = "Allow"
        Action   = ["organizations:CreatePolicy", "organizations:DeletePolicy", "organizations:UpdatePolicy"]
        Resource = local.org_policy_arn
      },
      {
        Sid      = "AttachOrganizationPolicies"
        Effect   = "Allow"
        Action   = ["organizations:AttachPolicy", "organizations:DetachPolicy"]
        Resource = concat(local.org_root_arns, local.org_ou_arns, [local.org_policy_arn])
      },
      { Sid = "ManageBreakGlassUsers", Effect = "Allow", Action = ["iam:CreateUser", "iam:UpdateUser"], Resource = local.break_glass_user_arns },
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
        Resource = local.break_glass_group_arns
      },
      {
        Sid      = "ManageBreakGlassRole"
        Effect   = "Allow"
        Action   = ["iam:CreateRole", "iam:UpdateAssumeRolePolicy", "iam:UpdateRole", "iam:UpdateRoleDescription"]
        Resource = local.break_glass_role_arns
      },
      {
        Sid       = "AttachBreakGlassAdministratorAccess"
        Effect    = "Allow"
        Action    = ["iam:AttachRolePolicy", "iam:DetachRolePolicy"]
        Resource  = local.break_glass_role_arns
        Condition = { ArnEquals = { "iam:PolicyARN" = local.administrator_access } }
      },
    ],
  )

  governance_policy_management_statements = [for s in [
    {
      Sid      = "ManageGovernancePolicy"
      Effect   = "Allow"
      Action   = ["iam:CreatePolicy", "iam:CreatePolicyVersion", "iam:DeletePolicy", "iam:DeletePolicyVersion", "iam:SetDefaultPolicyVersion"]
      Resource = local.governance_policy_arn
    },
    {
      Sid       = "AttachGovernancePolicy"
      Effect    = "Allow"
      Action    = "iam:AttachRolePolicy"
      Resource  = local.role_arns.apply_identity
      Condition = { ArnEquals = { "iam:PolicyARN" = local.governance_policy_arn } }
    },
  ] : s if local.management]

  # apply の role の統制の Deny（deployment-identity/ADR-0201 決定5）。Resource "*" で、条件を付けない。
  governance_change_actions = concat(
    [for v in ["Accept", "Attach", "Cancel", "Close", "Create", "Decline", "Delete", "Deregister", "Detach", "Disable", "Enable", "Invite", "Leave", "Move", "Put", "Register", "Remove", "Tag", "Terminate", "Untag", "Update"] : "organizations:${v}*"],
    ["account:CloseAccount"],
    ["iam:DeleteAccountPasswordPolicy", "iam:DisableOrganizationsRoot*", "iam:EnableOrganizationsRoot*", "iam:UpdateAccountPasswordPolicy"],
    [for a in [
      "AttachCustomerManagedPolicyReferenceToPermissionSet",
      "AttachManagedPolicyToPermissionSet",
      "CreateAccountAssignment",
      "CreatePermissionSet",
      "DeleteAccountAssignment",
      "DeleteInlinePolicyFromPermissionSet",
      "DeletePermissionSet",
      "DeletePermissionsBoundaryFromPermissionSet",
      "DetachCustomerManagedPolicyReferenceFromPermissionSet",
      "DetachManagedPolicyFromPermissionSet",
      "ProvisionPermissionSet",
      "PutInlinePolicyToPermissionSet",
      "PutPermissionsBoundaryToPermissionSet",
      "UpdatePermissionSet",
    ] : "sso:${a}"],
    ["sts:AssumeRoot"],
  )

  statements = {
    plan = concat(
      local.bucket_read_statements,
      [
        { Sid = "ReadState", Effect = "Allow", Action = "s3:GetObject", Resource = [local.identity_state_key, local.workload_state_key] },
        { Sid = "WritePlanOutput", Effect = "Allow", Action = "s3:PutObject", Resource = local.plan_output_keys },
        { Sid = "ReadStateKey", Effect = "Allow", Action = local.state_key_read_actions, Resource = var.state_key_arn },
      ],
      local.identity_read_statements,
      local.governance_read_statements,
    )

    apply = [
      { Sid = "ListStateBucket", Effect = "Allow", Action = "s3:ListBucket", Resource = var.state_bucket_arn },
      { Sid = "ReadStateBucketEncryption", Effect = "Allow", Action = "s3:GetEncryptionConfiguration", Resource = var.state_bucket_arn },
      { Sid = "WriteWorkloadState", Effect = "Allow", Action = ["s3:GetObject", "s3:PutObject"], Resource = local.workload_state_key },
      { Sid = "LockWorkloadState", Effect = "Allow", Action = ["s3:DeleteObject", "s3:GetObject", "s3:PutObject"], Resource = "${local.workload_state_key}.tflock" },
      { Sid = "ListKeyAliases", Effect = "Allow", Action = "kms:ListAliases", Resource = "*" },
      { Sid = "WriteWorkloadApplyOutput", Effect = "Allow", Action = "s3:PutObject", Resource = "${var.apply_output_bucket_arn}/*" },
      # Deny の範囲と形は deployment-identity/ADR-0201 決定5。
      { Sid = "DenyIdentityChange", Effect = "Deny", Action = "*", Resource = concat(local.identity_arns, local.governance_policy_arns) },
      { Sid = "DenyIdentityStateChange", Effect = "Deny", Action = "*", Resource = [local.identity_state_key, "${local.identity_state_key}.tflock"] },
      { Sid = "DenyStateBucketChange", Effect = "Deny", NotAction = ["s3:GetEncryptionConfiguration", "s3:ListBucket"], Resource = var.state_bucket_arn },
      { Sid = "DenyStateVersionDeletion", Effect = "Deny", Action = "s3:DeleteObjectVersion", Resource = "${var.state_bucket_arn}/*" },
      { Sid = "DenyPlanOutputChange", Effect = "Deny", Action = "*", Resource = [var.plan_output_bucket_arn, "${var.plan_output_bucket_arn}/*"] },
      { Sid = "DenyApplyOutputBucketChange", Effect = "Deny", Action = "*", Resource = var.apply_output_bucket_arn },
      { Sid = "DenyApplyOutputChange", Effect = "Deny", NotAction = ["s3:PutObject"], Resource = "${var.apply_output_bucket_arn}/*" },
      # kms:* にすると、Key Policy が role を名指して許す暗号操作まで塞ぐ。
      { Sid = "DenyStateKeyChange", Effect = "Deny", Action = local.state_key_administration_actions, Resource = var.state_key_arn },
      { Sid = "DenyGovernanceChange", Effect = "Deny", Action = local.governance_change_actions, Resource = "*" },
      { Sid = "DenyBreakGlassChange", Effect = "Deny", Action = "*", Resource = var.break_glass_arns },
    ]

    apply_identity = concat(
      local.bucket_read_statements,
      [
        { Sid = "WriteIdentityState", Effect = "Allow", Action = ["s3:GetObject", "s3:PutObject"], Resource = local.identity_state_key },
        { Sid = "LockIdentityState", Effect = "Allow", Action = ["s3:DeleteObject", "s3:GetObject", "s3:PutObject"], Resource = "${local.identity_state_key}.tflock" },
        # s3:PutLifecycleConfiguration は DeleteBucketLifecycle の権限も兼ねる。
        { Sid = "ConfigureBuckets", Effect = "Allow", Action = concat(local.bucket_configure_actions, ["s3:PutLifecycleConfiguration"]), Resource = local.bucket_arns },
        { Sid = "CreateApplyOutputBucket", Effect = "Allow", Action = "s3:CreateBucket", Resource = var.apply_output_bucket_arn },
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
          Resource = local.identity_role_arns
        },
        # 鍵と alias を作る権限は与えない（deployment-identity/ADR-0201 決定3）。
        { Sid = "ReadStateKey", Effect = "Allow", Action = local.state_key_read_actions, Resource = var.state_key_arn },
        { Sid = "ManageStateKey", Effect = "Allow", Action = ["kms:EnableKeyRotation", "kms:PutKeyPolicy"], Resource = var.state_key_arn },
        { Sid = "DenyLockoutSafetyBypass", Effect = "Deny", Action = "kms:PutKeyPolicy", Resource = "*", Condition = { Bool = { "kms:BypassPolicyLockoutSafetyCheck" = "true" } } },
      ],
      local.identity_read_statements,
      [{ Sid = "WriteApplyOutput", Effect = "Allow", Action = "s3:PutObject", Resource = "${var.plan_output_bucket_arn}/apply/*" }],
      local.governance_read_statements,
      local.governance_policy_management_statements,
    )

    # 範囲と、各 Action が要る理由は deployment-identity/ADR-0201 決定9（unlock は決定11）。
    unlock_identity = [
      { Sid = "ListIdentityLock", Effect = "Allow", Action = "s3:ListBucket", Resource = var.state_bucket_arn, Condition = { StringEquals = { "s3:prefix" = "identity/terraform.tfstate.tflock" } } },
      { Sid = "RemoveIdentityLock", Effect = "Allow", Action = ["s3:DeleteObject", "s3:GetObject"], Resource = "${local.identity_state_key}.tflock" },
    ]

    unlock = [
      { Sid = "ListWorkloadLock", Effect = "Allow", Action = "s3:ListBucket", Resource = var.state_bucket_arn, Condition = { StringEquals = { "s3:prefix" = "workload/terraform.tfstate.tflock" } } },
      { Sid = "RemoveWorkloadLock", Effect = "Allow", Action = ["s3:DeleteObject", "s3:GetObject"], Resource = "${local.workload_state_key}.tflock" },
    ]
  }
}

resource "aws_iam_role_policy" "apply_identity" {
  name = "apply_identity"
  role = aws_iam_role.apply_identity.name
  policy = jsonencode({
    Version   = "2012-10-17"
    Statement = local.statements.apply_identity
  })

  lifecycle {
    # 別のアカウントやリージョンの鍵を渡すと、apply-identity は自分の state の鍵を管理できなくなる。他の role は
    # この policy の後に作るので、ここで落ちれば他の role の policy にも届かない。
    precondition {
      condition     = startswith(var.state_key_arn, local.state_key_arn_prefix)
      error_message = "state_key_arn が、role と同じ partition・リージョン・アカウントの鍵ではありません。"
    }

    # 空か別のアカウントの ARN では、apply の role の Deny が自分の break-glass に効かない。
    precondition {
      condition     = length(var.break_glass_arns) > 0 && alltrue([for a in var.break_glass_arns : startswith(a, "arn:${data.aws_partition.current.partition}:iam::${data.aws_caller_identity.current.account_id}:")])
      error_message = "break_glass_arns が空か、role と同じ partition・アカウントの ARN ではありません。"
    }

    # 管理アカウントでは種類ごとの文の Resource になる。空の Resource は IAM が拒む。
    precondition {
      condition     = !local.management || (length(local.break_glass_user_arns) > 0 && length(local.break_glass_group_arns) > 0 && length(local.break_glass_role_arns) > 0)
      error_message = "管理アカウント（env が admin）の break_glass_arns は、break-glass の IAM user・group・role をそれぞれ1つ以上含めてください。"
    }
  }
}

moved {
  from = aws_iam_role_policy.this["apply_identity"]
  to   = aws_iam_role_policy.apply_identity
}

resource "aws_iam_role_policy" "this" {
  for_each = local.ci_roles

  name = each.key
  role = aws_iam_role.this[each.key].name
  policy = jsonencode({
    Version   = "2012-10-17"
    Statement = local.statements[each.key]
  })
}
