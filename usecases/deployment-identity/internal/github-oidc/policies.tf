# Action の一覧の根拠（Provider の各 resource が呼ぶ API）は usecases/deployment-identity/docs/adr/0201 が持つ。
locals {
  identity_state_key = "${var.state_bucket_arn}/identity/terraform.tfstate"
  workload_state_key = "${var.state_bucket_arn}/workload/terraform.tfstate"
  # pr/ は Pull Request の plan、branch/ は保護ブランチの plan（deployment-identity/ADR-0201 決定8）。
  plan_output_keys = ["${var.plan_output_bucket_arn}/pr/*", "${var.plan_output_bucket_arn}/branch/*"]

  state_key_read_actions = ["kms:DescribeKey", "kms:GetKeyPolicy", "kms:GetKeyRotationStatus", "kms:ListResourceTags"]
  # modules/kms の Key Policy がアカウントへ委任する Action のすべて（modules/kms/policy.tf の administration_actions。
  # deployment-identity/ADR-0201 決定5）。
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
    # kms:ListAliases は resource-level 権限を持たない（repository-wide/ADR-0301 決定9）。
    { Sid = "ListKeyAliases", Effect = "Allow", Action = "kms:ListAliases", Resource = "*" },
  ]

  statements = {
    plan = concat(
      [
        { Sid = "ListStateBucket", Effect = "Allow", Action = "s3:ListBucket", Resource = var.state_bucket_arn },
        { Sid = "ReadState", Effect = "Allow", Action = "s3:GetObject", Resource = [local.identity_state_key, local.workload_state_key] },
        { Sid = "ReadStateBucketConfiguration", Effect = "Allow", Action = local.bucket_read_actions, Resource = var.state_bucket_arn },
        { Sid = "ListPlanOutputBucket", Effect = "Allow", Action = "s3:ListBucket", Resource = var.plan_output_bucket_arn },
        { Sid = "ReadPlanOutputBucketConfiguration", Effect = "Allow", Action = local.bucket_read_actions, Resource = var.plan_output_bucket_arn },
        # 削除の権限を与えない。上書きされた出力は版として残る（repository-wide/ADR-0602 決定13）。
        { Sid = "WritePlanOutput", Effect = "Allow", Action = "s3:PutObject", Resource = local.plan_output_keys },
        { Sid = "ReadStateKey", Effect = "Allow", Action = local.state_key_read_actions, Resource = var.state_key_arn },
      ],
      local.identity_read_statements,
    )

    apply = [
      { Sid = "ListStateBucket", Effect = "Allow", Action = "s3:ListBucket", Resource = var.state_bucket_arn },
      { Sid = "WriteWorkloadState", Effect = "Allow", Action = ["s3:GetObject", "s3:PutObject"], Resource = local.workload_state_key },
      { Sid = "LockWorkloadState", Effect = "Allow", Action = ["s3:DeleteObject", "s3:GetObject", "s3:PutObject"], Resource = "${local.workload_state_key}.tflock" },
      # Deny の範囲と形は deployment-identity/ADR-0201 決定5。
      { Sid = "DenyIdentityChange", Effect = "Deny", Action = "*", Resource = local.identity_arns },
      { Sid = "DenyIdentityStateChange", Effect = "Deny", Action = "*", Resource = [local.identity_state_key, "${local.identity_state_key}.tflock"] },
      { Sid = "DenyStateBucketChange", Effect = "Deny", NotAction = ["s3:ListBucket"], Resource = var.state_bucket_arn },
      { Sid = "DenyStateVersionDeletion", Effect = "Deny", Action = "s3:DeleteObjectVersion", Resource = "${var.state_bucket_arn}/*" },
      { Sid = "DenyPlanOutputChange", Effect = "Deny", Action = "*", Resource = [var.plan_output_bucket_arn, "${var.plan_output_bucket_arn}/*"] },
      # kms:* にすると、Key Policy が role を名指して許す暗号操作まで塞ぐ。
      { Sid = "DenyStateKeyChange", Effect = "Deny", Action = local.state_key_administration_actions, Resource = var.state_key_arn },
    ]

    apply_identity = concat(
      [
        { Sid = "ListStateBucket", Effect = "Allow", Action = "s3:ListBucket", Resource = var.state_bucket_arn },
        { Sid = "WriteIdentityState", Effect = "Allow", Action = ["s3:GetObject", "s3:PutObject"], Resource = local.identity_state_key },
        { Sid = "LockIdentityState", Effect = "Allow", Action = ["s3:DeleteObject", "s3:GetObject", "s3:PutObject"], Resource = "${local.identity_state_key}.tflock" },
        { Sid = "ReadStateBucketConfiguration", Effect = "Allow", Action = local.bucket_read_actions, Resource = var.state_bucket_arn },
        { Sid = "ConfigureStateBucket", Effect = "Allow", Action = concat(local.bucket_configure_actions, ["s3:PutLifecycleConfiguration"]), Resource = var.state_bucket_arn },
        { Sid = "ListPlanOutputBucket", Effect = "Allow", Action = "s3:ListBucket", Resource = var.plan_output_bucket_arn },
        { Sid = "ReadPlanOutputBucketConfiguration", Effect = "Allow", Action = local.bucket_read_actions, Resource = var.plan_output_bucket_arn },
        # s3:PutLifecycleConfiguration は DeleteBucketLifecycle の権限も兼ねる。
        { Sid = "ConfigurePlanOutputBucket", Effect = "Allow", Action = concat(local.bucket_configure_actions, ["s3:PutLifecycleConfiguration"]), Resource = var.plan_output_bucket_arn },
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
    )

    # ListObjectsV2・GetObject・If-Match の DeleteObject だけ（deployment-identity/ADR-0201 決定9）。If-Match の削除は
    # s3:GetObject も要る。
    unlock_identity = [
      { Sid = "ListIdentityLock", Effect = "Allow", Action = "s3:ListBucket", Resource = var.state_bucket_arn, Condition = { StringEquals = { "s3:prefix" = "identity/terraform.tfstate.tflock" } } },
      { Sid = "RemoveIdentityLock", Effect = "Allow", Action = ["s3:DeleteObject", "s3:GetObject"], Resource = "${local.identity_state_key}.tflock" },
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
