module "state_backend" {
  source = "../../../usecases/state-backend"

  project = "tabp"
  env     = "admin"
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
  state_bucket_arn = module.state_backend.bucket_arn
}
