provider "aws" {
  region = "ap-northeast-1"
}

module "audit_evidence" {
  source = "../"

  project        = "tabp"
  env            = "log-archive"
  retention_days = 731
  writers = {
    cloudtrail_organization_trail = {
      trail_name       = "tabp-org"
      owner_account_id = "111111111111"
      organization_id  = "o-abcdefghij"
      region           = "ap-northeast-1"
    }
    config_source_accounts = ["111111111111", "111111111111"]
  }
  access_log_retention_days = 365
  notify_topic_arn          = "arn:aws:sns:ap-northeast-1:123456789012:tabp-log-archive-ops-notification"
}
