locals {
  state_name = "${var.project}-${var.env}-tfstate"
  # identity root の lock file。backend.tf の key と、deployment-identity の unlock-identity の IAM の Resource が同じ key を持つ。
  identity_lock_key = "identity/terraform.tfstate.tflock"
}

module "s3" {
  source = "../../modules/s3"

  name                              = local.state_name
  noncurrent_version_retention_days = var.state_noncurrent_version_retention_days
  kms_key_arn                       = module.kms.key_arn
}

module "kms" {
  source = "../../modules/kms"

  name = local.state_name
  users = {
    roles = [
      { arn = var.ci_role_arns.plan, via = "s3", access = "read-write" },
      { arn = var.ci_role_arns.apply_identity, via = "s3", access = "read-write" },
      { arn = var.ci_role_arns.unlock_identity, via = "s3", access = "read", objects = ["${module.s3.bucket_arn}/${local.identity_lock_key}"] },
    ]
  }
}

module "plan_output" {
  source = "./internal/plan-output"

  name           = "${var.project}-${var.env}-tfplan"
  retention_days = var.plan_output_retention_days
  kms_key_arn    = module.kms.key_arn
}
