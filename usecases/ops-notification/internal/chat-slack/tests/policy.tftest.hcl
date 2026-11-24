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

run "policies" {
  command = plan

  assert {
    condition     = aws_iam_policy.read.name == "tabp-sandbox-ops-chat-read" && aws_iam_policy.read.path == "/"
    error_message = "読み取りの policy の名前か path が違います。"
  }

  assert {
    condition = jsondecode(aws_iam_policy.read.policy) == {
      Version = "2012-10-17"
      Statement = [
        {
          Sid      = "ReadAlarms"
          Effect   = "Allow"
          Action   = ["cloudwatch:DescribeAlarmHistory", "cloudwatch:DescribeAlarms", "cloudwatch:ListTagsForResource"]
          Resource = ["arn:aws:cloudwatch:ap-northeast-1:123456789012:alarm:*", "arn:aws:cloudwatch:us-east-1:123456789012:alarm:*"]
        },
        {
          Sid      = "ReadMetrics"
          Effect   = "Allow"
          Action   = ["cloudwatch:DescribeAlarmsForMetric", "cloudwatch:GetMetricData", "cloudwatch:GetMetricStatistics", "cloudwatch:GetMetricWidgetImage", "cloudwatch:ListMetrics"]
          Resource = "*"
        },
      ]
    }
    error_message = "読み取りの policy の文書が、ops-notification/ADR-0201 の read の Action 集合になっていません。"
  }

  assert {
    condition     = aws_iam_role.this.name == "tabp-sandbox-ops-chat" && aws_iam_role.this.max_session_duration == 3600
    error_message = "channel role の名前か max_session_duration が違います。"
  }

  assert {
    condition     = aws_iam_role.this.permissions_boundary == var.permissions_boundary_arn
    error_message = "channel role が permissions boundary を持っていません。"
  }

  assert {
    condition = jsondecode(aws_iam_role.this.assume_role_policy) == {
      Version = "2012-10-17"
      Statement = [{
        Effect    = "Allow"
        Principal = { Service = "chatbot.amazonaws.com" }
        Action    = "sts:AssumeRole"
        Condition = { StringEquals = { "aws:SourceAccount" = "123456789012" } }
      }]
    }
    error_message = "channel role の信頼が、chatbot.amazonaws.com を aws:SourceAccount で自アカウントに限る1文になっていません。"
  }

  assert {
    condition     = aws_iam_role_policies_exclusive.this.role_name == "tabp-sandbox-ops-chat" && length(aws_iam_role_policies_exclusive.this.policy_names) == 0
    error_message = "channel role の inline policy の排他が、何も残さない形になっていません。"
  }

  assert {
    condition     = aws_iam_role_policy_attachments_exclusive.this.role_name == "tabp-sandbox-ops-chat"
    error_message = "managed policy の排他が channel role に付いていません。"
  }
}

# provider の region が us-east-1 でも、alarm の Resource を重ねない。
run "alarms_in_us_east_1_provider" {
  command = plan

  override_data {
    target = data.aws_region.current
    values = { region = "us-east-1" }
  }

  assert {
    condition     = [for s in jsondecode(aws_iam_policy.read.policy).Statement : s.Resource if s.Sid == "ReadAlarms"] == [["arn:aws:cloudwatch:us-east-1:123456789012:alarm:*"]]
    error_message = "provider の region が us-east-1 のとき、ReadAlarms の Resource が us-east-1 の alarm の1つになっていません。"
  }
}
