module "state_backend" {
  source = "../../../usecases/state-backend"

  project                                 = "tabp"
  env                                     = "admin"
  state_noncurrent_version_retention_days = 90
  ci_role_arns = {
    plan           = module.deployment_identity.plan_role_arn
    apply_identity = module.deployment_identity.apply_identity_role_arn
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
  state_bucket_arn       = module.state_backend.bucket_arn
  state_key_alias_name   = module.state_backend.state_key_alias_name
  plan_output_bucket_arn = module.state_backend.plan_output_bucket_arn
}
