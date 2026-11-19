locals {
  break_glass_path      = "/break-glass/"
  break_glass_role_name = "${var.project}-break-glass"
}

module "organization" {
  source = "./internal/organization"

  project               = var.project
  break_glass_role_name = local.break_glass_role_name
  break_glass_role_path = local.break_glass_path
}

module "break_glass" {
  source = "./internal/break-glass"

  role_name = local.break_glass_role_name
  path      = local.break_glass_path
}
