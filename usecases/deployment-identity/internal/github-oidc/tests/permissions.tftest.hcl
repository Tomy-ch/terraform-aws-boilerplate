mock_provider "aws" {}

variables {
  name_prefix      = "tabp-admin"
  env              = "admin"
  github           = { owner = "octo-org", owner_id = "123456", repository = "octo-repo", repository_id = "456789" }
  state_bucket_arn = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an"
}

# assert に for 式を書かない（tests/trust.tftest.hcl）。role と provider の ARN は plan では決まらないので、mock の apply で確かめる。
run "policies" {
  command = apply

  assert {
    condition = jsondecode(aws_iam_role_policy.this["plan"].policy).Statement == [
      { Sid = "ListStateBucket", Effect = "Allow", Action = "s3:ListBucket", Resource = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an" },
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
        Sid    = "ReadStateBucketConfiguration"
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
        Resource = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an"
      },
      { Sid = "ReadGitHubProvider", Effect = "Allow", Action = ["iam:GetOpenIDConnectProvider", "iam:ListOpenIDConnectProviderTags"], Resource = aws_iam_openid_connect_provider.github.arn },
      {
        Sid      = "ReadCiRoles"
        Effect   = "Allow"
        Action   = ["iam:GetRole", "iam:GetRolePolicy", "iam:ListAttachedRolePolicies", "iam:ListRolePolicies", "iam:ListRoleTags"]
        Resource = [aws_iam_role.this["apply"].arn, aws_iam_role.this["apply_identity"].arn, aws_iam_role.this["plan"].arn]
      },
    ]
    error_message = "plan の role の権限が、state の読み取りと、identity root の resource の読み取りだけになっていません。"
  }

  assert {
    condition = jsondecode(aws_iam_role_policy.this["apply"].policy).Statement == [
      { Sid = "ListStateBucket", Effect = "Allow", Action = "s3:ListBucket", Resource = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an" },
      { Sid = "WriteWorkloadState", Effect = "Allow", Action = ["s3:GetObject", "s3:PutObject"], Resource = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/workload/terraform.tfstate" },
      { Sid = "LockWorkloadState", Effect = "Allow", Action = ["s3:DeleteObject", "s3:GetObject", "s3:PutObject"], Resource = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/workload/terraform.tfstate.tflock" },
      {
        Sid      = "DenyIdentityChange"
        Effect   = "Deny"
        Action   = "iam:*"
        Resource = [aws_iam_role.this["apply"].arn, aws_iam_role.this["apply_identity"].arn, aws_iam_role.this["plan"].arn, aws_iam_openid_connect_provider.github.arn]
      },
      {
        Sid    = "DenyIdentityStateWrite"
        Effect = "Deny"
        Action = ["s3:Delete*", "s3:Put*"]
        Resource = [
          "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/identity/terraform.tfstate",
          "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/identity/terraform.tfstate.tflock",
        ]
      },
      { Sid = "DenyStateBucketChange", Effect = "Deny", Action = ["s3:Delete*", "s3:Put*"], Resource = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an" },
      { Sid = "DenyStateVersionDeletion", Effect = "Deny", Action = "s3:DeleteObjectVersion", Resource = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/*" },
    ]
    error_message = "apply の role の権限が、workload の state と lock の読み書きと、bootstrap の構成物・identity の state・state Bucket の変更の拒否になっていません。"
  }

  assert {
    condition = jsondecode(aws_iam_role_policy.this["apply_identity"].policy).Statement == [
      { Sid = "ListStateBucket", Effect = "Allow", Action = "s3:ListBucket", Resource = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an" },
      { Sid = "WriteIdentityState", Effect = "Allow", Action = ["s3:GetObject", "s3:PutObject"], Resource = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/identity/terraform.tfstate" },
      { Sid = "LockIdentityState", Effect = "Allow", Action = ["s3:DeleteObject", "s3:GetObject", "s3:PutObject"], Resource = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/identity/terraform.tfstate.tflock" },
      {
        Sid    = "ReadStateBucketConfiguration"
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
        Resource = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an"
      },
      {
        Sid      = "ConfigureStateBucket"
        Effect   = "Allow"
        Action   = ["s3:DeleteBucketPolicy", "s3:PutBucketOwnershipControls", "s3:PutBucketPolicy", "s3:PutBucketPublicAccessBlock", "s3:PutBucketVersioning", "s3:PutEncryptionConfiguration"]
        Resource = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an"
      },
      {
        Sid      = "ManageGitHubProvider"
        Effect   = "Allow"
        Action   = ["iam:AddClientIDToOpenIDConnectProvider", "iam:CreateOpenIDConnectProvider", "iam:DeleteOpenIDConnectProvider", "iam:RemoveClientIDFromOpenIDConnectProvider", "iam:UpdateOpenIDConnectProviderThumbprint"]
        Resource = aws_iam_openid_connect_provider.github.arn
      },
      {
        Sid      = "ManageCiRoles"
        Effect   = "Allow"
        Action   = ["iam:CreateRole", "iam:DeleteRole", "iam:DeleteRolePolicy", "iam:DetachRolePolicy", "iam:ListInstanceProfilesForRole", "iam:PutRolePolicy", "iam:UpdateAssumeRolePolicy", "iam:UpdateRole", "iam:UpdateRoleDescription"]
        Resource = [aws_iam_role.this["apply"].arn, aws_iam_role.this["apply_identity"].arn, aws_iam_role.this["plan"].arn]
      },
      { Sid = "ReadGitHubProvider", Effect = "Allow", Action = ["iam:GetOpenIDConnectProvider", "iam:ListOpenIDConnectProviderTags"], Resource = aws_iam_openid_connect_provider.github.arn },
      {
        Sid      = "ReadCiRoles"
        Effect   = "Allow"
        Action   = ["iam:GetRole", "iam:GetRolePolicy", "iam:ListAttachedRolePolicies", "iam:ListRolePolicies", "iam:ListRoleTags"]
        Resource = [aws_iam_role.this["apply"].arn, aws_iam_role.this["apply_identity"].arn, aws_iam_role.this["plan"].arn]
      },
    ]
    error_message = "apply-identity の role の権限が、identity の state と lock の読み書きと、bootstrap の構成物の管理になっていません。"
  }

  assert {
    condition = [
      [aws_iam_role_policy.this["plan"].name, aws_iam_role_policy.this["plan"].role],
      [aws_iam_role_policy.this["apply"].name, aws_iam_role_policy.this["apply"].role],
      [aws_iam_role_policy.this["apply_identity"].name, aws_iam_role_policy.this["apply_identity"].role],
      ] == [
      ["plan", aws_iam_role.this["plan"].name],
      ["apply", aws_iam_role.this["apply"].name],
      ["apply_identity", aws_iam_role.this["apply_identity"].name],
    ]
    error_message = "inline policy が、役割の名前で、その役割の role に付いていません。"
  }

  assert {
    condition = [
      [aws_iam_role_policies_exclusive.this["plan"].role_name, aws_iam_role_policies_exclusive.this["plan"].policy_names],
      [aws_iam_role_policies_exclusive.this["apply"].role_name, aws_iam_role_policies_exclusive.this["apply"].policy_names],
      [aws_iam_role_policies_exclusive.this["apply_identity"].role_name, aws_iam_role_policies_exclusive.this["apply_identity"].policy_names],
      ] == [
      [aws_iam_role.this["plan"].name, toset(["plan"])],
      [aws_iam_role.this["apply"].name, toset(["apply"])],
      [aws_iam_role.this["apply_identity"].name, toset(["apply_identity"])],
    ]
    error_message = "role の inline policy が、その役割の policy の1つだけに限られていません。"
  }

  assert {
    condition = [
      [aws_iam_role_policy_attachments_exclusive.this["plan"].role_name, length(aws_iam_role_policy_attachments_exclusive.this["plan"].policy_arns)],
      [aws_iam_role_policy_attachments_exclusive.this["apply"].role_name, length(aws_iam_role_policy_attachments_exclusive.this["apply"].policy_arns)],
      [aws_iam_role_policy_attachments_exclusive.this["apply_identity"].role_name, length(aws_iam_role_policy_attachments_exclusive.this["apply_identity"].policy_arns)],
      ] == [
      [aws_iam_role.this["plan"].name, 0],
      [aws_iam_role.this["apply"].name, 0],
      [aws_iam_role.this["apply_identity"].name, 0],
    ]
    error_message = "role に managed policy を付けない設定になっていません。"
  }
}
