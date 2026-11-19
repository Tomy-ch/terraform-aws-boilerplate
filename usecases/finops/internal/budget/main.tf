# kics-scan disable=e592a0c5-5bdb-414c-9066-5dba7cdea370
# e592a0c5（IAM Access Analyzer）: アカウント単位の統制で account-baseline が持つ（docs/v1.0-plan/scope.md 第7節）。撤回: account-baseline を実装したとき。

data "aws_caller_identity" "current" {}
data "aws_partition" "current" {}

locals {
  # arn:<partition>:sns:<region>:<account>:<name>
  topic_arn_parts = split(":", var.notification_topic_arn)
}

resource "aws_budgets_budget" "this" {
  name         = "${var.name_prefix}-monthly-cost"
  budget_type  = "COST"
  time_unit    = "MONTHLY"
  limit_amount = format("%.2f", var.monthly_amount_usd)
  limit_unit   = "USD"

  notification {
    notification_type         = "ACTUAL"
    comparison_operator       = "GREATER_THAN"
    threshold                 = 85
    threshold_type            = "PERCENTAGE"
    subscriber_sns_topic_arns = [var.notification_topic_arn]
  }

  notification {
    notification_type         = "ACTUAL"
    comparison_operator       = "GREATER_THAN"
    threshold                 = 100
    threshold_type            = "PERCENTAGE"
    subscriber_sns_topic_arns = [var.notification_topic_arn]
  }

  notification {
    notification_type         = "FORECASTED"
    comparison_operator       = "GREATER_THAN"
    threshold                 = 100
    threshold_type            = "PERCENTAGE"
    subscriber_sns_topic_arns = [var.notification_topic_arn]
  }

  lifecycle {
    # Budgets は別のアカウントの Topic へ通知できない。
    precondition {
      condition     = local.topic_arn_parts[1] == data.aws_partition.current.partition && local.topic_arn_parts[4] == data.aws_caller_identity.current.account_id
      error_message = "notification_topic_arn（${var.notification_topic_arn}）は、予算と同じ partition・アカウントの Topic にしてください。"
    }
  }
}
