# Action の一覧の根拠（Provider の各 resource が呼ぶ API）は usecases/deployment-identity/docs/adr/0201 が持つ。
locals {
  identity_state_key = "${var.state_bucket_arn}/identity/terraform.tfstate"
  workload_state_key = "${var.state_bucket_arn}/workload/terraform.tfstate"
  # pr/ は Pull Request の plan、branch/ は保護ブランチの plan（deployment-identity/ADR-0201 決定8）。
  plan_output_keys = ["${var.plan_output_bucket_arn}/pr/*", "${var.plan_output_bucket_arn}/branch/*"]

  # state の鍵の Resource は、鍵の ARN ではなくアカウントの鍵の pattern で与える（deployment-identity/ADR-0201 決定4）。
  kms_arn_prefix         = "arn:${data.aws_partition.current.partition}:kms:${data.aws_region.current.region}:${data.aws_caller_identity.current.account_id}"
  state_key_pattern      = "${local.kms_arn_prefix}:key/*"
  state_key_alias_arn    = "${local.kms_arn_prefix}:${var.state_key_alias_name}"
  state_key_read_actions = ["kms:DescribeKey", "kms:GetKeyPolicy", "kms:GetKeyRotationStatus", "kms:ListResourceTags"]

  identity_role_arns = [for k in keys(local.roles) : aws_iam_role.this[k].arn]
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
        # plan の role は鍵を作らないので、alias の付いた state の鍵にだけ読みを限れる。
        {
          Sid       = "ReadStateKey"
          Effect    = "Allow"
          Action    = local.state_key_read_actions
          Resource  = local.state_key_pattern
          Condition = { "ForAnyValue:StringEquals" = { "kms:ResourceAliases" = var.state_key_alias_name } }
        },
      ],
      local.identity_read_statements,
    )

    apply = [
      { Sid = "ListStateBucket", Effect = "Allow", Action = "s3:ListBucket", Resource = var.state_bucket_arn },
      { Sid = "WriteWorkloadState", Effect = "Allow", Action = ["s3:GetObject", "s3:PutObject"], Resource = local.workload_state_key },
      { Sid = "LockWorkloadState", Effect = "Allow", Action = ["s3:DeleteObject", "s3:GetObject", "s3:PutObject"], Resource = "${local.workload_state_key}.tflock" },
      # Deny の範囲は deployment-identity/ADR-0201 決定5。
      { Sid = "DenyIdentityChange", Effect = "Deny", Action = "iam:*", Resource = local.identity_arns },
      { Sid = "DenyIdentityStateWrite", Effect = "Deny", Action = ["s3:Delete*", "s3:Put*"], Resource = [local.identity_state_key, "${local.identity_state_key}.tflock"] },
      { Sid = "DenyStateBucketChange", Effect = "Deny", Action = ["s3:Delete*", "s3:Put*"], Resource = var.state_bucket_arn },
      { Sid = "DenyStateVersionDeletion", Effect = "Deny", Action = "s3:DeleteObjectVersion", Resource = "${var.state_bucket_arn}/*" },
      { Sid = "DenyPlanOutputBucketChange", Effect = "Deny", Action = ["s3:Delete*", "s3:Put*"], Resource = var.plan_output_bucket_arn },
      { Sid = "DenyPlanOutputWrite", Effect = "Deny", Action = ["s3:DeleteObjectVersion", "s3:Put*"], Resource = "${var.plan_output_bucket_arn}/*" },
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
        # kms:CreateKey は resource-level 権限を持たない（repository-wide/ADR-0301 決定9）。Provider は KeySpec と KeyUsage を常に送る。
        {
          Sid       = "CreateStateKey"
          Effect    = "Allow"
          Action    = "kms:CreateKey"
          Resource  = "*"
          Condition = { StringEquals = { "kms:KeySpec" = "SYMMETRIC_DEFAULT", "kms:KeyUsage" = "ENCRYPT_DECRYPT" } }
        },
        # 鍵を作る apply の途中では alias がまだ無いので、alias で鍵を絞れない（deployment-identity/ADR-0201 決定4）。
        { Sid = "ReadStateKey", Effect = "Allow", Action = local.state_key_read_actions, Resource = local.state_key_pattern },
        { Sid = "ManageStateKey", Effect = "Allow", Action = ["kms:EnableKeyRotation", "kms:PutKeyPolicy"], Resource = local.state_key_pattern },
        # CreateAlias は alias と鍵の両方に権限を要る。
        { Sid = "CreateStateKeyAlias", Effect = "Allow", Action = "kms:CreateAlias", Resource = [local.state_key_alias_arn, local.state_key_pattern] },
        # Provider が載せない値を KMS がどちらに評価しても既定の作成を妨げない Deny で拒む（deployment-identity/ADR-0201 決定1）。
        { Sid = "DenyMultiRegionKey", Effect = "Deny", Action = "kms:CreateKey", Resource = "*", Condition = { Bool = { "kms:MultiRegion" = "true" } } },
        { Sid = "DenyExternalKeyOrigin", Effect = "Deny", Action = "kms:CreateKey", Resource = "*", Condition = { StringEquals = { "kms:KeyOrigin" = ["AWS_CLOUDHSM", "EXTERNAL", "EXTERNAL_KEY_STORE"] } } },
        { Sid = "DenyLockoutSafetyBypass", Effect = "Deny", Action = ["kms:CreateKey", "kms:PutKeyPolicy"], Resource = "*", Condition = { Bool = { "kms:BypassPolicyLockoutSafetyCheck" = "true" } } },
      ],
      local.identity_read_statements,
    )
  }
}

resource "aws_iam_role_policy" "this" {
  for_each = local.roles

  name = each.key
  role = aws_iam_role.this[each.key].name
  policy = jsonencode({
    Version   = "2012-10-17"
    Statement = local.statements[each.key]
  })
}
