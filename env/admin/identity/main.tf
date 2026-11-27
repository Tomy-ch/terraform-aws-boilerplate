module "state_backend" {
  source = "../../../usecases/state-backend"

  project                                 = "tabp"
  env                                     = "admin"
  state_noncurrent_version_retention_days = 90
  ci_role_arns = {
    plan            = module.deployment_identity.plan_role_arn
    apply           = module.deployment_identity.apply_role_arn
    apply_identity  = module.deployment_identity.apply_identity_role_arn
    unlock_identity = module.deployment_identity.unlock_identity_role_arn
    unlock          = module.deployment_identity.unlock_role_arn
  }
}

module "deployment_identity" {
  source = "../../../usecases/deployment-identity"

  project = "tabp"
  env     = "admin"
  github = {
    owner         = "Tomy-ch"
    owner_id      = "182787471"
    repository    = "terraform-aws-boilerplate"
    repository_id = "1370808564"
  }
  state_bucket_arn        = module.state_backend.bucket_arn
  state_key_arn           = module.state_backend.state_key_arn
  plan_output_bucket_arn  = module.state_backend.plan_output_bucket_arn
  apply_output_bucket_arn = module.state_backend.apply_output_bucket_arn
  break_glass_arns        = module.organization_baseline.break_glass_arns

  bootstrap_read_role          = module.organization_baseline.bootstrap_read_role
  bootstrap_member_account_ids = module.organization_baseline.bootstrapping_account_ids
}

module "organization_baseline" {
  source = "../../../usecases/organization-baseline"

  project                      = "tabp"
  bootstrap_read_principal_arn = module.deployment_identity.plan_role_arn
  # 立ち上げを終えたアカウントを外す PR で、OrganizationAccountAccessRole が使えなくなる（docs/setup/organization-baseline.md）。
  bootstrapping = ["security", "log-archive"]

  state_bucket_arn        = module.state_backend.bucket_arn
  plan_output_bucket_arn  = module.state_backend.plan_output_bucket_arn
  apply_output_bucket_arn = module.state_backend.apply_output_bucket_arn
  state_key_arn           = module.state_backend.state_key_arn

  identity_center = local.identity_center
}

locals {
  # user と group は人が付けた label で、ID は管理アカウントの parameter が持つ（docs/setup/organization-baseline.md 3.3）。
  identity_center = {
    management_assignments = [
      for p in ["read_only", "terraform_plan", "billing", "support", "organization_admin"] : { permission_set = p, user = "owner" }
    ]
    member_assignments = [
      for pair in setproduct(["security", "log-archive"], ["read_only", "terraform_plan", "billing", "support"]) :
      { account = pair[0], permission_set = pair[1], group = "operators" }
    ]
  }
}

module "state_backend_security" {
  source = "../../../usecases/state-backend"
  providers = {
    aws = aws.security
  }

  project                                 = "tabp"
  env                                     = "security"
  state_noncurrent_version_retention_days = 90
  ci_role_arns = {
    plan            = module.deployment_identity_security.plan_role_arn
    apply           = module.deployment_identity_security.apply_role_arn
    apply_identity  = module.deployment_identity_security.apply_identity_role_arn
    unlock_identity = module.deployment_identity_security.unlock_identity_role_arn
    unlock          = module.deployment_identity_security.unlock_role_arn
  }
}

module "deployment_identity_security" {
  source = "../../../usecases/deployment-identity"
  providers = {
    aws = aws.security
  }

  project = "tabp"
  env     = "security"
  github = {
    owner         = "Tomy-ch"
    owner_id      = "182787471"
    repository    = "terraform-aws-boilerplate"
    repository_id = "1370808564"
  }
  state_bucket_arn        = module.state_backend_security.bucket_arn
  state_key_arn           = module.state_backend_security.state_key_arn
  plan_output_bucket_arn  = module.state_backend_security.plan_output_bucket_arn
  apply_output_bucket_arn = module.state_backend_security.apply_output_bucket_arn
  break_glass_arns        = [local.member_break_glass_arns["security"]]
}

module "state_backend_log_archive" {
  source = "../../../usecases/state-backend"
  providers = {
    aws = aws.log_archive
  }

  project                                 = "tabp"
  env                                     = "log-archive"
  state_noncurrent_version_retention_days = 90
  ci_role_arns = {
    plan            = module.deployment_identity_log_archive.plan_role_arn
    apply           = module.deployment_identity_log_archive.apply_role_arn
    apply_identity  = module.deployment_identity_log_archive.apply_identity_role_arn
    unlock_identity = module.deployment_identity_log_archive.unlock_identity_role_arn
    unlock          = module.deployment_identity_log_archive.unlock_role_arn
  }
}

module "deployment_identity_log_archive" {
  source = "../../../usecases/deployment-identity"
  providers = {
    aws = aws.log_archive
  }

  project = "tabp"
  env     = "log-archive"
  github = {
    owner         = "Tomy-ch"
    owner_id      = "182787471"
    repository    = "terraform-aws-boilerplate"
    repository_id = "1370808564"
  }
  state_bucket_arn        = module.state_backend_log_archive.bucket_arn
  state_key_arn           = module.state_backend_log_archive.state_key_arn
  plan_output_bucket_arn  = module.state_backend_log_archive.plan_output_bucket_arn
  apply_output_bucket_arn = module.state_backend_log_archive.apply_output_bucket_arn
  break_glass_arns        = [local.member_break_glass_arns["log-archive"]]
}

locals {
  member_break_glass_arns = {
    for env, id in module.organization_baseline.bootstrapping_account_ids :
    env => "arn:${data.aws_partition.current.partition}:iam::${id}:role${module.organization_baseline.member_break_glass_role.path}${module.organization_baseline.member_break_glass_role.name}"
  }
}
