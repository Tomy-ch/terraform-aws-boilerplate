locals {
  name_prefix = "${var.project}-${var.env}"
}

module "budget" {
  source = "./internal/budget"

  name_prefix            = local.name_prefix
  notification_topic_arn = var.notification_topic_arn
  monthly_amount_usd     = var.monthly_budget_usd
}

module "cost_anomaly" {
  source = "./internal/cost-anomaly"

  name_prefix = local.name_prefix
  # メンバーアカウントごとの monitor は管理アカウントでしか作れない（finops/ADR-0001）。
  scope                  = var.env == "admin" ? "organization" : "account"
  notification_topic_arn = var.notification_topic_arn
  threshold_usd          = var.anomaly_threshold_usd
}
