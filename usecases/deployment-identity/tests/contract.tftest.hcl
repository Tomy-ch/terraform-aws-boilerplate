# 公開 output の名前と、それが返す値の配線を固定する（ADR-0202 決定4）。
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
  project                 = "tabp"
  env                     = "admin"
  github                  = { owner = "octo-org", owner_id = "123456", repository = "octo-repo", repository_id = "456789" }
  state_bucket_arn        = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an"
  state_key_arn           = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  plan_output_bucket_arn  = "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an"
  apply_output_bucket_arn = "arn:aws:s3:::tabp-admin-tfapply-123456789012-ap-northeast-1-an"
  break_glass_arns        = ["arn:aws:iam::123456789012:user/break-glass/break-glass-1", "arn:aws:iam::123456789012:user/break-glass/break-glass-2", "arn:aws:iam::123456789012:group/break-glass/break-glass", "arn:aws:iam::123456789012:role/break-glass/tabp-break-glass"]
}

run "outputs" {
  command = apply

  assert {
    condition = [output.plan_role_arn, output.apply_role_arn, output.apply_identity_role_arn, output.unlock_identity_role_arn, output.unlock_role_arn] == [
      module.github_oidc.role_arns["plan"],
      module.github_oidc.role_arns["apply"],
      module.github_oidc.role_arns["apply_identity"],
      module.github_oidc.role_arns["unlock_identity"],
      module.github_oidc.role_arns["unlock"],
    ]
    error_message = "plan_role_arn / apply_role_arn / apply_identity_role_arn / unlock_identity_role_arn / unlock_role_arn が、それぞれの役割の role の ARN を返していません。"
  }

  assert {
    condition     = length(toset([output.plan_role_arn, output.apply_role_arn, output.apply_identity_role_arn, output.unlock_identity_role_arn, output.unlock_role_arn])) == 5
    error_message = "5つの role の ARN が互いに異なっていません。"
  }
}

# 取り違えが見えるよう、上の admin とは別の値を渡す。
run "wiring" {
  command = apply

  variables {
    project                 = "t4bp"
    env                     = "dev"
    state_bucket_arn        = "arn:aws:s3:::t4bp-dev-tfstate-123456789012-ap-northeast-1-an"
    state_key_arn           = "arn:aws:kms:ap-northeast-1:123456789012:key/abcdef01-2345-6789-abcd-ef0123456789"
    plan_output_bucket_arn  = "arn:aws:s3:::t4bp-dev-tfplan-123456789012-ap-northeast-1-an"
    apply_output_bucket_arn = "arn:aws:s3:::t4bp-dev-tfapply-123456789012-ap-northeast-1-an"
    break_glass_arns        = ["arn:aws:iam::123456789012:role/break-glass/t4bp-break-glass"]
  }

  assert {
    condition = module.github_oidc.roles == {
      plan            = { name = "t4bp-dev-ci-plan", environment = "dev-plan" }
      apply           = { name = "t4bp-dev-ci-apply", environment = "dev-apply" }
      apply_identity  = { name = "t4bp-dev-ci-apply-identity", environment = "dev-apply-identity" }
      unlock_identity = { name = "t4bp-dev-ci-unlock-identity", environment = "dev-unlock-identity" }
      unlock          = { name = "t4bp-dev-ci-unlock", environment = "dev-unlock" }
    }
    error_message = "role と environment の名前が、project と env から組まれていません。"
  }

  assert {
    condition = one([for s in jsondecode(module.github_oidc.role_policies["plan"]).Statement : s.Resource if s.Sid == "ListBuckets"]) == [
      "arn:aws:s3:::t4bp-dev-tfstate-123456789012-ap-northeast-1-an",
      "arn:aws:s3:::t4bp-dev-tfplan-123456789012-ap-northeast-1-an",
      "arn:aws:s3:::t4bp-dev-tfapply-123456789012-ap-northeast-1-an",
    ]
    error_message = "state_bucket_arn・plan_output_bucket_arn・apply_output_bucket_arn が内部の module の権限へ渡っていません。"
  }

  assert {
    condition     = one([for s in jsondecode(module.github_oidc.role_policies["plan"]).Statement : s.Resource if s.Sid == "ReadStateKey"]) == "arn:aws:kms:ap-northeast-1:123456789012:key/abcdef01-2345-6789-abcd-ef0123456789"
    error_message = "state_key_arn が内部の module の権限へ渡っていません。"
  }

  assert {
    condition     = one([for s in jsondecode(module.github_oidc.role_policies["apply"]).Statement : s.Resource if s.Sid == "DenyBreakGlassChange"]) == ["arn:aws:iam::123456789012:role/break-glass/t4bp-break-glass"]
    error_message = "break_glass_arns が apply の role の Deny へ渡っていません。"
  }

  assert {
    condition     = one([for s in jsondecode(module.github_oidc.role_policies["apply"]).Statement : s.Resource if s.Sid == "WriteWorkloadApplyOutput"]) == "arn:aws:s3:::t4bp-dev-tfapply-123456789012-ap-northeast-1-an/*"
    error_message = "apply の role が、apply_output_bucket_arn の Bucket の object に apply の出力を書けません。"
  }
}
