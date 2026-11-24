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
  target = aws_iam_role.this
  values = { arn = "arn:aws:iam::123456789012:role/tabp-sandbox-ops-chat" }
}

override_resource {
  target = aws_iam_policy.read
  values = { arn = "arn:aws:iam::123456789012:policy/tabp-sandbox-ops-chat-read" }
}

variables {
  name                     = "tabp-sandbox-ops-chat"
  slack                    = { team_id = "T07EA123LEP", channel_id = "C07EZ1ABC23" }
  topic_arn                = "arn:aws:sns:ap-northeast-1:123456789012:tabp-sandbox-ops-notification"
  us_east_1_topic_arn      = "arn:aws:sns:us-east-1:123456789012:tabp-sandbox-ops-notification-us-east-1"
  permissions_boundary_arn = "arn:aws:iam::123456789012:policy/tabp-sandbox-workload-boundary"
}

run "configuration" {
  command = plan

  assert {
    condition = [
      aws_chatbot_slack_channel_configuration.this.configuration_name,
      aws_chatbot_slack_channel_configuration.this.slack_team_id,
      aws_chatbot_slack_channel_configuration.this.slack_channel_id,
      aws_chatbot_slack_channel_configuration.this.logging_level,
      aws_chatbot_slack_channel_configuration.this.region,
      ] == [
      "tabp-sandbox-ops-chat",
      "T07EA123LEP",
      "C07EZ1ABC23",
      "NONE",
      "us-west-2",
    ]
    error_message = "Chatbot の構成の名前・Slack の ID・logging_level・region が違います。"
  }

  assert {
    condition     = aws_chatbot_slack_channel_configuration.this.user_authorization_required == true
    error_message = "user_authorization_required が true になっていません。"
  }

  assert {
    condition     = toset(aws_chatbot_slack_channel_configuration.this.sns_topic_arns) == toset([var.topic_arn, var.us_east_1_topic_arn])
    error_message = "Chatbot の構成が購読する Topic が、渡した2つの Topic だけになっていません。"
  }
}

# policy と role の ARN は plan では決まらないので、mock の apply で配線を見る。
run "wiring" {
  command = apply

  assert {
    condition     = aws_chatbot_slack_channel_configuration.this.iam_role_arn == aws_iam_role.this.arn
    error_message = "Chatbot の構成の channel role が、この module の role になっていません。"
  }

  assert {
    condition     = toset(aws_chatbot_slack_channel_configuration.this.guardrail_policy_arns) == toset([aws_iam_policy.read.arn])
    error_message = "guardrail が読み取りの policy だけになっていません。"
  }

  assert {
    condition     = toset(aws_iam_role_policy_attachments_exclusive.this.policy_arns) == toset(aws_chatbot_slack_channel_configuration.this.guardrail_policy_arns)
    error_message = "channel role に付く policy と guardrail が同じではありません。"
  }

  assert {
    condition     = aws_iam_role_policy_attachment.read.role == aws_iam_role.this.name && aws_iam_role_policy_attachment.read.policy_arn == aws_iam_policy.read.arn
    error_message = "読み取りの policy を channel role に付けるのが、attachment になっていません。"
  }
}

run "service_linked_role_with_other_path" {
  command = plan

  override_data {
    target = data.aws_iam_role.chatbot_service_linked
    values = {
      path = "/"
      arn  = "arn:aws:iam::123456789012:role/AWSServiceRoleForAWSChatbot"
    }
  }

  expect_failures = [data.aws_iam_role.chatbot_service_linked]
}

run "service_linked_role_in_other_account" {
  command = plan

  override_data {
    target = data.aws_iam_role.chatbot_service_linked
    values = {
      path = "/aws-service-role/management.chatbot.amazonaws.com/"
      arn  = "arn:aws:iam::210987654321:role/aws-service-role/management.chatbot.amazonaws.com/AWSServiceRoleForAWSChatbot"
    }
  }

  expect_failures = [aws_chatbot_slack_channel_configuration.this]
}
