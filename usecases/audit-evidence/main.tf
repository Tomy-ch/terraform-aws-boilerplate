data "aws_partition" "current" {}

locals {
  name            = "${var.project}-${var.env}-audit"
  access_log_name = "${local.name}-access-log"
  trail           = var.writers.cloudtrail_organization_trail

  # 保持の使い分けの根拠は audit-evidence/ADR-0101。
  retention = var.env == "sandbox" ? { mode = "GOVERNANCE", days = 1 } : { mode = "COMPLIANCE", days = var.retention_days }
}

module "archive_key" {
  source = "../../modules/kms"

  name = local.name
  users = {
    services = {
      cloudtrail = ["arn:${data.aws_partition.current.partition}:cloudtrail:${local.trail.region}:${local.trail.owner_account_id}:trail/${local.trail.trail_name}"]
      config     = sort(var.writers.config_source_accounts)
    }
  }
}

# CloudWatch Logs は Log Group ごとに別の鍵を勧める（CloudWatch Logs User Guide「Encrypt log data using AWS KMS」）。
module "access_log_key" {
  source = "../../modules/kms"

  name  = local.access_log_name
  users = { services = { logs = [local.access_log_name] } }
}

module "access_log" {
  source = "../../modules/vended-log-group"

  name                = local.access_log_name
  retention_days      = var.access_log_retention_days
  kms_key_arn         = module.access_log_key.key_arn
  deletion_protection = var.env != "sandbox"
  delivery_sources    = [local.access_log_name]
}

module "archive" {
  source = "./internal/archive-bucket"

  name                       = local.name
  kms_key_arn                = module.archive_key.key_arn
  retention                  = local.retention
  force_destroy              = var.env == "sandbox"
  writers                    = var.writers
  access_log_source_name     = local.access_log_name
  access_log_destination_arn = module.access_log.delivery_destination_arn
  notify_topic_arn           = var.notify_topic_arn
}
