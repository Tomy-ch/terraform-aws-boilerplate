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

  iam_arn_prefix = "arn:${data.aws_partition.current.partition}:iam::${data.aws_caller_identity.current.account_id}"
  # CI の role と customer managed policy は名前を `<name_prefix>-ci-` で始める（deployment-identity/ADR-0201 決定5）。
  ci_arn_patterns = ["${local.iam_arn_prefix}:role/${var.name_prefix}-ci-*", "${local.iam_arn_prefix}:policy/${var.name_prefix}-ci-*"]

  # apply の role の workload の権限（決定17）と、role を作る usecase ごとの permissions boundary（決定15）。
  workload_policy_name  = "${var.name_prefix}-ci-apply-workload"
  workload_policy_arn   = "${local.iam_arn_prefix}:policy/${local.workload_policy_name}"
  boundary_policy_names = { ops_notification = "${var.name_prefix}-ci-boundary-ops-notification" }
  boundary_policy_arns  = { for k, n in local.boundary_policy_names : k => "${local.iam_arn_prefix}:policy/${n}" }

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
    {
      Sid      = "ReadCiPolicies"
      Effect   = "Allow"
      Action   = ["iam:GetPolicy", "iam:GetPolicyVersion", "iam:ListPolicyTags", "iam:ListPolicyVersions"]
      Resource = local.ci_policy_arns
    },
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

  governance_policy_name = "${var.name_prefix}-ci-apply-identity-governance"
  governance_policy_arn  = "arn:${data.aws_partition.current.partition}:iam::${data.aws_caller_identity.current.account_id}:policy/${local.governance_policy_name}"
  governance_policy_arns = [for a in [local.governance_policy_arn] : a if local.management]
  ci_policy_arns         = concat(local.governance_policy_arns, [local.workload_policy_arn], values(local.boundary_policy_arns))

  # workload root の usecase の構成物の読み取り（決定13）。名前を `<name_prefix>-` で始める構成物の pattern で限る。
  regional_arn_suffix = "${data.aws_region.current.region}:${data.aws_caller_identity.current.account_id}"
  workload_key_arns   = "arn:${data.aws_partition.current.partition}:kms:${local.regional_arn_suffix}:key/*"
  workload_key_conditions = {
    StringEquals = { "kms:KeySpec" = "SYMMETRIC_DEFAULT", "kms:KeyUsage" = "ENCRYPT_DECRYPT", "kms:KeyOrigin" = "AWS_KMS" }
    Bool         = { "kms:MultiRegion" = "false" }
  }
  workload_read_statements = [
    {
      Sid      = "ReadWorkloadTopics"
      Effect   = "Allow"
      Action   = ["sns:GetTopicAttributes", "sns:ListTagsForResource"]
      Resource = "arn:${data.aws_partition.current.partition}:sns:${local.regional_arn_suffix}:${var.name_prefix}-*"
    },
    {
      Sid       = "ReadWorkloadKeys"
      Effect    = "Allow"
      Action    = ["kms:DescribeKey", "kms:GetKeyPolicy", "kms:GetKeyRotationStatus", "kms:ListResourceTags"]
      Resource  = local.workload_key_arns
      Condition = local.workload_key_conditions
    },
    {
      Sid    = "ReadWorkloadSchedules"
      Effect = "Allow"
      Action = ["scheduler:GetSchedule", "scheduler:GetScheduleGroup", "scheduler:ListTagsForResource"]
      Resource = [
        "arn:${data.aws_partition.current.partition}:scheduler:${local.regional_arn_suffix}:schedule-group/${var.name_prefix}-*",
        "arn:${data.aws_partition.current.partition}:scheduler:${local.regional_arn_suffix}:schedule/${var.name_prefix}-*/*",
      ]
    },
    { Sid = "DescribeChatConfigurations", Effect = "Allow", Action = "chatbot:DescribeSlackChannelConfigurations", Resource = "*" },
    {
      Sid      = "ReadWorkloadRoles"
      Effect   = "Allow"
      Action   = ["iam:GetRole", "iam:GetRolePolicy", "iam:ListAttachedRolePolicies", "iam:ListRolePolicies"]
      Resource = "${local.iam_arn_prefix}:role/${var.name_prefix}-*"
    },
    {
      Sid      = "ReadWorkloadPolicies"
      Effect   = "Allow"
      Action   = ["iam:GetPolicy", "iam:GetPolicyVersion"]
      Resource = "${local.iam_arn_prefix}:policy/${var.name_prefix}-*"
    },
  ]

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

  ci_policy_management_statements = concat([
    {
      Sid      = "ManageCiPolicies"
      Effect   = "Allow"
      Action   = ["iam:CreatePolicy", "iam:CreatePolicyVersion", "iam:DeletePolicy", "iam:DeletePolicyVersion", "iam:SetDefaultPolicyVersion"]
      Resource = local.ci_policy_arns
    },
    {
      Sid       = "AttachApplyWorkloadPolicy"
      Effect    = "Allow"
      Action    = "iam:AttachRolePolicy"
      Resource  = local.role_arns.apply
      Condition = { ArnEquals = { "iam:PolicyARN" = local.workload_policy_arn } }
    },
    ], [for s in [
      {
        Sid       = "AttachGovernancePolicy"
        Effect    = "Allow"
        Action    = "iam:AttachRolePolicy"
        Resource  = local.role_arns.apply_identity
        Condition = { ArnEquals = { "iam:PolicyARN" = local.governance_policy_arn } }
      },
  ] : s if local.management])

  # 決定1。apply-identity と apply の role に同じ文で置く。
  lockout_bypass_deny_statement = { Sid = "DenyLockoutSafetyBypass", Effect = "Deny", Action = ["kms:CreateKey", "kms:PutKeyPolicy"], Resource = "*", Condition = { Bool = { "kms:BypassPolicyLockoutSafetyCheck" = "true" } } }

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

  # apply の role の Deny（deployment-identity/ADR-0201 決定5）。
  bootstrap_deny_statements = [
    { Sid = "DenyIdentityChange", Effect = "Deny", Action = "*", Resource = concat(local.ci_arn_patterns, [aws_iam_openid_connect_provider.github.arn]) },
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

  statements = {
    plan = concat(
      local.bucket_read_statements,
      [
        { Sid = "ReadState", Effect = "Allow", Action = "s3:GetObject", Resource = [local.identity_state_key, local.workload_state_key] },
        { Sid = "WritePlanOutput", Effect = "Allow", Action = "s3:PutObject", Resource = local.plan_output_keys },
        { Sid = "ReadStateKey", Effect = "Allow", Action = local.state_key_read_actions, Resource = var.state_key_arn },
      ],
      local.identity_read_statements,
      local.workload_read_statements,
      local.governance_read_statements,
    )

    apply = concat([
      { Sid = "ListStateBucket", Effect = "Allow", Action = "s3:ListBucket", Resource = var.state_bucket_arn },
      { Sid = "ReadStateBucketEncryption", Effect = "Allow", Action = "s3:GetEncryptionConfiguration", Resource = var.state_bucket_arn },
      { Sid = "WriteWorkloadState", Effect = "Allow", Action = ["s3:GetObject", "s3:PutObject"], Resource = local.workload_state_key },
      { Sid = "LockWorkloadState", Effect = "Allow", Action = ["s3:DeleteObject", "s3:GetObject", "s3:PutObject"], Resource = "${local.workload_state_key}.tflock" },
      { Sid = "ListKeyAliases", Effect = "Allow", Action = "kms:ListAliases", Resource = "*" },
      { Sid = "WriteWorkloadApplyOutput", Effect = "Allow", Action = "s3:PutObject", Resource = "${var.apply_output_bucket_arn}/*" },
      ], local.bootstrap_deny_statements, [
      { Sid = "DenyBoundaryRemoval", Effect = "Deny", Action = "iam:DeleteRolePermissionsBoundary", Resource = "*" },
      local.lockout_bypass_deny_statement,
    ])

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
        local.lockout_bypass_deny_statement,
      ],
      local.identity_read_statements,
      [{ Sid = "WriteApplyOutput", Effect = "Allow", Action = "s3:PutObject", Resource = "${var.plan_output_bucket_arn}/apply/*" }],
      local.governance_read_statements,
      local.ci_policy_management_statements,
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
