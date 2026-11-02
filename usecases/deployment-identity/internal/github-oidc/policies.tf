# Action の一覧の根拠（Provider の各 resource が呼ぶ API）は usecases/deployment-identity/docs/adr/0201 が持つ。
locals {
  identity_state_key = "${var.state_bucket_arn}/identity/terraform.tfstate"
  workload_state_key = "${var.state_bucket_arn}/workload/terraform.tfstate"
  # CI の plan が出力を書く key の接頭辞。Pull Request の plan と保護ブランチの plan を分ける。
  plan_output_keys = ["${var.plan_output_bucket_arn}/pr/*", "${var.plan_output_bucket_arn}/branch/*"]

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
      ],
      local.identity_read_statements,
    )

    apply = [
      { Sid = "ListStateBucket", Effect = "Allow", Action = "s3:ListBucket", Resource = var.state_bucket_arn },
      { Sid = "WriteWorkloadState", Effect = "Allow", Action = ["s3:GetObject", "s3:PutObject"], Resource = local.workload_state_key },
      { Sid = "LockWorkloadState", Effect = "Allow", Action = ["s3:DeleteObject", "s3:GetObject", "s3:PutObject"], Resource = "${local.workload_state_key}.tflock" },
      # bootstrap の構成物、identity root の state、2つの Bucket の設定、plan の出力を変えさせない Deny（deployment-identity/ADR-0201 決定5）。
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
        { Sid = "ConfigureStateBucket", Effect = "Allow", Action = local.bucket_configure_actions, Resource = var.state_bucket_arn },
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
