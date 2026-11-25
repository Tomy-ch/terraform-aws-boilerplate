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
  target = module.ops_notification.module.sns.aws_sns_topic.this
  values = { arn = "arn:aws:sns:ap-northeast-1:123456789012:tabp-sandbox-ops-notification" }
}

override_resource {
  target = module.ops_notification.module.kms.aws_kms_key.this
  values = { arn = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab" }
}

override_resource {
  target = module.ops_notification.module.us_east_1_topic.aws_sns_topic.this
  values = { arn = "arn:aws:sns:us-east-1:123456789012:tabp-sandbox-ops-notification-us-east-1" }
}

override_resource {
  target = module.ops_notification.module.us_east_1_topic.aws_kms_key.this
  values = { arn = "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ac" }
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

run "reference_applies" {
  command = apply
}
