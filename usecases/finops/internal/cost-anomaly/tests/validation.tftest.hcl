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
  scope                  = "account"
  notification_topic_arn = "arn:aws:sns:ap-northeast-1:123456789012:tabp-sandbox-ops-notification"
  threshold_usd          = 100
}

run "rejects_unknown_scope" {
  command = plan

  variables {
    scope = "tag"
  }

  expect_failures = [var.scope]
}

run "rejects_zero_threshold" {
  command = plan

  variables {
    threshold_usd = 0
  }

  expect_failures = [var.threshold_usd]
}

run "accepts_api_maximum_threshold" {
  command = plan

  variables {
    threshold_usd = 10000000000
  }
}

run "rejects_threshold_over_api_maximum" {
  command = plan

  variables {
    threshold_usd = 10000000000.01
  }

  expect_failures = [var.threshold_usd]
}

run "rejects_fifo_topic" {
  command = plan

  variables {
    notification_topic_arn = "arn:aws:sns:ap-northeast-1:123456789012:tabp-sandbox-ops-notification.fifo"
  }

  expect_failures = [var.notification_topic_arn]
}

run "rejects_name_prefix_with_upper_case" {
  command = plan

  variables {
    name_prefix = "Tabp-sandbox"
  }

  expect_failures = [var.name_prefix]
}

run "accepts_smallest_threshold" {
  command = plan

  variables {
    threshold_usd = 0.01
  }
}

run "rejects_name_prefix_with_trailing_hyphen" {
  command = plan

  variables {
    name_prefix = "tabp-"
  }

  expect_failures = [var.name_prefix]
}

run "rejects_name_prefix_with_leading_hyphen" {
  command = plan

  variables {
    name_prefix = "-tabp"
  }

  expect_failures = [var.name_prefix]
}

run "rejects_non_topic_arn" {
  command = plan

  variables {
    notification_topic_arn = "arn:aws:sqs:ap-northeast-1:123456789012:tabp-sandbox-ops-notification"
  }

  expect_failures = [var.notification_topic_arn]
}
