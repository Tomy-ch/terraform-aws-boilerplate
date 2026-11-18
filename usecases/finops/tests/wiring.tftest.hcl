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

run "member_account_wiring" {
  command = plan

  assert {
    condition     = module.budget.budget.name == "tabp-sandbox-monthly-cost"
    error_message = "予算の名前が <project>-<env>-monthly-cost になっていません。"
  }

  assert {
    condition     = module.budget.budget.limit_amount == "70.00"
    error_message = "monthly_budget_usd が予算の額に渡っていません。"
  }

  assert {
    condition     = alltrue([for n in module.budget.budget.notifications : n.subscriber_sns_topic_arns == toset([var.notification_topic_arn])])
    error_message = "notification_topic_arn が予算の通知の宛先に渡っていません。"
  }

  assert {
    condition     = module.cost_anomaly.monitor == { name = "tabp-sandbox-services", dimension = "SERVICE" }
    error_message = "メンバーアカウントの monitor が AWS のサービスごとになっていません。"
  }

  assert {
    condition     = module.cost_anomaly.subscription.subscriber == toset([{ type = "SNS", address = var.notification_topic_arn }])
    error_message = "notification_topic_arn が異常の通知の宛先に渡っていません。"
  }

  assert {
    condition     = contains([for e in module.cost_anomaly.subscription.threshold_expression[0].and : e.dimension[0].values], toset(["20"]))
    error_message = "anomaly_threshold_usd が異常の影響額の閾値に渡っていません。"
  }

  assert {
    condition     = module.cost_anomaly.subscription.name == "tabp-sandbox-immediate"
    error_message = "subscription の名前が <project>-<env>-immediate になっていません。"
  }
}

run "management_account_wiring" {
  command = plan

  variables {
    env                    = "admin"
    notification_topic_arn = "arn:aws:sns:ap-northeast-1:123456789012:tabp-admin-ops-notification"
  }

  assert {
    condition     = module.cost_anomaly.monitor == { name = "tabp-admin-linked-accounts", dimension = "LINKED_ACCOUNT" }
    error_message = "管理アカウントの monitor がメンバーアカウントごとになっていません。"
  }

  assert {
    condition     = module.budget.budget.name == "tabp-admin-monthly-cost"
    error_message = "管理アカウントの予算の名前が <project>-admin-monthly-cost になっていません。"
  }
}

run "non_management_envs_watch_services" {
  command = plan

  variables {
    env = "log-archive"
  }

  assert {
    condition     = module.cost_anomaly.monitor.dimension == "SERVICE"
    error_message = "admin 以外の env の monitor が AWS のサービスごとになっていません。"
  }
}
