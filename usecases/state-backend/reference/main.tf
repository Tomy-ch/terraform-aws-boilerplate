provider "aws" {
  region = "ap-northeast-1"
}

module "state_backend" {
  source = "../"

  project                                 = "tabp"
  env                                     = "admin"
  state_noncurrent_version_retention_days = 90
  ci_role_arns = {
    plan            = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan"
    apply_identity  = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
    unlock_identity = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"
  }
}
