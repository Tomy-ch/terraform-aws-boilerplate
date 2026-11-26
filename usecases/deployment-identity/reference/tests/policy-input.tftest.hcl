# Policy Test の入力を作る（make tf-policy）。ARN を与える理由は conftest/plan/README.md の「入力」。
mock_provider "aws" {
  override_during = plan

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
  target          = module.deployment_identity.module.github_oidc.aws_iam_role.apply_identity
  override_during = plan
  values          = { arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity" }
}

override_resource {
  target          = module.deployment_identity.module.github_oidc.aws_iam_role.this["unlock_identity"]
  override_during = plan
  values          = { arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity" }
}

override_resource {
  target          = module.deployment_identity.module.github_oidc.aws_iam_role.this["unlock"]
  override_during = plan
  values          = { arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock" }
}

override_resource {
  target          = module.deployment_identity.module.github_oidc.aws_iam_policy.manage
  override_during = plan
  values          = { arn = "arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-identity-manage" }
}

override_resource {
  target          = module.deployment_identity.module.github_oidc.aws_iam_policy.governance[0]
  override_during = plan
  values          = { arn = "arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-identity-governance" }
}

override_resource {
  target          = module.deployment_identity.module.github_oidc.aws_iam_policy.members[0]
  override_during = plan
  values          = { arn = "arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-identity-members" }
}

override_resource {
  target          = module.deployment_identity.module.github_oidc.aws_iam_policy.workload
  override_during = plan
  values          = { arn = "arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-workload" }
}

override_resource {
  target          = module.deployment_identity.module.github_oidc.aws_iam_policy.workload_read
  override_during = plan
  values          = { arn = "arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-workload-read" }
}

override_resource {
  target          = module.deployment_identity.module.github_oidc.aws_iam_policy.workload_2
  override_during = plan
  values          = { arn = "arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-workload-2" }
}

override_resource {
  target          = module.deployment_identity.module.github_oidc.aws_iam_policy.boundary["ops-notification"]
  override_during = plan
  values          = { arn = "arn:aws:iam::123456789012:policy/tabp-admin-ci-boundary-ops-notification" }
}

override_resource {
  target          = module.deployment_identity.module.service_linked_roles.aws_iam_service_linked_role.chatbot[0]
  override_during = plan
  values          = { arn = "arn:aws:iam::123456789012:role/aws-service-role/management.chatbot.amazonaws.com/AWSServiceRoleForAWSChatbot" }
}

run "policy_input" {
  command = plan
}
