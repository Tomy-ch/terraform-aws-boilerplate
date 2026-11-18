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
}

# provider の ARN は plan では決まらないので、mock の apply で確かめる。
run "trust_per_role" {
  command = apply

  assert {
    condition = jsondecode(aws_iam_role.this["plan"].assume_role_policy) == {
      Version = "2012-10-17"
      Statement = [
        {
          Effect    = "Allow"
          Principal = { Federated = aws_iam_openid_connect_provider.github.arn }
          Action    = "sts:AssumeRoleWithWebIdentity"
          Condition = {
            StringEquals = {
              "token.actions.githubusercontent.com:aud"                 = "sts.amazonaws.com"
              "token.actions.githubusercontent.com:sub"                 = "repo:octo-org@123456/octo-repo@456789:environment:admin-plan"
              "token.actions.githubusercontent.com:repository_id"       = "456789"
              "token.actions.githubusercontent.com:repository_owner_id" = "123456"
            }
          }
        },
      ]
    }
    error_message = "plan の role の信頼条件が、environment admin-plan と、リポジトリと所有者の ID に完全一致で限られた1文になっていません。"
  }

  assert {
    condition = jsondecode(aws_iam_role.this["apply"].assume_role_policy) == {
      Version = "2012-10-17"
      Statement = [
        {
          Effect    = "Allow"
          Principal = { Federated = aws_iam_openid_connect_provider.github.arn }
          Action    = "sts:AssumeRoleWithWebIdentity"
          Condition = {
            StringEquals = {
              "token.actions.githubusercontent.com:aud"                 = "sts.amazonaws.com"
              "token.actions.githubusercontent.com:sub"                 = "repo:octo-org@123456/octo-repo@456789:environment:admin-apply"
              "token.actions.githubusercontent.com:repository_id"       = "456789"
              "token.actions.githubusercontent.com:repository_owner_id" = "123456"
            }
          }
        },
      ]
    }
    error_message = "apply の role の信頼条件が、environment admin-apply と、リポジトリと所有者の ID に完全一致で限られた1文になっていません。"
  }

  assert {
    condition = jsondecode(aws_iam_role.apply_identity.assume_role_policy) == {
      Version = "2012-10-17"
      Statement = [
        {
          Effect    = "Allow"
          Principal = { Federated = aws_iam_openid_connect_provider.github.arn }
          Action    = "sts:AssumeRoleWithWebIdentity"
          Condition = {
            StringEquals = {
              "token.actions.githubusercontent.com:aud"                 = "sts.amazonaws.com"
              "token.actions.githubusercontent.com:sub"                 = "repo:octo-org@123456/octo-repo@456789:environment:admin-apply-identity"
              "token.actions.githubusercontent.com:repository_id"       = "456789"
              "token.actions.githubusercontent.com:repository_owner_id" = "123456"
            }
          }
        },
      ]
    }
    error_message = "apply_identity の role の信頼条件が、environment admin-apply-identity と、リポジトリと所有者の ID に完全一致で限られた1文になっていません。"
  }

  assert {
    condition = jsondecode(aws_iam_role.this["unlock_identity"].assume_role_policy) == {
      Version = "2012-10-17"
      Statement = [
        {
          Effect    = "Allow"
          Principal = { Federated = aws_iam_openid_connect_provider.github.arn }
          Action    = "sts:AssumeRoleWithWebIdentity"
          Condition = {
            StringEquals = {
              "token.actions.githubusercontent.com:aud"                 = "sts.amazonaws.com"
              "token.actions.githubusercontent.com:sub"                 = "repo:octo-org@123456/octo-repo@456789:environment:admin-unlock-identity"
              "token.actions.githubusercontent.com:repository_id"       = "456789"
              "token.actions.githubusercontent.com:repository_owner_id" = "123456"
            }
          }
        },
      ]
    }
    error_message = "unlock_identity の role の信頼条件が、environment admin-unlock-identity と、リポジトリと所有者の ID に完全一致で限られた1文になっていません。"
  }

  assert {
    condition = jsondecode(aws_iam_role.this["unlock"].assume_role_policy) == {
      Version = "2012-10-17"
      Statement = [
        {
          Effect    = "Allow"
          Principal = { Federated = aws_iam_openid_connect_provider.github.arn }
          Action    = "sts:AssumeRoleWithWebIdentity"
          Condition = {
            StringEquals = {
              "token.actions.githubusercontent.com:aud"                 = "sts.amazonaws.com"
              "token.actions.githubusercontent.com:sub"                 = "repo:octo-org@123456/octo-repo@456789:environment:admin-unlock"
              "token.actions.githubusercontent.com:repository_id"       = "456789"
              "token.actions.githubusercontent.com:repository_owner_id" = "123456"
            }
          }
        },
      ]
    }
    error_message = "unlock の role の信頼条件が、environment admin-unlock と、リポジトリと所有者の ID に完全一致で限られた1文になっていません。"
  }
}

run "role_names" {
  command = plan

  assert {
    condition = [aws_iam_role.this["plan"].name, aws_iam_role.this["apply"].name, aws_iam_role.apply_identity.name, aws_iam_role.this["unlock_identity"].name, aws_iam_role.this["unlock"].name] == [
      "tabp-admin-ci-plan",
      "tabp-admin-ci-apply",
      "tabp-admin-ci-apply-identity",
      "tabp-admin-ci-unlock-identity",
      "tabp-admin-ci-unlock",
    ]
    error_message = "role の名前が <name_prefix>-ci-<役割> になっていません。"
  }

  assert {
    condition     = [aws_iam_role.this["plan"].max_session_duration, aws_iam_role.this["apply"].max_session_duration, aws_iam_role.apply_identity.max_session_duration, aws_iam_role.this["unlock_identity"].max_session_duration, aws_iam_role.this["unlock"].max_session_duration] == [3600, 3600, 3600, 3600, 3600]
    error_message = "role のセッションの上限が1時間になっていません。"
  }
}

run "names_follow_inputs" {
  command = apply

  variables {
    name_prefix = "t4bp-dev"
    env         = "dev"
  }

  assert {
    condition     = [aws_iam_role.this["plan"].name, aws_iam_role.this["apply"].name, aws_iam_role.apply_identity.name, aws_iam_role.this["unlock_identity"].name, aws_iam_role.this["unlock"].name] == ["t4bp-dev-ci-plan", "t4bp-dev-ci-apply", "t4bp-dev-ci-apply-identity", "t4bp-dev-ci-unlock-identity", "t4bp-dev-ci-unlock"]
    error_message = "role の名前が name_prefix から組まれていません。"
  }

  assert {
    condition     = jsondecode(aws_iam_role.apply_identity.assume_role_policy).Statement[0].Condition.StringEquals["token.actions.githubusercontent.com:sub"] == "repo:octo-org@123456/octo-repo@456789:environment:dev-apply-identity"
    error_message = "信頼条件の environment が env から組まれていません。"
  }
}

# 公開 output の値は、この map の取り違えをそのまま受け継ぐ。
run "outputs_per_role" {
  command = apply

  assert {
    condition = output.role_arns == {
      plan            = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan"
      apply           = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply"
      apply_identity  = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
      unlock_identity = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"
      unlock          = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock"
    }
    error_message = "role_arns が、役割ごとにその役割の role の ARN を返していません。"
  }

  assert {
    condition = output.roles == {
      plan            = { name = "tabp-admin-ci-plan", environment = "admin-plan" }
      apply           = { name = "tabp-admin-ci-apply", environment = "admin-apply" }
      apply_identity  = { name = "tabp-admin-ci-apply-identity", environment = "admin-apply-identity" }
      unlock_identity = { name = "tabp-admin-ci-unlock-identity", environment = "admin-unlock-identity" }
      unlock          = { name = "tabp-admin-ci-unlock", environment = "admin-unlock" }
    }
    error_message = "roles が、役割ごとの role 名と environment 名を返していません。"
  }

  assert {
    condition = output.role_policies == {
      plan            = aws_iam_role_policy.this["plan"].policy
      apply           = aws_iam_role_policy.this["apply"].policy
      apply_identity  = aws_iam_role_policy.apply_identity.policy
      unlock_identity = aws_iam_role_policy.this["unlock_identity"].policy
      unlock          = aws_iam_role_policy.this["unlock"].policy
    }
    error_message = "role_policies が、役割ごとにその役割の inline policy を返していません。"
  }
}

# role_arns は Key Policy の Principal に渡る。role の置換で computed の arn が決まらない plan でも、Key Policy の
# 文書が決まるよう、role の名前から組む（plan.kms_key_policy の PLAN-KMS-09）。
run "role_arns_do_not_read_computed_arn" {
  command = plan

  override_resource {
    target = aws_iam_role.this["plan"]
    values = { arn = "arn:aws:iam::210987654321:role/computed" }
  }

  override_resource {
    target = aws_iam_role.apply_identity
    values = { arn = "arn:aws:iam::210987654321:role/computed" }
  }

  assert {
    condition     = output.role_arns["plan"] == "arn:aws:iam::123456789012:role/tabp-admin-ci-plan" && output.role_arns["apply_identity"] == "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
    error_message = "role_arns が、role の名前ではなく computed の arn から組まれています。"
  }
}
