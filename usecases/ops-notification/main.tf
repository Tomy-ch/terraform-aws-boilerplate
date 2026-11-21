locals {
  name_prefix = "${var.project}-${var.env}"
  topic_name  = "${local.name_prefix}-ops-notification"

  # us-east-1 にしか出ない metric の alarm の通知先（ops-notification/ADR-0001 決定1）。
  us_east_1_topic_name = "${local.topic_name}-us-east-1"

  # 送り手は入力にせず固定する（ops-notification/ADR-0101）。
  publishers = toset(["budgets", "cloudwatch", "costalerts", "events"])

  key_users = {
    roles    = [{ arn = module.heartbeat.role_arn, via = "sns", access = "read-write", topics = [local.topic_name] }]
    services = { sns = [{ topic = local.topic_name, publishers = local.publishers }] }
  }

  # module へ渡す値。テストは子 module の resource を参照できないので、受け渡しを local で固定する。
  # Topic の ARN は鍵の Key Policy の後に決まり、心拍の role の名前は Key Policy の前に要るので、別の local に分ける。
  topic_arn           = module.sns.topic_arn
  us_east_1_topic_arn = module.us_east_1_topic.topic_arn
  sns_args            = { name = local.topic_name, kms_key_arn = module.kms.key_arn, publishers = local.publishers }
  heartbeat_args = {
    name = "${local.name_prefix}-ops-heartbeat"
    # schedule group と schedule の名前の上限（64文字）に、最も長い env の log-archive でも収める。
    us_east_1_name           = "${local.name_prefix}-ops-us-east-1"
    env                      = var.env
    permissions_boundary_arn = var.permissions_boundary_arn
  }
  chat_args = {
    name                     = "${local.name_prefix}-ops-chat"
    slack                    = var.chat.slack
    permissions_boundary_arn = var.permissions_boundary_arn
  }
}

module "kms" {
  source = "../../modules/kms"

  name  = local.topic_name
  users = local.key_users
}

module "sns" {
  source = "../../modules/sns"

  name        = local.sns_args.name
  kms_key_arn = local.sns_args.kms_key_arn
  publishers  = local.sns_args.publishers
}

module "us_east_1_topic" {
  source = "./internal/us-east-1-topic"

  name               = local.us_east_1_topic_name
  heartbeat_role_arn = module.heartbeat.role_arn
}

module "heartbeat" {
  source = "./internal/heartbeat"

  name                     = local.heartbeat_args.name
  us_east_1_name           = local.heartbeat_args.us_east_1_name
  env                      = local.heartbeat_args.env
  topic_arn                = local.topic_arn
  us_east_1_topic_arn      = local.us_east_1_topic_arn
  permissions_boundary_arn = local.heartbeat_args.permissions_boundary_arn
}

module "chat" {
  source = "./internal/chat-slack"

  name                     = local.chat_args.name
  slack                    = local.chat_args.slack
  topic_arn                = local.topic_arn
  us_east_1_topic_arn      = local.us_east_1_topic_arn
  permissions_boundary_arn = local.chat_args.permissions_boundary_arn
}
