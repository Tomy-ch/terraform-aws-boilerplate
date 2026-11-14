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
  name_prefix            = "tabp-admin"
  env                    = "admin"
  github                 = { owner = "octo-org", owner_id = "123456", repository = "octo-repo", repository_id = "456789" }
  state_bucket_arn       = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an"
  state_key_arn          = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  plan_output_bucket_arn = "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an"
}

# assert に for 式を書かない（tests/trust.tftest.hcl）。provider の ARN は plan では決まらないので、mock の apply で確かめる。
# role の ARN は名前から組むので、mock の role の ARN ではなく literal と比べる。
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
      { Sid = "ListPlanOutputBucket", Effect = "Allow", Action = "s3:ListBucket", Resource = "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an" },
      {
        Sid    = "ReadPlanOutputBucketConfiguration"
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
        Resource = "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an"
      },
      { Sid = "WritePlanOutput", Effect = "Allow", Action = "s3:PutObject", Resource = ["arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an/pr/*", "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an/branch/*"] },
      {
        Sid      = "ReadStateKey"
        Effect   = "Allow"
        Action   = ["kms:DescribeKey", "kms:GetKeyPolicy", "kms:GetKeyRotationStatus", "kms:ListResourceTags"]
        Resource = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
      },
      { Sid = "ReadGitHubProvider", Effect = "Allow", Action = ["iam:GetOpenIDConnectProvider", "iam:ListOpenIDConnectProviderTags"], Resource = aws_iam_openid_connect_provider.github.arn },
      {
        Sid      = "ReadCiRoles"
        Effect   = "Allow"
        Action   = ["iam:GetRole", "iam:GetRolePolicy", "iam:ListAttachedRolePolicies", "iam:ListRolePolicies", "iam:ListRoleTags"]
        Resource = ["arn:aws:iam::123456789012:role/tabp-admin-ci-apply", "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity", "arn:aws:iam::123456789012:role/tabp-admin-ci-plan", "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"]
      },
      { Sid = "ListKeyAliases", Effect = "Allow", Action = "kms:ListAliases", Resource = "*" },
    ]
    error_message = "plan の role の権限が、state と2つの Bucket の設定と identity root の resource と state の鍵の読み取りと、plan の出力の書き込みだけになっていません。"
  }

  assert {
    condition = jsondecode(aws_iam_role_policy.this["apply"].policy).Statement == [
      { Sid = "ListStateBucket", Effect = "Allow", Action = "s3:ListBucket", Resource = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an" },
      { Sid = "WriteWorkloadState", Effect = "Allow", Action = ["s3:GetObject", "s3:PutObject"], Resource = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/workload/terraform.tfstate" },
      { Sid = "LockWorkloadState", Effect = "Allow", Action = ["s3:DeleteObject", "s3:GetObject", "s3:PutObject"], Resource = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/workload/terraform.tfstate.tflock" },
      {
        Sid    = "DenyIdentityChange"
        Effect = "Deny"
        Action = "*"
        Resource = [
          "arn:aws:iam::123456789012:role/tabp-admin-ci-apply",
          "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity",
          "arn:aws:iam::123456789012:role/tabp-admin-ci-plan",
          "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity",
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
      { Sid = "DenyStateBucketChange", Effect = "Deny", NotAction = ["s3:ListBucket"], Resource = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an" },
      { Sid = "DenyStateVersionDeletion", Effect = "Deny", Action = "s3:DeleteObjectVersion", Resource = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/*" },
      { Sid = "DenyPlanOutputChange", Effect = "Deny", Action = "*", Resource = ["arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an", "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an/*"] },
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
    ]
    error_message = "apply の role の権限が、workload の state と lock の読み書きと、bootstrap の構成物・identity の state・2つの Bucket・plan の出力・state の鍵の変更の拒否になっていません。"
  }

  assert {
    condition = jsondecode(aws_iam_role_policy.apply_identity.policy).Statement == [
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
        Action   = ["s3:DeleteBucketPolicy", "s3:PutBucketOwnershipControls", "s3:PutBucketPolicy", "s3:PutBucketPublicAccessBlock", "s3:PutBucketVersioning", "s3:PutEncryptionConfiguration", "s3:PutLifecycleConfiguration"]
        Resource = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an"
      },
      { Sid = "ListPlanOutputBucket", Effect = "Allow", Action = "s3:ListBucket", Resource = "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an" },
      {
        Sid    = "ReadPlanOutputBucketConfiguration"
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
        Resource = "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an"
      },
      {
        Sid      = "ConfigurePlanOutputBucket"
        Effect   = "Allow"
        Action   = ["s3:DeleteBucketPolicy", "s3:PutBucketOwnershipControls", "s3:PutBucketPolicy", "s3:PutBucketPublicAccessBlock", "s3:PutBucketVersioning", "s3:PutEncryptionConfiguration", "s3:PutLifecycleConfiguration"]
        Resource = "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an"
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
        Resource = ["arn:aws:iam::123456789012:role/tabp-admin-ci-apply", "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity", "arn:aws:iam::123456789012:role/tabp-admin-ci-plan", "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"]
      },
      { Sid = "ReadStateKey", Effect = "Allow", Action = ["kms:DescribeKey", "kms:GetKeyPolicy", "kms:GetKeyRotationStatus", "kms:ListResourceTags"], Resource = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab" },
      { Sid = "ManageStateKey", Effect = "Allow", Action = ["kms:EnableKeyRotation", "kms:PutKeyPolicy"], Resource = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab" },
      { Sid = "DenyLockoutSafetyBypass", Effect = "Deny", Action = "kms:PutKeyPolicy", Resource = "*", Condition = { Bool = { "kms:BypassPolicyLockoutSafetyCheck" = "true" } } },
      { Sid = "ReadGitHubProvider", Effect = "Allow", Action = ["iam:GetOpenIDConnectProvider", "iam:ListOpenIDConnectProviderTags"], Resource = aws_iam_openid_connect_provider.github.arn },
      {
        Sid      = "ReadCiRoles"
        Effect   = "Allow"
        Action   = ["iam:GetRole", "iam:GetRolePolicy", "iam:ListAttachedRolePolicies", "iam:ListRolePolicies", "iam:ListRoleTags"]
        Resource = ["arn:aws:iam::123456789012:role/tabp-admin-ci-apply", "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity", "arn:aws:iam::123456789012:role/tabp-admin-ci-plan", "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"]
      },
      { Sid = "ListKeyAliases", Effect = "Allow", Action = "kms:ListAliases", Resource = "*" },
    ]
    error_message = "apply-identity の role の権限が、identity の state と lock の読み書きと、bootstrap の構成物と state の鍵の管理になっていません。"
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
    condition = [
      [aws_iam_role_policy.this["plan"].name, aws_iam_role_policy.this["plan"].role],
      [aws_iam_role_policy.this["apply"].name, aws_iam_role_policy.this["apply"].role],
      [aws_iam_role_policy.apply_identity.name, aws_iam_role_policy.apply_identity.role],
      [aws_iam_role_policy.this["unlock_identity"].name, aws_iam_role_policy.this["unlock_identity"].role],
      ] == [
      ["plan", aws_iam_role.this["plan"].name],
      ["apply", aws_iam_role.this["apply"].name],
      ["apply_identity", aws_iam_role.apply_identity.name],
      ["unlock_identity", aws_iam_role.this["unlock_identity"].name],
    ]
    error_message = "inline policy が、役割の名前で、その役割の role に付いていません。"
  }

  assert {
    condition = [
      [aws_iam_role_policies_exclusive.this["plan"].role_name, aws_iam_role_policies_exclusive.this["plan"].policy_names],
      [aws_iam_role_policies_exclusive.this["apply"].role_name, aws_iam_role_policies_exclusive.this["apply"].policy_names],
      [aws_iam_role_policies_exclusive.this["apply_identity"].role_name, aws_iam_role_policies_exclusive.this["apply_identity"].policy_names],
      [aws_iam_role_policies_exclusive.this["unlock_identity"].role_name, aws_iam_role_policies_exclusive.this["unlock_identity"].policy_names],
      ] == [
      [aws_iam_role.this["plan"].name, toset(["plan"])],
      [aws_iam_role.this["apply"].name, toset(["apply"])],
      [aws_iam_role.apply_identity.name, toset(["apply_identity"])],
      [aws_iam_role.this["unlock_identity"].name, toset(["unlock_identity"])],
    ]
    error_message = "role の inline policy が、その役割の policy の1つだけに限られていません。"
  }

  assert {
    condition = [
      [aws_iam_role_policy_attachments_exclusive.this["plan"].role_name, length(aws_iam_role_policy_attachments_exclusive.this["plan"].policy_arns)],
      [aws_iam_role_policy_attachments_exclusive.this["apply"].role_name, length(aws_iam_role_policy_attachments_exclusive.this["apply"].policy_arns)],
      [aws_iam_role_policy_attachments_exclusive.this["apply_identity"].role_name, length(aws_iam_role_policy_attachments_exclusive.this["apply_identity"].policy_arns)],
      [aws_iam_role_policy_attachments_exclusive.this["unlock_identity"].role_name, length(aws_iam_role_policy_attachments_exclusive.this["unlock_identity"].policy_arns)],
      ] == [
      [aws_iam_role.this["plan"].name, 0],
      [aws_iam_role.this["apply"].name, 0],
      [aws_iam_role.apply_identity.name, 0],
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
      jsondecode(aws_iam_role_policy.this["plan"].policy).Statement[6].Resource,
      jsondecode(aws_iam_role_policy.this["apply"].policy).Statement[8].Resource,
      jsondecode(aws_iam_role_policy.apply_identity.policy).Statement[10].Resource,
      jsondecode(aws_iam_role_policy.apply_identity.policy).Statement[11].Resource,
      ] == [
      "arn:aws-us-gov:kms:us-gov-west-1:210987654321:key/abcdef01-2345-6789-abcd-ef0123456789",
      "arn:aws-us-gov:kms:us-gov-west-1:210987654321:key/abcdef01-2345-6789-abcd-ef0123456789",
      "arn:aws-us-gov:kms:us-gov-west-1:210987654321:key/abcdef01-2345-6789-abcd-ef0123456789",
      "arn:aws-us-gov:kms:us-gov-west-1:210987654321:key/abcdef01-2345-6789-abcd-ef0123456789",
    ]
    error_message = "plan の読み取り、apply の Deny、apply-identity の読み取りと管理の Resource が、入力の鍵の ARN になっていません。"
  }
}

# 別のアカウントの鍵を渡すと、apply-identity が自分の state の鍵を管理できなくなる。
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

run "key_in_other_partition" {
  command = plan

  variables {
    state_key_arn = "arn:aws-cn:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  }

  expect_failures = [aws_iam_role_policy.apply_identity]
}

# inline policy は role あたり 10,240 文字（空白を除く）が上限である（deployment-identity/ADR-0201「影響」）。
# mock の ARN は実物より短いので、入力が許す最も長い値（37文字の project と sandbox、aws-cn の 63文字の Bucket）と、
# それに対応する長さの ARN を与えて数える。role の ARN は名前から組むので、partition と name_prefix から長さが決まる。
run "policy_length" {
  command = apply

  variables {
    name_prefix            = "abcdefghijklmnopqrstuvwxyz01234567890-sandbox"
    state_bucket_arn       = format("arn:aws-cn:s3:::%063d", 1)
    state_key_arn          = "arn:aws-cn:kms:cn-northwest-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
    plan_output_bucket_arn = format("arn:aws-cn:s3:::%063d", 2)
  }

  override_data {
    target = data.aws_partition.current
    values = { partition = "aws-cn" }
  }
  override_data {
    target = data.aws_region.current
    values = { region = "cn-northwest-1" }
  }
  override_resource {
    target = aws_iam_openid_connect_provider.github
    values = { arn = "arn:aws-cn:iam::123456789012:oidc-provider/token.actions.githubusercontent.com" }
  }

  assert {
    condition     = length(trimprefix(var.state_bucket_arn, "arn:aws-cn:s3:::")) == 63
    error_message = "境界の入力の長さが、Bucket 名の 63 文字になっていません。"
  }

  assert {
    condition     = length(aws_iam_role.this["unlock_identity"].name) == 64
    error_message = "境界の入力で、最も長い role 名が IAM の上限の 64 文字になっていません。"
  }

  assert {
    condition     = length(aws_iam_role_policy.this["plan"].policy) <= 10240
    error_message = "plan の role の inline policy が 10,240 文字を超えています。"
  }

  assert {
    condition     = length(aws_iam_role_policy.this["apply"].policy) <= 10240
    error_message = "apply の role の inline policy が 10,240 文字を超えています。"
  }

  assert {
    condition     = length(aws_iam_role_policy.apply_identity.policy) <= 10240
    error_message = "apply-identity の role の inline policy が 10,240 文字を超えています。"
  }

  assert {
    condition     = length(aws_iam_role_policy.this["unlock_identity"].policy) <= 10240
    error_message = "unlock-identity の role の inline policy が 10,240 文字を超えています。"
  }
}

# 既定と違う partition とアカウントを与え、入力を読んだかを区別する。
run "role_arns_follow_data_sources" {
  command = apply

  variables {
    name_prefix   = "t4bp-dev"
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
    condition = jsondecode(aws_iam_role_policy.apply_identity.policy).Statement[9].Resource == [
      "arn:aws-us-gov:iam::210987654321:role/t4bp-dev-ci-apply",
      "arn:aws-us-gov:iam::210987654321:role/t4bp-dev-ci-apply-identity",
      "arn:aws-us-gov:iam::210987654321:role/t4bp-dev-ci-plan",
      "arn:aws-us-gov:iam::210987654321:role/t4bp-dev-ci-unlock-identity",
    ]
    error_message = "apply-identity の ManageCiRoles の Resource が、partition・アカウント・role 名から組んだ ARN になっていません。"
  }
}
