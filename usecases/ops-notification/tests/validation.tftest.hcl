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
  project                  = "tabp"
  env                      = "sandbox"
  chat                     = { slack = { team_id = "T07EA123LEP", channel_id = "C07EZ1ABC23" } }
  permissions_boundary_arn = "arn:aws:iam::123456789012:policy/tabp-sandbox-workload-boundary"
}

# 最も長い名前は role 名 `<project>-log-archive-ops-heartbeat` で、IAM の上限の64文字に収まる。
run "accepts_longest_names" {
  command = plan

  variables {
    project = "abcdefghijklmnopqrstuvwxyz01234567890"
    env     = "log-archive"
  }

  assert {
    condition     = module.heartbeat.role_arn == "arn:aws:iam::123456789012:role/abcdefghijklmnopqrstuvwxyz01234567890-log-archive-ops-heartbeat"
    error_message = "37文字の project と log-archive で、63文字の role 名が組まれていません。"
  }

  assert {
    condition     = local.heartbeat_args.us_east_1_name == "abcdefghijklmnopqrstuvwxyz01234567890-log-archive-ops-us-east-1" && length(local.heartbeat_args.us_east_1_name) == 63
    error_message = "37文字の project と log-archive で、us-east-1 の schedule group と schedule の63文字の名前が組まれていません。"
  }
}

run "rejects_38_character_project" {
  command = plan

  variables {
    project = "abcdefghijklmnopqrstuvwxyz012345678901"
  }

  expect_failures = [var.project]
}

run "rejects_project_with_hyphen" {
  command = plan

  variables {
    project = "ta-bp"
  }

  expect_failures = [var.project]
}

run "rejects_uppercase_project" {
  command = plan

  variables {
    project = "Tabp"
  }

  expect_failures = [var.project]
}

run "accepts_admin" {
  command = plan

  variables {
    env = "admin"
  }
}

run "accepts_security" {
  command = plan

  variables {
    env = "security"
  }
}

run "accepts_dev" {
  command = plan

  variables {
    env = "dev"
  }
}

run "accepts_stg" {
  command = plan

  variables {
    env = "stg"
  }
}

run "accepts_prd" {
  command = plan

  variables {
    env = "prd"
  }
}

run "rejects_unknown_env" {
  command = plan

  variables {
    env = "production"
  }

  expect_failures = [var.env]
}

run "rejects_chat_without_slack" {
  command = plan

  variables {
    chat = { slack = null }
  }

  expect_failures = [var.chat]
}

run "rejects_slack_channel_name" {
  command = plan

  variables {
    chat = { slack = { team_id = "T07EA123LEP", channel_id = "#ops" } }
  }

  expect_failures = [var.chat]
}

run "rejects_aws_managed_boundary" {
  command = plan

  variables {
    permissions_boundary_arn = "arn:aws:iam::aws:policy/ReadOnlyAccess"
  }

  expect_failures = [var.permissions_boundary_arn]
}
