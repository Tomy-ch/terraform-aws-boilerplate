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
  state_key_alias_name   = "alias/tabp-admin-tfstate"
  plan_output_bucket_arn = "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an"
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
        Sid       = "ReadStateKey"
        Effect    = "Allow"
        Action    = ["kms:DescribeKey", "kms:GetKeyPolicy", "kms:GetKeyRotationStatus", "kms:ListResourceTags"]
        Resource  = "arn:aws:kms:ap-northeast-1:123456789012:key/*"
        Condition = { "ForAnyValue:StringEquals" = { "kms:ResourceAliases" = "alias/tabp-admin-tfstate" } }
      },
      { Sid = "ReadGitHubProvider", Effect = "Allow", Action = ["iam:GetOpenIDConnectProvider", "iam:ListOpenIDConnectProviderTags"], Resource = aws_iam_openid_connect_provider.github.arn },
      {
        Sid      = "ReadCiRoles"
        Effect   = "Allow"
        Action   = ["iam:GetRole", "iam:GetRolePolicy", "iam:ListAttachedRolePolicies", "iam:ListRolePolicies", "iam:ListRoleTags"]
        Resource = [aws_iam_role.this["apply"].arn, aws_iam_role.this["apply_identity"].arn, aws_iam_role.this["plan"].arn]
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
      { Sid = "DenyPlanOutputBucketChange", Effect = "Deny", Action = ["s3:Delete*", "s3:Put*"], Resource = "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an" },
      { Sid = "DenyPlanOutputWrite", Effect = "Deny", Action = ["s3:DeleteObjectVersion", "s3:Put*"], Resource = "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an/*" },
    ]
    error_message = "apply の role の権限が、workload の state と lock の読み書きと、bootstrap の構成物・identity の state・2つの Bucket・plan の出力の変更の拒否になっていません。"
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
        Resource = [aws_iam_role.this["apply"].arn, aws_iam_role.this["apply_identity"].arn, aws_iam_role.this["plan"].arn]
      },
      {
        Sid       = "CreateStateKey"
        Effect    = "Allow"
        Action    = "kms:CreateKey"
        Resource  = "*"
        Condition = { StringEquals = { "kms:KeySpec" = "SYMMETRIC_DEFAULT", "kms:KeyUsage" = "ENCRYPT_DECRYPT" } }
      },
      {
        Sid       = "ReadStateKey"
        Effect    = "Allow"
        Action    = ["kms:DescribeKey", "kms:GetKeyPolicy", "kms:GetKeyRotationStatus", "kms:ListResourceTags"]
        Resource  = "arn:aws:kms:ap-northeast-1:123456789012:key/*"
        Condition = { "ForAllValues:StringEquals" = { "kms:ResourceAliases" = "alias/tabp-admin-tfstate" } }
      },
      {
        Sid       = "ManageStateKey"
        Effect    = "Allow"
        Action    = ["kms:EnableKeyRotation", "kms:PutKeyPolicy"]
        Resource  = "arn:aws:kms:ap-northeast-1:123456789012:key/*"
        Condition = { "ForAllValues:StringEquals" = { "kms:ResourceAliases" = "alias/tabp-admin-tfstate" } }
      },
      {
        Sid       = "CreateStateKeyAlias"
        Effect    = "Allow"
        Action    = "kms:CreateAlias"
        Resource  = ["arn:aws:kms:ap-northeast-1:123456789012:alias/tabp-admin-tfstate", "arn:aws:kms:ap-northeast-1:123456789012:key/*"]
        Condition = { "ForAllValues:StringEquals" = { "kms:ResourceAliases" = "alias/tabp-admin-tfstate" } }
      },
      { Sid = "DenyMultiRegionKey", Effect = "Deny", Action = "kms:CreateKey", Resource = "*", Condition = { Bool = { "kms:MultiRegion" = "true" } } },
      { Sid = "DenyExternalKeyOrigin", Effect = "Deny", Action = "kms:CreateKey", Resource = "*", Condition = { StringEquals = { "kms:KeyOrigin" = ["AWS_CLOUDHSM", "EXTERNAL", "EXTERNAL_KEY_STORE"] } } },
      { Sid = "DenyLockoutSafetyBypass", Effect = "Deny", Action = ["kms:CreateKey", "kms:PutKeyPolicy"], Resource = "*", Condition = { Bool = { "kms:BypassPolicyLockoutSafetyCheck" = "true" } } },
      { Sid = "ReadGitHubProvider", Effect = "Allow", Action = ["iam:GetOpenIDConnectProvider", "iam:ListOpenIDConnectProviderTags"], Resource = aws_iam_openid_connect_provider.github.arn },
      {
        Sid      = "ReadCiRoles"
        Effect   = "Allow"
        Action   = ["iam:GetRole", "iam:GetRolePolicy", "iam:ListAttachedRolePolicies", "iam:ListRolePolicies", "iam:ListRoleTags"]
        Resource = [aws_iam_role.this["apply"].arn, aws_iam_role.this["apply_identity"].arn, aws_iam_role.this["plan"].arn]
      },
      { Sid = "ListKeyAliases", Effect = "Allow", Action = "kms:ListAliases", Resource = "*" },
    ]
    error_message = "apply-identity の role の権限が、identity の state と lock の読み書きと、bootstrap の構成物と state の鍵の管理になっていません。"
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

# 鍵の ARN は data source から、alias は入力から組む。Bucket の ARN と違う値を与え、どちらを読んだかを区別する。
run "key_arns_follow_inputs" {
  command = apply

  variables {
    name_prefix          = "t4bp-dev"
    state_key_alias_name = "alias/other-tfstate"
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
    condition = jsondecode(aws_iam_role_policy.this["plan"].policy).Statement[6] == {
      Sid       = "ReadStateKey"
      Effect    = "Allow"
      Action    = ["kms:DescribeKey", "kms:GetKeyPolicy", "kms:GetKeyRotationStatus", "kms:ListResourceTags"]
      Resource  = "arn:aws-us-gov:kms:us-gov-west-1:210987654321:key/*"
      Condition = { "ForAnyValue:StringEquals" = { "kms:ResourceAliases" = "alias/other-tfstate" } }
    }
    error_message = "plan の role の鍵の読み取りが、data source の partition・region・account と入力の alias から組まれていません。"
  }

  assert {
    condition = [jsondecode(aws_iam_role_policy.this["apply_identity"].policy).Statement[11].Resource, jsondecode(aws_iam_role_policy.this["apply_identity"].policy).Statement[11].Condition] == [
      "arn:aws-us-gov:kms:us-gov-west-1:210987654321:key/*",
      { "ForAllValues:StringEquals" = { "kms:ResourceAliases" = "alias/other-tfstate" } },
    ]
    error_message = "apply-identity の鍵の読み取りが、data source と入力の alias から組まれていません。"
  }

  assert {
    condition = [jsondecode(aws_iam_role_policy.this["apply_identity"].policy).Statement[12].Resource, jsondecode(aws_iam_role_policy.this["apply_identity"].policy).Statement[12].Condition] == [
      "arn:aws-us-gov:kms:us-gov-west-1:210987654321:key/*",
      { "ForAllValues:StringEquals" = { "kms:ResourceAliases" = "alias/other-tfstate" } },
    ]
    error_message = "apply-identity の鍵の管理が、data source と入力の alias から組まれていません。"
  }

  assert {
    condition = jsondecode(aws_iam_role_policy.this["apply_identity"].policy).Statement[13] == {
      Sid       = "CreateStateKeyAlias"
      Effect    = "Allow"
      Action    = "kms:CreateAlias"
      Resource  = ["arn:aws-us-gov:kms:us-gov-west-1:210987654321:alias/other-tfstate", "arn:aws-us-gov:kms:us-gov-west-1:210987654321:key/*"]
      Condition = { "ForAllValues:StringEquals" = { "kms:ResourceAliases" = "alias/other-tfstate" } }
    }
    error_message = "apply-identity の alias の作成が、data source の partition・region・account と入力の alias から組まれていません。"
  }
}

# inline policy は role あたり 10,240 文字（空白を除く）が上限である（deployment-identity/ADR-0201「影響」）。
# mock の ARN は実物より短いので、入力が許す最も長い値（38文字の project と sandbox、250文字の alias、aws-cn の
# 63文字の Bucket）と、それに対応する長さの ARN を与えて数える。
run "policy_length" {
  command = apply

  variables {
    name_prefix            = "abcdefghijklmnopqrstuvwxyz012345678901-sandbox"
    state_bucket_arn       = format("arn:aws-cn:s3:::%063d", 1)
    state_key_alias_name   = format("alias/%0250d", 0)
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
    target = aws_iam_role.this["plan"]
    values = { arn = "arn:aws-cn:iam::123456789012:role/abcdefghijklmnopqrstuvwxyz012345678901-sandbox-ci-plan" }
  }
  override_resource {
    target = aws_iam_role.this["apply"]
    values = { arn = "arn:aws-cn:iam::123456789012:role/abcdefghijklmnopqrstuvwxyz012345678901-sandbox-ci-apply" }
  }
  override_resource {
    target = aws_iam_role.this["apply_identity"]
    values = { arn = "arn:aws-cn:iam::123456789012:role/abcdefghijklmnopqrstuvwxyz012345678901-sandbox-ci-apply-identity" }
  }
  override_resource {
    target = aws_iam_openid_connect_provider.github
    values = { arn = "arn:aws-cn:iam::123456789012:oidc-provider/token.actions.githubusercontent.com" }
  }

  assert {
    condition     = length(var.state_key_alias_name) == 256 && length(trimprefix(var.state_bucket_arn, "arn:aws-cn:s3:::")) == 63
    error_message = "境界の入力の長さが、alias の 256 文字と Bucket 名の 63 文字になっていません。"
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
    condition     = length(aws_iam_role_policy.this["apply_identity"].policy) <= 10240
    error_message = "apply-identity の role の inline policy が 10,240 文字を超えています。"
  }
}
