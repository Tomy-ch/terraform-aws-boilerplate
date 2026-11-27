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

  # partition は data.aws_partition.current の postcondition が限る。
  delegated_admin_regions = module.default_regions.by_partition[data.aws_partition.current.partition]
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

# Identity Center を有効にする前は作らない（organization-baseline/ADR-0204 決定5）。
module "access" {
  source = "./internal/access"
  count  = var.identity_center == null ? 0 : 1

  project                = var.project
  partition              = data.aws_partition.current.partition
  management_account_id  = module.organization.management_account_id
  member_account_ids     = module.organization.account_ids
  management_assignments = var.identity_center.management_assignments
  member_assignments     = var.identity_center.member_assignments
  bootstrap_bucket_arns  = [var.state_bucket_arn, var.plan_output_bucket_arn, var.apply_output_bucket_arn]
  state_key_arn          = var.state_key_arn
}

# partition ごとの、既定で有効なリージョン（repository-wide/ADR-0209）。
module "default_regions" {
  source = "../../modules/default-regions"
}

data "aws_partition" "current" {
  lifecycle {
    postcondition {
      condition     = contains(keys(module.default_regions.by_partition), self.partition)
      error_message = "partition ${self.partition} は organization-baseline の対象の外です（README の unsupported）。"
    }
  }
}
