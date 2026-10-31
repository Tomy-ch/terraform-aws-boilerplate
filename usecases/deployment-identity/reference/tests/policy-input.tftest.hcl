# Policy Test の入力を作る（make tf-policy）。ARN を与える理由は conftest/plan/README.md の「入力」。
mock_provider "aws" {
  override_during = plan
}

override_resource {
  target          = module.deployment_identity.module.github_oidc.aws_iam_openid_connect_provider.github
  override_during = plan
  values          = { arn = "arn:aws:iam::123456789012:oidc-provider/token.actions.githubusercontent.com" }
}

override_resource {
  target          = module.deployment_identity.module.github_oidc.aws_iam_role.this["plan"]
  override_during = plan
  values          = { arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan" }
}

override_resource {
  target          = module.deployment_identity.module.github_oidc.aws_iam_role.this["apply"]
  override_during = plan
  values          = { arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply" }
}

override_resource {
  target          = module.deployment_identity.module.github_oidc.aws_iam_role.this["apply_identity"]
  override_during = plan
  values          = { arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity" }
}

run "policy_input" {
  command = plan
}
