provider "aws" {
  region = "ap-northeast-1"
}

# Config を置く workload のアカウント、Config を置かない sandbox、password policy を置かない管理アカウントの3つを、推奨構成として示す。
module "account_baseline_dev" {
  source = "../"

  project = "tabp"
  env     = "dev"
  config_delivery = {
    bucket_name = "tabp-log-archive-audit-evidence-210987654321-ap-northeast-1-an"
    kms_key_arn = "arn:aws:kms:ap-northeast-1:210987654321:key/11111111-2222-3333-4444-555555555555"
  }
  config_history_retention_days = 2557
  security_contact = {
    name          = "Security Team"
    title         = "Security"
    email_address = "security@example.com"
    phone_number  = "+81 3 0000 0000"
  }
}

module "account_baseline_sandbox" {
  source = "../"

  project = "tabp"
  env     = "sandbox"
  config_delivery = {
    bucket_name = "tabp-log-archive-audit-evidence-210987654321-ap-northeast-1-an"
    kms_key_arn = "arn:aws:kms:ap-northeast-1:210987654321:key/11111111-2222-3333-4444-555555555555"
  }
  config_history_retention_days = 2557
  security_contact = {
    name          = "Security Team"
    title         = "Security"
    email_address = "security@example.com"
    phone_number  = "+81 3 0000 0000"
  }
}

module "account_baseline_admin" {
  source = "../"

  project = "tabp"
  env     = "admin"
  config_delivery = {
    bucket_name = "tabp-log-archive-audit-evidence-210987654321-ap-northeast-1-an"
    kms_key_arn = "arn:aws:kms:ap-northeast-1:210987654321:key/11111111-2222-3333-4444-555555555555"
  }
  config_history_retention_days = 2557
  security_contact = {
    name          = "Security Team"
    title         = "Security"
    email_address = "security@example.com"
    phone_number  = "+81 3 0000 0000"
  }
}
