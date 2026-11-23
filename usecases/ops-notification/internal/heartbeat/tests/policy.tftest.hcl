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
  target          = aws_scheduler_schedule_group.this
  override_during = plan
  values          = { arn = "arn:aws:scheduler:ap-northeast-1:123456789012:schedule-group/tabp-sandbox-ops-heartbeat" }
}

override_resource {
  target          = aws_scheduler_schedule_group.us_east_1
  override_during = plan
  values          = { arn = "arn:aws:scheduler:us-east-1:123456789012:schedule-group/tabp-sandbox-ops-us-east-1" }
}

variables {
  name                     = "tabp-sandbox-ops-heartbeat"
  us_east_1_name           = "tabp-sandbox-ops-us-east-1"
  env                      = "sandbox"
  topic_arn                = "arn:aws:sns:ap-northeast-1:123456789012:tabp-sandbox-ops-notification"
  us_east_1_topic_arn      = "arn:aws:sns:us-east-1:123456789012:tabp-sandbox-ops-notification-us-east-1"
  permissions_boundary_arn = "arn:aws:iam::123456789012:policy/tabp-sandbox-workload-boundary"
}

run "role" {
  command = plan

  assert {
    condition     = aws_iam_role.this.name == "tabp-sandbox-ops-heartbeat" && aws_iam_role.this.max_session_duration == 3600
    error_message = "role の名前か max_session_duration が違います。"
  }

  assert {
    condition     = aws_iam_role.this.permissions_boundary == var.permissions_boundary_arn
    error_message = "role が permissions boundary を持っていません。"
  }

  assert {
    condition = jsondecode(aws_iam_role.this.assume_role_policy) == {
      Version = "2012-10-17"
      Statement = [{
        Effect    = "Allow"
        Principal = { Service = "scheduler.amazonaws.com" }
        Action    = "sts:AssumeRole"
        Condition = { StringEquals = {
          "aws:SourceAccount" = "123456789012"
          "aws:SourceArn"     = ["arn:aws:scheduler:ap-northeast-1:123456789012:schedule-group/tabp-sandbox-ops-heartbeat", "arn:aws:scheduler:us-east-1:123456789012:schedule-group/tabp-sandbox-ops-us-east-1"]
        } }
      }]
    }
    error_message = "role の信頼が、scheduler.amazonaws.com をアカウントと2つの schedule group の ARN で限る形になっていません。"
  }

  assert {
    condition = jsondecode(aws_iam_role_policy.this.policy) == {
      Version = "2012-10-17"
      Statement = [{
        Effect   = "Allow"
        Action   = "sns:Publish"
        Resource = [var.topic_arn, var.us_east_1_topic_arn]
      }]
    }
    error_message = "role の権限が、2つの Topic への sns:Publish だけになっていません。"
  }

  assert {
    condition     = aws_iam_role_policy.this.name == "publish" && aws_iam_role_policy.this.role == "tabp-sandbox-ops-heartbeat"
    error_message = "inline policy の名前か付け先の role が違います。"
  }

  assert {
    condition     = aws_iam_role_policies_exclusive.this.role_name == "tabp-sandbox-ops-heartbeat" && aws_iam_role_policies_exclusive.this.policy_names == toset(["publish"])
    error_message = "inline policy の排他が publish だけを残す形になっていません。"
  }

  assert {
    condition     = aws_iam_role_policy_attachments_exclusive.this.role_name == "tabp-sandbox-ops-heartbeat" && length(aws_iam_role_policy_attachments_exclusive.this.policy_arns) == 0
    error_message = "managed policy の排他が、何も付けない形になっていません。"
  }
}
