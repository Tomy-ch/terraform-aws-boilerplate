locals {
  name_prefix = "${var.project}-${var.env}"
}

# partition ごとの、既定で有効なリージョン（repository-wide/ADR-0209）。
module "default_regions" {
  source = "../../modules/default-regions"
}

data "aws_partition" "current" {
  lifecycle {
    postcondition {
      condition     = contains(keys(module.default_regions.by_partition), self.partition)
      error_message = "partition ${self.partition} は account-baseline の対象の外です（README の unsupported）。"
    }
  }
}

module "account_controls" {
  source = "./internal/account-controls"

  security_contact = var.security_contact
  # 管理アカウントの password policy は organization-baseline の break-glass が持つ。
  manage_password_policy = var.env != "admin"
}

module "regional_controls" {
  source = "./internal/regional-controls"

  name_prefix = local.name_prefix
  regions     = module.default_regions.by_partition[data.aws_partition.current.partition]
}

# sandbox の例外は公式推奨からの逸脱（account-baseline/ADR-0102 決定6）。
module "config_recorder" {
  source = "./internal/config-recorder"
  count  = var.env == "sandbox" ? 0 : 1

  name_prefix    = local.name_prefix
  regions        = module.default_regions.by_partition[data.aws_partition.current.partition]
  delivery       = var.config_delivery
  retention_days = var.config_history_retention_days
}
