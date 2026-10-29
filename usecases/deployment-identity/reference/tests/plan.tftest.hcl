mock_provider "aws" {}

# role の ARN は plan では決まらないので、mock の apply で見る。
run "reference_roles" {
  command = apply

  assert {
    condition = length(toset([
      module.deployment_identity.plan_role_arn,
      module.deployment_identity.apply_role_arn,
      module.deployment_identity.apply_identity_role_arn,
    ])) == 3
    error_message = "reference の値から3つの role が組まれていません。"
  }
}
