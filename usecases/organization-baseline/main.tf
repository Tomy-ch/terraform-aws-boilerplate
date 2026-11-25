locals {
  break_glass_path      = "/break-glass/"
  break_glass_role_name = "${var.project}-break-glass"
}

module "organization" {
  source = "./internal/organization"

  project               = var.project
  break_glass_role_name = local.break_glass_role_name
  break_glass_role_path = local.break_glass_path
  bootstrapping         = var.bootstrapping
  # partition は data.aws_partition.current の postcondition が限る。
  delegated_admin_regions = module.default_regions.by_partition[data.aws_partition.current.partition]
}

module "break_glass" {
  source = "./internal/break-glass"

  role_name            = local.break_glass_role_name
  path                 = local.break_glass_path
  organization_root_id = module.organization.root_id
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
