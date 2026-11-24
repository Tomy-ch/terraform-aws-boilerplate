# provider の region が us-east-1 でも、子 module の region の precondition を通って apply でき、2つの Topic・鍵の alias・
# schedule group と schedule の名前が違うこと。名前は provider の region に依らず組むので、衝突しないことはこの違いが持つ。
mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
  mock_data "aws_region" {
    defaults = { region = "us-east-1" }
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
  target = module.sns.aws_sns_topic.this
  values = { arn = "arn:aws:sns:us-east-1:123456789012:tabp-sandbox-ops-notification" }
}

override_resource {
  target = module.kms.aws_kms_key.this
  values = { arn = "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab" }
}

override_resource {
  target = module.us_east_1_topic.aws_sns_topic.this
  values = { arn = "arn:aws:sns:us-east-1:123456789012:tabp-sandbox-ops-notification-us-east-1" }
}

override_resource {
  target = module.us_east_1_topic.aws_kms_key.this
  values = { arn = "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ac" }
}

override_resource {
  target = module.heartbeat.aws_iam_role.this
  values = { arn = "arn:aws:iam::123456789012:role/tabp-sandbox-ops-heartbeat" }
}

override_resource {
  target = module.chat.aws_iam_role.this
  values = { arn = "arn:aws:iam::123456789012:role/tabp-sandbox-ops-chat" }
}

override_resource {
  target = module.chat.aws_iam_policy.read
  values = { arn = "arn:aws:iam::123456789012:policy/tabp-sandbox-ops-chat-read" }
}

variables {
  project                  = "tabp"
  env                      = "sandbox"
  chat                     = { slack = { team_id = "T07EA123LEP", channel_id = "C07EZ1ABC23" } }
  permissions_boundary_arn = "arn:aws:iam::123456789012:policy/tabp-sandbox-workload-boundary"
}

run "applies_in_us_east_1_provider_with_distinct_names" {
  command = apply

  assert {
    condition     = local.sns_args.name != local.us_east_1_topic_name && module.kms.alias_name != "alias/${local.us_east_1_topic_name}"
    error_message = "provider の region が us-east-1 のとき、2つの Topic か鍵の alias の名前が同じです。"
  }

  assert {
    condition     = local.heartbeat_args.name != local.heartbeat_args.us_east_1_name
    error_message = "provider の region が us-east-1 のとき、2つの schedule group と schedule の名前が同じです。"
  }
}
