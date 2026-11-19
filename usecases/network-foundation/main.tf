locals {
  name                     = "${var.project}-${var.env}"
  flow_logs_log_group_name = "/${var.project}/${var.env}/network-foundation/vpc-flow-logs"
}

module "kms" {
  source = "../../modules/kms"

  name = "${local.name}-network-flow-logs"
  users = {
    services = { logs = [local.flow_logs_log_group_name] }
  }
}

module "flow_logs" {
  source = "../../modules/log-group"

  name                = local.flow_logs_log_group_name
  retention_days      = var.flow_logs_retention_days
  kms_key_arn         = module.kms.key_arn
  deletion_protection = var.flow_logs_deletion_protection
}

module "vpc" {
  source = "../../modules/vpc"

  name                    = local.name
  cidr                    = var.cidr
  flow_logs_log_group_arn = module.flow_logs.log_group_arn
}

module "subnet_tiers" {
  source = "./internal/subnet-tiers"

  vpc_id                = module.vpc.vpc_id
  vpc_cidr              = module.vpc.vpc_cidr
  availability_zone_ids = var.availability_zone_ids
}
