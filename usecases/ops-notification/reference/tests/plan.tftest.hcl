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
}

override_resource {
  target = module.ops_notification.module.sns.aws_sns_topic.this
  values = { arn = "arn:aws:sns:ap-northeast-1:123456789012:tabp-sandbox-ops-notification" }
}

override_resource {
  target = module.ops_notification.module.kms.aws_kms_key.this
  values = { arn = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab" }
}

override_resource {
  target = module.ops_notification.module.heartbeat.aws_iam_role.this
  values = { arn = "arn:aws:iam::123456789012:role/tabp-sandbox-ops-heartbeat" }
}

override_resource {
  target = module.ops_notification.module.chat.aws_iam_role.this
  values = { arn = "arn:aws:iam::123456789012:role/tabp-sandbox-ops-chat" }
}

override_resource {
  target = module.ops_notification.module.chat.aws_iam_policy.read
  values = { arn = "arn:aws:iam::123456789012:policy/tabp-sandbox-ops-chat-read" }
}

# reference の値が入力の検査と precondition を通り、mock の apply まで進むこと。
run "reference_applies" {
  command = apply
}
