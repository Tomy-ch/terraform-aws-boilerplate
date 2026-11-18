mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }
}

variables {
  project                = "tabp"
  env                    = "sandbox"
  notification_topic_arn = "arn:aws:sns:ap-northeast-1:123456789012:tabp-sandbox-ops-notification"
  monthly_budget_usd     = 70
  anomaly_threshold_usd  = 20
}

run "accepts_every_env" {
  command = plan

  variables {
    env = "security"
  }
}

run "rejects_unknown_env" {
  command = plan

  variables {
    env = "prod"
  }

  expect_failures = [var.env]
}

run "rejects_project_with_hyphen" {
  command = plan

  variables {
    project = "ta-bp"
  }

  expect_failures = [var.project]
}

run "accepts_longest_project" {
  command = plan

  variables {
    env     = "log-archive"
    project = join("", [for i in range(75) : "a"])
  }

  assert {
    condition     = length(module.budget.budget.name) == 100
    error_message = "最も長い project と env で予算の名前が100文字になっていません。"
  }
}

run "rejects_project_over_75_chars" {
  command = plan

  variables {
    project = join("", [for i in range(76) : "a"])
  }

  expect_failures = [var.project]
}

run "rejects_non_topic_arn" {
  command = plan

  variables {
    notification_topic_arn = "arn:aws:sqs:ap-northeast-1:123456789012:tabp-sandbox-ops-notification"
  }

  expect_failures = [var.notification_topic_arn]
}

run "rejects_fifo_topic" {
  command = plan

  variables {
    notification_topic_arn = "arn:aws:sns:ap-northeast-1:123456789012:tabp-sandbox-ops-notification.fifo"
  }

  expect_failures = [var.notification_topic_arn]
}

run "rejects_zero_budget" {
  command = plan

  variables {
    monthly_budget_usd = 0
  }

  expect_failures = [var.monthly_budget_usd]
}

run "rejects_budget_with_three_decimal_places" {
  command = plan

  variables {
    monthly_budget_usd = 70.125
  }

  expect_failures = [var.monthly_budget_usd]
}

run "rejects_zero_anomaly_threshold" {
  command = plan

  variables {
    anomaly_threshold_usd = 0
  }

  expect_failures = [var.anomaly_threshold_usd]
}

run "rejects_anomaly_threshold_over_api_maximum" {
  command = plan

  variables {
    anomaly_threshold_usd = 10000000001
  }

  expect_failures = [var.anomaly_threshold_usd]
}
