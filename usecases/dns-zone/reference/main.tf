provider "aws" {
  region = "ap-northeast-1"
}

module "dns_zone" {
  source = "../"

  project     = "tabp"
  env         = "prd"
  apex_domain = "example.com"
  delegations = {
    dev = {
      name_servers = ["ns-1.awsdns-01.com", "ns-2.awsdns-02.net", "ns-3.awsdns-03.org", "ns-4.awsdns-04.co.uk"]
      ds_record    = "12345 13 2 0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF"
    }
    stg = {
      name_servers = ["ns-5.awsdns-05.com", "ns-6.awsdns-06.net", "ns-7.awsdns-07.org", "ns-8.awsdns-08.co.uk"]
    }
  }
  query_log_retention_days      = 365
  query_log_deletion_protection = true
  notify_topic_arn              = "arn:aws:sns:us-east-1:123456789012:tabp-prd-ops-notification"
}
