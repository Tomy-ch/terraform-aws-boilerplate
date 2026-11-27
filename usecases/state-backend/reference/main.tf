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
    apply           = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply"
    apply_identity  = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
    unlock_identity = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"
    unlock          = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock"
  }
}

provider "aws" {
  alias  = "security"
  region = "ap-northeast-1"
}

module "state_backend_security" {
  source = "../"
  providers = {
    aws = aws.security
  }

  project                                 = "tabp"
  env                                     = "security"
  state_noncurrent_version_retention_days = 90
  ci_role_arns = {
    plan            = "arn:aws:iam::111111111111:role/tabp-security-ci-plan"
    apply           = "arn:aws:iam::111111111111:role/tabp-security-ci-apply"
    apply_identity  = "arn:aws:iam::111111111111:role/tabp-security-ci-apply-identity"
    unlock_identity = "arn:aws:iam::111111111111:role/tabp-security-ci-unlock-identity"
    unlock          = "arn:aws:iam::111111111111:role/tabp-security-ci-unlock"
  }
}
