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
  # identity root が先に作る Chatbot の service-linked role（ops-notification/ADR-0201 決定9）。
  mock_data "aws_iam_role" {
    defaults = {
      path = "/aws-service-role/management.chatbot.amazonaws.com/"
      arn  = "arn:aws:iam::123456789012:role/aws-service-role/management.chatbot.amazonaws.com/AWSServiceRoleForAWSChatbot"
    }
  }
}

variables {
  name                     = "tabp-sandbox-ops-chat"
  slack                    = { team_id = "T07EA123LEP", channel_id = "C07EZ1ABC23" }
  topic_arn                = "arn:aws:sns:ap-northeast-1:123456789012:tabp-sandbox-ops-notification"
  us_east_1_topic_arn      = "arn:aws:sns:us-east-1:123456789012:tabp-sandbox-ops-notification-us-east-1"
  permissions_boundary_arn = "arn:aws:iam::123456789012:policy/tabp-sandbox-workload-boundary"
}

run "accepts_64_character_name" {
  command = plan

  variables {
    name = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  }
}

run "rejects_65_character_name" {
  command = plan

  variables {
    name = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  }

  expect_failures = [var.name]
}

run "rejects_name_with_dot" {
  command = plan

  variables {
    name = "tabp.sandbox"
  }

  expect_failures = [var.name]
}

run "rejects_lowercase_team_id" {
  command = plan

  variables {
    slack = { team_id = "t07ea123lep", channel_id = "C07EZ1ABC23" }
  }

  expect_failures = [var.slack]
}

run "rejects_empty_team_id" {
  command = plan

  variables {
    slack = { team_id = "", channel_id = "C07EZ1ABC23" }
  }

  expect_failures = [var.slack]
}

run "rejects_channel_name_instead_of_id" {
  command = plan

  variables {
    slack = { team_id = "T07EA123LEP", channel_id = "#ops" }
  }

  expect_failures = [var.slack]
}

run "rejects_empty_channel_id" {
  command = plan

  variables {
    slack = { team_id = "T07EA123LEP", channel_id = "" }
  }

  expect_failures = [var.slack]
}

run "rejects_topic_name_instead_of_arn" {
  command = plan

  variables {
    topic_arn = "tabp-sandbox-ops-notification"
  }

  expect_failures = [var.topic_arn]
}

run "rejects_topic_in_another_account" {
  command = plan

  variables {
    topic_arn = "arn:aws:sns:ap-northeast-1:210987654321:tabp-sandbox-ops-notification"
  }

  expect_failures = [aws_chatbot_slack_channel_configuration.this]
}

run "rejects_us_east_1_topic_in_another_region" {
  command = plan

  variables {
    us_east_1_topic_arn = "arn:aws:sns:ap-northeast-1:123456789012:tabp-sandbox-ops-notification-us-east-1"
  }

  expect_failures = [var.us_east_1_topic_arn]
}

run "rejects_us_east_1_topic_in_another_account" {
  command = plan

  variables {
    us_east_1_topic_arn = "arn:aws:sns:us-east-1:210987654321:tabp-sandbox-ops-notification-us-east-1"
  }

  expect_failures = [aws_chatbot_slack_channel_configuration.this]
}

run "rejects_us_east_1_topic_in_another_partition" {
  command = plan

  variables {
    us_east_1_topic_arn = "arn:aws-us-gov:sns:us-east-1:123456789012:tabp-sandbox-ops-notification-us-east-1"
  }

  expect_failures = [aws_chatbot_slack_channel_configuration.this]
}

run "rejects_aws_managed_boundary" {
  command = plan

  variables {
    permissions_boundary_arn = "arn:aws:iam::aws:policy/ReadOnlyAccess"
  }

  expect_failures = [var.permissions_boundary_arn]
}

run "rejects_boundary_in_another_account" {
  command = plan

  variables {
    permissions_boundary_arn = "arn:aws:iam::210987654321:policy/tabp-sandbox-workload-boundary"
  }

  expect_failures = [aws_iam_role.this]
}
