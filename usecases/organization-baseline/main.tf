locals {
  break_glass_path      = "/break-glass/"
  break_glass_role_name = "${var.project}-break-glass"
  # メンバーの立ち上げの読み取りの role（organization-baseline/ADR-0203）。
  bootstrap_read_path      = "/bootstrap/"
  bootstrap_read_role_name = "${var.project}-bootstrap-read"
}

module "organization" {
  source = "./internal/organization"

  project               = var.project
  break_glass_role_name = local.break_glass_role_name
  break_glass_role_path = local.break_glass_path
  bootstrapping         = var.bootstrapping

  bootstrap_read_role_name = local.bootstrap_read_role_name
  bootstrap_read_role_path = local.bootstrap_read_path
}

module "break_glass" {
  source = "./internal/break-glass"

  role_name            = local.break_glass_role_name
  path                 = local.break_glass_path
  organization_root_id = module.organization.root_id
}

module "bootstrap_access" {
  source = "./internal/bootstrap-access"

  project               = var.project
  role_name             = local.bootstrap_read_role_name
  path                  = local.bootstrap_read_path
  trusted_principal_arn = var.bootstrap_read_principal_arn
  organization_root_id  = module.organization.root_id
}

module "account_ids" {
  source = "./internal/account-ids"

  project          = var.project
  account_ids      = module.organization.account_ids
  organization_arn = module.organization.arn
}
