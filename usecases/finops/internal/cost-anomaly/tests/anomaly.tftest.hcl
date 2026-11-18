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

run "account_scope" {
  command = plan

  assert {
    condition     = [aws_ce_anomaly_monitor.this.monitor_type, aws_ce_anomaly_monitor.this.monitor_dimension] == ["DIMENSIONAL", "SERVICE"]
    error_message = "scope = account の monitor が AWS のサービスごとの AWS managed monitor になっていません。"
  }

  assert {
    condition     = aws_ce_anomaly_monitor.this.name == "tabp-sandbox-services"
    error_message = "scope = account の monitor の名前が <name_prefix>-services になっていません。"
  }

  assert {
    condition     = aws_ce_anomaly_monitor.this.monitor_specification == null
    error_message = "AWS managed monitor が monitor_specification を持っています。"
  }
}

run "organization_scope" {
  command = plan

  variables {
    name_prefix = "tabp-admin"
    scope       = "organization"
  }

  assert {
    condition     = [aws_ce_anomaly_monitor.this.monitor_type, aws_ce_anomaly_monitor.this.monitor_dimension] == ["DIMENSIONAL", "LINKED_ACCOUNT"]
    error_message = "scope = organization の monitor がメンバーアカウントごとの AWS managed monitor になっていません。"
  }

  assert {
    condition     = aws_ce_anomaly_monitor.this.name == "tabp-admin-linked-accounts"
    error_message = "scope = organization の monitor の名前が <name_prefix>-linked-accounts になっていません。"
  }
}

run "subscription" {
  command = plan

  assert {
    condition     = aws_ce_anomaly_subscription.this.name == "tabp-sandbox-immediate"
    error_message = "subscription の名前が <name_prefix>-immediate になっていません。"
  }


  assert {
    condition = aws_ce_anomaly_subscription.this.subscriber == toset([{
      type    = "SNS"
      address = "arn:aws:sns:ap-northeast-1:123456789012:tabp-sandbox-ops-notification"
    }])
    error_message = "subscription の宛先が Topic 1つになっていません。"
  }

  assert {
    condition = [for e in aws_ce_anomaly_subscription.this.threshold_expression[0].and : e.dimension[0]] == [
      { key = "ANOMALY_TOTAL_IMPACT_ABSOLUTE", match_options = toset(["GREATER_THAN_OR_EQUAL"]), values = toset(["100"]) },
      { key = "ANOMALY_TOTAL_IMPACT_PERCENTAGE", match_options = toset(["GREATER_THAN_OR_EQUAL"]), values = toset(["40"]) },
    ]
    error_message = "subscription の閾値が、影響額 threshold_usd 以上かつ40%以上の AND になっていません。"
  }
}

# monitor の ARN は plan では決まらないので、plan の時点で形のある値を与えて配線を見る。
run "subscription_points_to_monitor" {
  command = plan

  override_resource {
    target          = aws_ce_anomaly_monitor.this
    override_during = plan
    values          = { arn = "arn:aws:ce::123456789012:anomalymonitor/1234abcd-12ab-34cd-56ef-1234567890ab" }
  }

  assert {
    condition     = aws_ce_anomaly_subscription.this.monitor_arn_list == tolist(["arn:aws:ce::123456789012:anomalymonitor/1234abcd-12ab-34cd-56ef-1234567890ab"])
    error_message = "subscription が同じ module の monitor だけを指していません。"
  }
}

run "fractional_threshold" {
  command = plan

  variables {
    threshold_usd = 12.5
  }

  assert {
    condition     = contains([for e in aws_ce_anomaly_subscription.this.threshold_expression[0].and : e.dimension[0].values], toset(["12.5"]))
    error_message = "影響額の閾値が数の文字列表現になっていません。"
  }
}

run "rejects_topic_in_other_account" {
  command = plan

  variables {
    notification_topic_arn = "arn:aws:sns:ap-northeast-1:210987654321:tabp-sandbox-ops-notification"
  }

  expect_failures = [aws_ce_anomaly_subscription.this]
}

run "rejects_topic_in_other_partition" {
  command = plan

  variables {
    notification_topic_arn = "arn:aws-cn:sns:cn-north-1:123456789012:tabp-sandbox-ops-notification"
  }

  expect_failures = [aws_ce_anomaly_subscription.this]
}
