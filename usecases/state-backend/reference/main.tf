provider "aws" {
  region = "ap-northeast-1"
}

module "state_backend" {
  source = "../"

  project = "tabp"
  env     = "admin"
}
