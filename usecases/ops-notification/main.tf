locals {
  name_prefix = "${var.project}-${var.env}"
  topic_name  = "${local.name_prefix}-ops-notification"

  # 送り手は入力にせず固定する（ops-notification/ADR-0101）。
  publishers = toset(["budgets", "cloudwatch", "costalerts", "events"])

  key_users = {
    roles    = [{ arn = module.heartbeat.role_arn, via = "sns", access = "read-write", topics = [local.topic_name] }]
    services = { sns = [{ topic = local.topic_name, publishers = local.publishers }] }
  }
}

module "kms" {
  source = "../../modules/kms"

  name  = local.topic_name
  users = local.key_users
}

module "sns" {
  source = "../../modules/sns"

  name        = local.topic_name
  kms_key_arn = module.kms.key_arn
  publishers  = local.publishers
}

module "heartbeat" {
  source = "./internal/heartbeat"

  name                     = "${local.name_prefix}-ops-heartbeat"
  env                      = var.env
  topic_arn                = module.sns.topic_arn
  permissions_boundary_arn = var.permissions_boundary_arn
}

module "chat" {
  source = "./internal/chat-slack"

  name                     = "${local.name_prefix}-ops-chat"
  slack                    = var.chat.slack
  topic_arn                = module.sns.topic_arn
  permissions_boundary_arn = var.permissions_boundary_arn
}
