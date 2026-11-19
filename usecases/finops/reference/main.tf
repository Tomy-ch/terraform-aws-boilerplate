provider "aws" {
  region = "ap-northeast-1"
}

# 管理アカウントとメンバーアカウントで monitor の次元が違うので、両方を推奨構成として示す。
module "finops_admin" {
  source = "../"

  project                = "tabp"
  env                    = "admin"
  notification_topic_arn = "arn:aws:sns:ap-northeast-1:123456789012:tabp-admin-ops-notification"
  monthly_budget_usd     = 300
  anomaly_threshold_usd  = 50
}

module "finops_sandbox" {
  source = "../"

  project                = "tabp"
  env                    = "sandbox"
  notification_topic_arn = "arn:aws:sns:ap-northeast-1:123456789012:tabp-sandbox-ops-notification"
  monthly_budget_usd     = 70
  anomaly_threshold_usd  = 20
}
