# Policy Test の入力を作る（make tf-policy）。ARN を与える理由は conftest/plan/README.md の「入力」。
mock_provider "aws" {
  override_during = plan

  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
  mock_data "aws_region" {
    defaults = { region = "ap-northeast-1" }
  }
  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }
}

override_resource {
  target          = module.ops_notification.module.sns.aws_sns_topic.this
  override_during = plan
  values = {
    arn    = "arn:aws:sns:ap-northeast-1:123456789012:tabp-sandbox-ops-notification"
    policy = null
  }
}

override_resource {
  target          = module.ops_notification.module.kms.aws_kms_key.this
  override_during = plan
  values = {
    arn    = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
    policy = null
  }
}

override_resource {
  target          = module.ops_notification.module.us_east_1_topic.aws_sns_topic.this
  override_during = plan
  values = {
    arn    = "arn:aws:sns:us-east-1:123456789012:tabp-sandbox-ops-notification-us-east-1"
    policy = null
  }
}

override_resource {
  target          = module.ops_notification.module.us_east_1_topic.aws_kms_key.this
  override_during = plan
  values = {
    arn    = "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ac"
    policy = null
  }
}

override_resource {
  target          = module.ops_notification.module.heartbeat.aws_scheduler_schedule_group.us_east_1
  override_during = plan
  values          = { arn = "arn:aws:scheduler:us-east-1:123456789012:schedule-group/tabp-sandbox-ops-us-east-1" }
}

override_resource {
  target          = module.ops_notification.module.heartbeat.aws_scheduler_schedule_group.this
  override_during = plan
  values          = { arn = "arn:aws:scheduler:ap-northeast-1:123456789012:schedule-group/tabp-sandbox-ops-heartbeat" }
}

override_resource {
  target          = module.ops_notification.module.heartbeat.aws_iam_role.this
  override_during = plan
  values          = { arn = "arn:aws:iam::123456789012:role/tabp-sandbox-ops-heartbeat" }
}

override_resource {
  target          = module.ops_notification.module.chat.aws_iam_role.this
  override_during = plan
  values          = { arn = "arn:aws:iam::123456789012:role/tabp-sandbox-ops-chat" }
}

override_resource {
  target          = module.ops_notification.module.chat.aws_iam_policy.read
  override_during = plan
  values          = { arn = "arn:aws:iam::123456789012:policy/tabp-sandbox-ops-chat-read" }
}

run "policy_input" {
  command = plan
}
