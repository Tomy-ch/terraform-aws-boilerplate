mock_provider "aws" {}

variables {
  name_prefix      = "tabp-admin"
  env              = "admin"
  github           = { owner = "octo-org", owner_id = "123456", repository = "octo-repo", repository_id = "456789" }
  state_bucket_arn = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an"
}

# assert に for 式を書かない。Terraform 1.16.2 は、for 式の assert が落ちると診断を出す前に panic する。

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
    condition = jsondecode(aws_iam_role.this["apply_identity"].assume_role_policy) == {
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
}

run "role_names" {
  command = plan

  assert {
    condition = [aws_iam_role.this["plan"].name, aws_iam_role.this["apply"].name, aws_iam_role.this["apply_identity"].name] == [
      "tabp-admin-ci-plan",
      "tabp-admin-ci-apply",
      "tabp-admin-ci-apply-identity",
    ]
    error_message = "role の名前が <name_prefix>-ci-<役割> になっていません。"
  }

  assert {
    condition     = [aws_iam_role.this["plan"].max_session_duration, aws_iam_role.this["apply"].max_session_duration, aws_iam_role.this["apply_identity"].max_session_duration] == [3600, 3600, 3600]
    error_message = "role のセッションの上限が1時間になっていません。"
  }
}

# role と environment の名前が、admin に固定されず入力から組まれることを見る。
run "names_follow_inputs" {
  command = apply

  variables {
    name_prefix = "t4bp-dev"
    env         = "dev"
  }

  assert {
    condition     = [aws_iam_role.this["plan"].name, aws_iam_role.this["apply"].name, aws_iam_role.this["apply_identity"].name] == ["t4bp-dev-ci-plan", "t4bp-dev-ci-apply", "t4bp-dev-ci-apply-identity"]
    error_message = "role の名前が name_prefix から組まれていません。"
  }

  assert {
    condition     = jsondecode(aws_iam_role.this["apply_identity"].assume_role_policy).Statement[0].Condition.StringEquals["token.actions.githubusercontent.com:sub"] == "repo:octo-org@123456/octo-repo@456789:environment:dev-apply-identity"
    error_message = "信頼条件の environment が env から組まれていません。"
  }
}

# 公開 output の値は、この map の取り違えをそのまま受け継ぐ。
run "outputs_per_role" {
  command = apply

  assert {
    condition = output.role_arns == {
      plan           = aws_iam_role.this["plan"].arn
      apply          = aws_iam_role.this["apply"].arn
      apply_identity = aws_iam_role.this["apply_identity"].arn
    }
    error_message = "role_arns が、役割ごとにその役割の role の ARN を返していません。"
  }

  assert {
    condition = output.roles == {
      plan           = { name = "tabp-admin-ci-plan", environment = "admin-plan" }
      apply          = { name = "tabp-admin-ci-apply", environment = "admin-apply" }
      apply_identity = { name = "tabp-admin-ci-apply-identity", environment = "admin-apply-identity" }
    }
    error_message = "roles が、役割ごとの role 名と environment 名を返していません。"
  }

  assert {
    condition = output.role_policies == {
      plan           = aws_iam_role_policy.this["plan"].policy
      apply          = aws_iam_role_policy.this["apply"].policy
      apply_identity = aws_iam_role_policy.this["apply_identity"].policy
    }
    error_message = "role_policies が、役割ごとにその役割の inline policy を返していません。"
  }
}
