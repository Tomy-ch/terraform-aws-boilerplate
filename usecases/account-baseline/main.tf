locals {
  name_prefix = "${var.project}-${var.env}"

  # partition ごとの、既定で有効なリージョン（opt-in でないもの）。既定のリージョンは無効にできないので、集合はこれより縮まない（account-baseline/ADR-0101）。
  default_regions = {
    aws = toset([
      "ap-northeast-1", "ap-northeast-2", "ap-northeast-3", "ap-south-1", "ap-southeast-1", "ap-southeast-2",
      "ca-central-1", "eu-central-1", "eu-north-1", "eu-west-1", "eu-west-2", "eu-west-3", "sa-east-1",
      "us-east-1", "us-east-2", "us-west-1", "us-west-2",
    ])
  }
}

data "aws_partition" "current" {
  lifecycle {
    postcondition {
      condition     = contains(keys(local.default_regions), self.partition)
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
  regions     = local.default_regions[data.aws_partition.current.partition]
}

# sandbox は AWS Config を常駐させない（#86、account-baseline/ADR-0102）。
module "config_recorder" {
  source = "./internal/config-recorder"
  count  = var.env == "sandbox" ? 0 : 1

  name_prefix    = local.name_prefix
  regions        = local.default_regions[data.aws_partition.current.partition]
  delivery       = var.config_delivery
  retention_days = var.config_history_retention_days
}
