# 公開 output の名前と、それが返す値の配線を固定する。変えるなら公開契約の変更として扱う（ADR-0202 決定4）。
mock_provider "aws" {}

variables {
  project          = "tabp"
  env              = "admin"
  github           = { owner = "octo-org", owner_id = "123456", repository = "octo-repo", repository_id = "456789" }
  state_bucket_arn = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an"
}

run "outputs" {
  command = apply

  assert {
    condition = [output.plan_role_arn, output.apply_role_arn, output.apply_identity_role_arn] == [
      module.github_oidc.role_arns["plan"],
      module.github_oidc.role_arns["apply"],
      module.github_oidc.role_arns["apply_identity"],
    ]
    error_message = "plan_role_arn / apply_role_arn / apply_identity_role_arn が、それぞれの役割の role の ARN を返していません。"
  }

  assert {
    condition     = length(toset([output.plan_role_arn, output.apply_role_arn, output.apply_identity_role_arn])) == 3
    error_message = "3つの role の ARN が互いに異なっていません。"
  }
}
