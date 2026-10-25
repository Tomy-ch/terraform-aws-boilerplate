module "state_backend" {
  source = "../../../usecases/state-backend"

  project = "tabp"
  env     = "admin"
}
