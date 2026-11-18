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

run "budget" {
  command = plan

  assert {
    condition     = aws_budgets_budget.this.name == "tabp-sandbox-monthly-cost"
    error_message = "予算の名前が <name_prefix>-monthly-cost になっていません。"
  }

  assert {
    condition     = [aws_budgets_budget.this.budget_type, aws_budgets_budget.this.time_unit, aws_budgets_budget.this.limit_unit] == ["COST", "MONTHLY", "USD"]
    error_message = "予算が費用の月次の USD 建てになっていません。"
  }


  assert {
    condition     = aws_budgets_budget.this.limit_amount == "70.00"
    error_message = "limit_amount が額を小数2桁の文字列にしていません。"
  }

  assert {
    condition = toset([for n in aws_budgets_budget.this.notification : {
      notification_type         = n.notification_type
      comparison_operator       = n.comparison_operator
      threshold                 = n.threshold
      threshold_type            = n.threshold_type
      subscriber_sns_topic_arns = n.subscriber_sns_topic_arns
      }]) == toset([
      for n in [
        { notification_type = "ACTUAL", threshold = 85 },
        { notification_type = "ACTUAL", threshold = 100 },
        { notification_type = "FORECASTED", threshold = 100 },
        ] : {
        notification_type         = n.notification_type
        comparison_operator       = "GREATER_THAN"
        threshold                 = n.threshold
        threshold_type            = "PERCENTAGE"
        subscriber_sns_topic_arns = toset(["arn:aws:sns:ap-northeast-1:123456789012:tabp-sandbox-ops-notification"])
      }
    ])
    error_message = "予算の通知が、実績の85%・100%と予測の100%を Topic 1つへ送る3つになっていません。"
  }


  assert {
    condition     = output.budget.notifications == aws_budgets_budget.this.notification
    error_message = "budget の出力が予算の通知を返していません。"
  }
}

run "rejects_topic_in_other_account" {
  command = plan

  variables {
    notification_topic_arn = "arn:aws:sns:ap-northeast-1:210987654321:tabp-sandbox-ops-notification"
  }

  expect_failures = [aws_budgets_budget.this]
}

run "rejects_topic_in_other_partition" {
  command = plan

  variables {
    notification_topic_arn = "arn:aws-us-gov:sns:us-gov-west-1:123456789012:tabp-sandbox-ops-notification"
  }

  expect_failures = [aws_budgets_budget.this]
}

run "formats_fractional_amount" {
  command = plan

  variables {
    monthly_amount_usd = 12.5
  }

  assert {
    condition     = aws_budgets_budget.this.limit_amount == "12.50"
    error_message = "limit_amount が小数1桁の額を2桁の文字列にしていません。"
  }
}
