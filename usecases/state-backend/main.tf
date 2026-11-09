locals {
  # state Bucket と state の鍵の alias は同じ名前を持つ。
  state_name = "${var.project}-${var.env}-tfstate"

  state_key_users = {
    roles = [
      { arn = var.ci_role_arns.plan, via = "s3" },
      { arn = var.ci_role_arns.apply_identity, via = "s3" },
    ]
  }
}

module "s3" {
  source = "../../modules/s3"

  name                              = local.state_name
  noncurrent_version_retention_days = var.state_noncurrent_version_retention_days
}

module "kms" {
  source = "../../modules/kms"

  name  = local.state_name
  users = local.state_key_users
}

module "plan_output" {
  source = "./internal/plan-output"

  name           = "${var.project}-${var.env}-tfplan"
  retention_days = var.plan_output_retention_days
}
