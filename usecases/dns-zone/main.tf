locals {
  name      = "${var.project}-${var.env}"
  zone_name = var.env == "prd" ? var.apex_domain : "${var.env}.${var.apex_domain}"
}

module "query_log" {
  source = "./internal/query-log"

  name                = local.name
  zone_name           = local.zone_name
  retention_days      = var.query_log_retention_days
  deletion_protection = var.query_log_deletion_protection
}

module "dnssec_key" {
  source = "./internal/dnssec-key"

  name      = "${local.name}-dns-dnssec"
  zone_name = local.zone_name
}

module "zone" {
  source = "./internal/zone"

  name                = local.name
  zone_name           = local.zone_name
  query_log_group_arn = module.query_log.log_group_arn
  dnssec_key_arn      = module.dnssec_key.key_arn
  notify_topic_arn    = var.notify_topic_arn
  delegations         = var.delegations
}
