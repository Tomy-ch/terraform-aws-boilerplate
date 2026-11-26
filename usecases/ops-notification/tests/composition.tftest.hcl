# 名前の組み立てと、内部の module への値の受け渡し。Key Policy と Topic Policy の文書の一致は Policy Test（plan.ops_notification）が見る。
mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
  mock_data "aws_region" {
    defaults = { region = "ap-northeast-1" }
  }
  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }
  mock_data "aws_iam_role" {
    defaults = {
      path = "/aws-service-role/management.chatbot.amazonaws.com/"
      arn  = "arn:aws:iam::123456789012:role/aws-service-role/management.chatbot.amazonaws.com/AWSServiceRoleForAWSChatbot"
    }
  }
}

override_resource {
  target = module.kms.aws_kms_key.this
  values = { arn = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab" }
}

# 取り違えが見えるよう、reference とは別の値を渡す。
variables {
  project                  = "t4bp"
  env                      = "dev"
  chat                     = { slack = { team_id = "T0000000001", channel_id = "C0000000002" } }
  permissions_boundary_arn = "arn:aws:iam::123456789012:policy/t4bp-dev-workload-boundary"
}

run "names" {
  command = plan

  assert {
    condition     = module.kms.alias_name == "alias/t4bp-dev-ops-notification"
    error_message = "鍵の alias が alias/<project>-<env>-ops-notification になっていません。"
  }

  assert {
    condition     = module.heartbeat.role_arn == "arn:aws:iam::123456789012:role/t4bp-dev-ops-heartbeat"
    error_message = "心拍の role が <project>-<env>-ops-heartbeat になっていません。"
  }

  assert {
    condition     = local.us_east_1_topic_name == "t4bp-dev-ops-notification-us-east-1"
    error_message = "us-east-1 の Topic が <project>-<env>-ops-notification-us-east-1 になっていません。"
  }
}

run "fixed_publishers" {
  command = plan

  assert {
    condition     = local.publishers == toset(["budgets", "cloudwatch", "costalerts", "events"])
    error_message = "送り手が budgets・cloudwatch・costalerts・events の4つになっていません（ops-notification/ADR-0101）。"
  }

  assert {
    condition = local.key_users == {
      roles    = [{ arn = "arn:aws:iam::123456789012:role/t4bp-dev-ops-heartbeat", via = "sns", access = "read-write", topics = ["t4bp-dev-ops-notification"] }]
      services = { sns = [{ topic = "t4bp-dev-ops-notification", publishers = toset(["budgets", "cloudwatch", "costalerts", "events"]) }] }
    }
    error_message = "鍵の利用者が、心拍の role（via = sns）と、Topic と同じ名前・同じ送り手になっていません。"
  }
}

override_resource {
  target = module.sns.aws_sns_topic.this
  values = { arn = "arn:aws:sns:ap-northeast-1:123456789012:t4bp-dev-ops-notification" }
}

override_resource {
  target = module.us_east_1_topic.aws_sns_topic.this
  values = { arn = "arn:aws:sns:us-east-1:123456789012:t4bp-dev-ops-notification-us-east-1" }
}

override_resource {
  target = module.us_east_1_topic.aws_kms_key.this
  values = { arn = "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ac" }
}

run "wiring" {
  command = apply

  override_resource {
    target = module.heartbeat.aws_iam_role.this
    values = { arn = "arn:aws:iam::123456789012:role/t4bp-dev-ops-heartbeat" }
  }

  override_resource {
    target = module.chat.aws_iam_role.this
    values = { arn = "arn:aws:iam::123456789012:role/t4bp-dev-ops-chat" }
  }

  override_resource {
    target = module.chat.aws_iam_policy.read
    values = { arn = "arn:aws:iam::123456789012:policy/t4bp-dev-ops-chat-read" }
  }

  assert {
    condition     = local.sns_args == { name = "t4bp-dev-ops-notification", kms_key_arn = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab", publishers = toset(["budgets", "cloudwatch", "costalerts", "events"]) }
    error_message = "modules/sns に、Topic の名前・鍵の key_arn・固定の送り手が渡っていません。"
  }

  # 心拍と Chatbot の構成が Topic を指すことは、Policy Test（PLAN-NOTIFY-05・06）が plan の値で見る。
  assert {
    condition     = local.topic_arn == output.topic_arn && local.topic_arn == "arn:aws:sns:ap-northeast-1:123456789012:t4bp-dev-ops-notification"
    error_message = "心拍と Chatbot の構成へ渡す Topic の ARN が、modules/sns の Topic になっていません。"
  }

  assert {
    condition     = local.us_east_1_topic_arn == output.us_east_1_topic_arn && local.us_east_1_topic_arn == "arn:aws:sns:us-east-1:123456789012:t4bp-dev-ops-notification-us-east-1"
    error_message = "心拍と Chatbot の構成へ渡す us-east-1 の Topic の ARN が、internal/us-east-1-topic の Topic になっていません。"
  }

  assert {
    condition = local.heartbeat_args == {
      name                     = "t4bp-dev-ops-heartbeat"
      us_east_1_name           = "t4bp-dev-ops-us-east-1"
      env                      = "dev"
      permissions_boundary_arn = "arn:aws:iam::123456789012:policy/t4bp-dev-workload-boundary"
    }
    error_message = "心拍に、名前・env・permissions boundary が渡っていません。"
  }

  assert {
    condition = local.chat_args == {
      name                     = "t4bp-dev-ops-chat"
      slack                    = { team_id = "T0000000001", channel_id = "C0000000002" }
      permissions_boundary_arn = "arn:aws:iam::123456789012:policy/t4bp-dev-workload-boundary"
    }
    error_message = "Chatbot の構成に、名前・Slack の ID・permissions boundary が渡っていません。"
  }
}
