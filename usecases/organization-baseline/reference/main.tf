provider "aws" {
  region = "ap-northeast-1"
}

module "organization_baseline" {
  source = "../"

  project = "tabp"
}
