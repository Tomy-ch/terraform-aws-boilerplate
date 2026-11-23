# kics-scan disable=e592a0c5-5bdb-414c-9066-5dba7cdea370
# e592a0c5（IAM Access Analyzer）: KICS はファイルごとに analyzer の宣言を求める。analyzer はアカウント単位の統制で account-baseline が持つ（docs/v1.0-plan/scope.md 第7節）。撤回: KICS がファイルの外の宣言を数えるようになったとき。

data "aws_caller_identity" "current" {}
data "aws_partition" "current" {}

locals {
  monitor = {
    organization = { dimension = "LINKED_ACCOUNT", name = "${var.name_prefix}-linked-accounts" }
    account      = { dimension = "SERVICE", name = "${var.name_prefix}-services" }
  }[var.scope]

  # arn:<partition>:sns:<region>:<account>:<name>
  topic_arn_parts = split(":", var.notification_topic_arn)
}

resource "aws_ce_anomaly_monitor" "this" {
  name              = local.monitor.name
  monitor_type      = "DIMENSIONAL"
  monitor_dimension = local.monitor.dimension
}

resource "aws_ce_anomaly_subscription" "this" {
  name             = "${var.name_prefix}-immediate"
  frequency        = "IMMEDIATE"
  monitor_arn_list = [aws_ce_anomaly_monitor.this.arn]

  subscriber {
    type    = "SNS"
    address = var.notification_topic_arn
  }

  threshold_expression {
    and {
      dimension {
        key           = "ANOMALY_TOTAL_IMPACT_ABSOLUTE"
        match_options = ["GREATER_THAN_OR_EQUAL"]
        values        = [tostring(var.threshold_usd)]
      }
    }
    and {
      dimension {
        key           = "ANOMALY_TOTAL_IMPACT_PERCENTAGE"
        match_options = ["GREATER_THAN_OR_EQUAL"]
        values        = ["40"]
      }
    }
  }

  lifecycle {
    precondition {
      condition     = local.topic_arn_parts[1] == data.aws_partition.current.partition && local.topic_arn_parts[4] == data.aws_caller_identity.current.account_id
      error_message = "notification_topic_arn（${var.notification_topic_arn}）は、subscription と同じ partition・アカウントの Topic にしてください。"
    }
  }
}
