provider "aws" {
  region = "ap-northeast-1"
}

module "network_foundation" {
  source = "../"

  project                       = "tabp"
  env                           = "prd"
  cidr                          = "10.0.0.0/16"
  availability_zone_ids         = ["apne1-az1", "apne1-az2", "apne1-az4"]
  flow_logs_retention_days      = 365
  flow_logs_deletion_protection = true
}
