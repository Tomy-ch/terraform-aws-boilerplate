# 公開 output の名前と、それが返す値の配線を固定する（ADR-0202 決定4）。
mock_provider "aws" {
  # state_key_arn と access_log_* の precondition は、ARN を data source の partition・region・account と比べる。
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
  project                = "tabp"
  env                    = "admin"
  github                 = { owner = "octo-org", owner_id = "123456", repository = "octo-repo", repository_id = "456789" }
  state_bucket_arn       = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an"
  state_key_arn          = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  plan_output_bucket_arn = "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an"

  access_log_group_arn       = "arn:aws:logs:ap-northeast-1:123456789012:log-group:tabp-admin-state-backend-access"
  access_log_destination_arn = "arn:aws:logs:ap-northeast-1:123456789012:delivery-destination:tabp-admin-state-backend-access"
  access_log_source_arns = [
    "arn:aws:logs:ap-northeast-1:123456789012:delivery-source:tabp-admin-tfstate",
    "arn:aws:logs:ap-northeast-1:123456789012:delivery-source:tabp-admin-tfplan",
  ]
}

run "outputs" {
  command = apply

  assert {
    condition = [output.plan_role_arn, output.apply_role_arn, output.apply_identity_role_arn, output.unlock_identity_role_arn] == [
      module.github_oidc.role_arns["plan"],
      module.github_oidc.role_arns["apply"],
      module.github_oidc.role_arns["apply_identity"],
      module.github_oidc.role_arns["unlock_identity"],
    ]
    error_message = "plan_role_arn / apply_role_arn / apply_identity_role_arn / unlock_identity_role_arn が、それぞれの役割の role の ARN を返していません。"
  }

  assert {
    condition     = length(toset([output.plan_role_arn, output.apply_role_arn, output.apply_identity_role_arn, output.unlock_identity_role_arn])) == 4
    error_message = "4つの role の ARN が互いに異なっていません。"
  }
}

# 取り違えが見えるよう、上の admin とは別の値を渡す。
run "wiring" {
  command = apply

  variables {
    project                = "t4bp"
    env                    = "dev"
    state_bucket_arn       = "arn:aws:s3:::t4bp-dev-tfstate-123456789012-ap-northeast-1-an"
    state_key_arn          = "arn:aws:kms:ap-northeast-1:123456789012:key/abcdef01-2345-6789-abcd-ef0123456789"
    plan_output_bucket_arn = "arn:aws:s3:::t4bp-dev-tfplan-123456789012-ap-northeast-1-an"

    access_log_group_arn       = "arn:aws:logs:ap-northeast-1:123456789012:log-group:t4bp-dev-state-backend-access"
    access_log_destination_arn = "arn:aws:logs:ap-northeast-1:123456789012:delivery-destination:t4bp-dev-state-backend-access"
    access_log_source_arns     = ["arn:aws:logs:ap-northeast-1:123456789012:delivery-source:t4bp-dev-tfstate", "arn:aws:logs:ap-northeast-1:123456789012:delivery-source:t4bp-dev-tfplan"]
  }

  assert {
    condition = module.github_oidc.roles == {
      plan            = { name = "t4bp-dev-ci-plan", environment = "dev-plan" }
      apply           = { name = "t4bp-dev-ci-apply", environment = "dev-apply" }
      apply_identity  = { name = "t4bp-dev-ci-apply-identity", environment = "dev-apply-identity" }
      unlock_identity = { name = "t4bp-dev-ci-unlock-identity", environment = "dev-unlock-identity" }
    }
    error_message = "role と environment の名前が、project と env から組まれていません。"
  }

  assert {
    condition     = jsondecode(module.github_oidc.role_policies["plan"]).Statement[0].Resource == "arn:aws:s3:::t4bp-dev-tfstate-123456789012-ap-northeast-1-an"
    error_message = "state_bucket_arn が内部の module の権限へ渡っていません。"
  }

  assert {
    condition     = jsondecode(module.github_oidc.role_policies["plan"]).Statement[3].Resource == "arn:aws:s3:::t4bp-dev-tfplan-123456789012-ap-northeast-1-an"
    error_message = "plan_output_bucket_arn が内部の module の権限へ渡っていません。"
  }

  assert {
    condition     = jsondecode(module.github_oidc.role_policies["plan"]).Statement[6].Resource == "arn:aws:kms:ap-northeast-1:123456789012:key/abcdef01-2345-6789-abcd-ef0123456789"
    error_message = "state_key_arn が内部の module の権限へ渡っていません。"
  }

  assert {
    condition = [
      jsondecode(module.github_oidc.role_policies["plan"]).Statement[10].Resource,
      jsondecode(module.github_oidc.role_policies["plan"]).Statement[13].Resource,
      jsondecode(module.github_oidc.role_policies["plan"]).Statement[14].Resource,
      ] == [
      "arn:aws:logs:ap-northeast-1:123456789012:log-group:t4bp-dev-state-backend-access",
      "arn:aws:logs:ap-northeast-1:123456789012:delivery-destination:t4bp-dev-state-backend-access",
      ["arn:aws:logs:ap-northeast-1:123456789012:delivery-source:t4bp-dev-tfstate", "arn:aws:logs:ap-northeast-1:123456789012:delivery-source:t4bp-dev-tfplan"],
    ]
    error_message = "access_log_group_arn・access_log_destination_arn・access_log_source_arns が内部の module の権限へ渡っていません。"
  }
}
