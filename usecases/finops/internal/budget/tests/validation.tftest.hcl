mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }
}

variables {
  name_prefix            = "tabp-sandbox"
  notification_topic_arn = "arn:aws:sns:ap-northeast-1:123456789012:tabp-sandbox-ops-notification"
  monthly_amount_usd     = 70
}

run "accepts_two_decimal_places" {
  command = plan

  variables {
    monthly_amount_usd = 0.01
  }
}

run "rejects_zero_amount" {
  command = plan

  variables {
    monthly_amount_usd = 0
  }

  expect_failures = [var.monthly_amount_usd]
}

run "rejects_three_decimal_places" {
  command = plan

  variables {
    monthly_amount_usd = 10.005
  }

  expect_failures = [var.monthly_amount_usd]
}

run "rejects_fifo_topic" {
  command = plan

  variables {
    notification_topic_arn = "arn:aws:sns:ap-northeast-1:123456789012:tabp-sandbox-ops-notification.fifo"
  }

  expect_failures = [var.notification_topic_arn]
}

run "rejects_non_topic_arn" {
  command = plan

  variables {
    notification_topic_arn = "arn:aws:sqs:ap-northeast-1:123456789012:tabp-sandbox-ops-notification"
  }

  expect_failures = [var.notification_topic_arn]
}

run "accepts_longest_name_prefix" {
  command = plan

  variables {
    name_prefix = "a${join("", [for i in range(86) : "b"])}"
  }

  assert {
    condition     = length(aws_budgets_budget.this.name) == 100
    error_message = "最も長い name_prefix で予算の名前が100文字になっていません。"
  }
}

run "rejects_name_prefix_over_87_chars" {
  command = plan

  variables {
    name_prefix = "a${join("", [for i in range(87) : "b"])}"
  }

  expect_failures = [var.name_prefix]
}

run "rejects_name_prefix_with_trailing_hyphen" {
  command = plan

  variables {
    name_prefix = "tabp-"
  }

  expect_failures = [var.name_prefix]
}
