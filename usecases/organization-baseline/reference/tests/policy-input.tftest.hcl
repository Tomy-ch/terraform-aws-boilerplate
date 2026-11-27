# Policy Test の入力を作る（make tf-policy）。ID と ARN を与える理由は conftest/plan/README.md の「入力」。
mock_provider "aws" {
  mock_data "aws_ssm_parameter" {
    defaults = { value = "{\"security\":\"security@example.com\",\"log-archive\":\"log-archive@example.com\"}" }
  }
  override_during = plan

  mock_data "aws_caller_identity" {
    defaults = { account_id = "111111111111" }
  }
  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }
  mock_data "aws_region" {
    defaults = { region = "ap-northeast-1" }
  }
}

override_resource {
  target          = module.organization_baseline.module.organization.aws_organizations_organization.this
  override_during = plan
  values = {
    id                = "o-exampleorg1"
    arn               = "arn:aws:organizations::111111111111:organization/o-exampleorg1"
    master_account_id = "111111111111"
    roots             = [{ id = "r-exam", arn = "arn:aws:organizations::111111111111:root/o-exampleorg1/r-exam", name = "Root", policy_types = [] }]
  }
}

override_resource {
  target          = module.organization_baseline.module.organization.aws_organizations_organizational_unit.top["Security"]
  override_during = plan
  values          = { id = "ou-exam-security", arn = "arn:aws:organizations::111111111111:ou/o-exampleorg1/ou-exam-security" }
}

override_resource {
  target          = module.organization_baseline.module.organization.aws_organizations_organizational_unit.top["Workloads"]
  override_during = plan
  values          = { id = "ou-exam-workload", arn = "arn:aws:organizations::111111111111:ou/o-exampleorg1/ou-exam-workload" }
}

override_resource {
  target          = module.organization_baseline.module.organization.aws_organizations_organizational_unit.top["Sandbox"]
  override_during = plan
  values          = { id = "ou-exam-sandboxx", arn = "arn:aws:organizations::111111111111:ou/o-exampleorg1/ou-exam-sandboxx" }
}

override_resource {
  target          = module.organization_baseline.module.organization.aws_organizations_organizational_unit.top["PolicyStaging"]
  override_during = plan
  values          = { id = "ou-exam-staginga", arn = "arn:aws:organizations::111111111111:ou/o-exampleorg1/ou-exam-staginga" }
}

override_resource {
  target          = module.organization_baseline.module.organization.aws_organizations_organizational_unit.workloads["Prod"]
  override_during = plan
  values          = { id = "ou-exam-prodprod", arn = "arn:aws:organizations::111111111111:ou/o-exampleorg1/ou-exam-prodprod" }
}

override_resource {
  target          = module.organization_baseline.module.organization.aws_organizations_organizational_unit.workloads["NonProd"]
  override_during = plan
  values          = { id = "ou-exam-nonprodx", arn = "arn:aws:organizations::111111111111:ou/o-exampleorg1/ou-exam-nonprodx" }
}

override_resource {
  target          = module.organization_baseline.module.break_glass.aws_iam_user.this["break-glass-1"]
  override_during = plan
  values          = { arn = "arn:aws:iam::111111111111:user/break-glass/break-glass-1" }
}

override_resource {
  target          = module.organization_baseline.module.break_glass.aws_iam_user.this["break-glass-2"]
  override_during = plan
  values          = { arn = "arn:aws:iam::111111111111:user/break-glass/break-glass-2" }
}

override_data {
  override_during = plan
  target          = module.organization_baseline.module.access[0].data.aws_ssoadmin_instances.this
  values          = { arns = ["arn:aws:sso:::instance/ssoins-0000000000000000"], identity_store_ids = ["d-0000000000"] }
}

override_data {
  override_during = plan
  target          = module.organization_baseline.module.access[0].data.aws_ssm_parameter.principals
  values          = { value = "{\"users\":{\"owner\":\"00000000-0000-0000-0000-000000000001\"},\"groups\":{\"operators\":\"00000000-0000-0000-0000-000000000002\"}}" }
}

# 付与・inline・割当の permission_set_arn と、割当の target_id を plan で決め、付く policy と対象のアカウントで判定させる。
override_resource {
  target          = module.organization_baseline.module.organization.aws_organizations_account.this["security"]
  override_during = plan
  values          = { id = "222222222222" }
}

override_resource {
  target          = module.organization_baseline.module.organization.aws_organizations_account.this["log-archive"]
  override_during = plan
  values          = { id = "333333333333" }
}

override_resource {
  target          = module.organization_baseline.module.access[0].aws_ssoadmin_permission_set.this["read_only"]
  override_during = plan
  values          = { arn = "arn:aws:sso:::permissionSet/ssoins-0000000000000000/ps-000000000000000a" }
}

override_resource {
  target          = module.organization_baseline.module.access[0].aws_ssoadmin_permission_set.this["terraform_plan"]
  override_during = plan
  values          = { arn = "arn:aws:sso:::permissionSet/ssoins-0000000000000000/ps-000000000000000b" }
}

override_resource {
  target          = module.organization_baseline.module.access[0].aws_ssoadmin_permission_set.this["billing"]
  override_during = plan
  values          = { arn = "arn:aws:sso:::permissionSet/ssoins-0000000000000000/ps-000000000000000c" }
}

override_resource {
  target          = module.organization_baseline.module.access[0].aws_ssoadmin_permission_set.this["support"]
  override_during = plan
  values          = { arn = "arn:aws:sso:::permissionSet/ssoins-0000000000000000/ps-000000000000000d" }
}

override_resource {
  target          = module.organization_baseline.module.access[0].aws_ssoadmin_permission_set.this["organization_admin"]
  override_during = plan
  values          = { arn = "arn:aws:sso:::permissionSet/ssoins-0000000000000000/ps-000000000000000e" }
}

# 付け先の policy_id を plan で決め、付け先の宣言と突き合わせる。
override_resource {
  target          = module.organization_baseline.module.organization.aws_organizations_policy.this["deny-leave-and-close"]
  override_during = plan
  values          = { id = "p-denyleaveandclose" }
}

override_resource {
  target          = module.organization_baseline.module.organization.aws_organizations_policy.this["deny-bootstrap-role"]
  override_during = plan
  values          = { id = "p-denybootstraprole" }
}

override_resource {
  target          = module.organization_baseline.module.organization.aws_organizations_policy.this["deny-state-write"]
  override_during = plan
  values          = { id = "p-denystatewrite" }
}

override_resource {
  target          = module.organization_baseline.module.organization.aws_organizations_policy.this["protect-governance-roles"]
  override_during = plan
  values          = { id = "p-protectgovernancerol" }
}

override_resource {
  target          = module.organization_baseline.module.organization.aws_organizations_policy.this["deny-guardduty-stop"]
  override_during = plan
  values          = { id = "p-denyguarddutystop" }
}

override_resource {
  target          = module.organization_baseline.module.organization.aws_organizations_policy.this["deny-root-user"]
  override_during = plan
  values          = { id = "p-denyrootuser" }
}

override_resource {
  target          = module.organization_baseline.module.organization.aws_organizations_policy.this["deny-iam-user"]
  override_during = plan
  values          = { id = "p-denyiamuser" }
}

override_resource {
  target          = module.organization_baseline.module.organization.aws_organizations_policy.this["deny-baseline-stop"]
  override_during = plan
  values          = { id = "p-denybaselinestop" }
}

override_resource {
  target          = module.organization_baseline.module.organization.aws_organizations_policy.this["deny-enable-region"]
  override_during = plan
  values          = { id = "p-denyenableregion" }
}

override_resource {
  target          = module.organization_baseline.module.organization.aws_organizations_policy.this["data-perimeter-prod-resources"]
  override_during = plan
  values          = { id = "p-dataperimeterprodres" }
}

override_resource {
  target          = module.organization_baseline.module.organization.aws_organizations_policy.this["data-perimeter-org-principals"]
  override_during = plan
  values          = { id = "p-dataperimeterorgprin" }
}

override_resource {
  target          = module.organization_baseline.module.organization.aws_organizations_policy.this["data-perimeter-prod-principals"]
  override_during = plan
  values          = { id = "p-dataperimeterprodpri" }
}

override_resource {
  target          = module.organization_baseline.module.organization.aws_organizations_policy.this["ec2-defaults"]
  override_during = plan
  values          = { id = "p-ec2defaults" }
}

run "policy_input" {
  command = plan
}
