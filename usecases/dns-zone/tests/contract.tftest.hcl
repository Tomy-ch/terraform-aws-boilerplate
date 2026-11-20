mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
  mock_data "aws_region" {
    defaults = { region = "ap-northeast-1" }
  }
  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }
}

# resource policy の Resource と、internal/zone の鍵の precondition を通すため、ARN を形のある値にする。
override_resource {
  target = module.query_log.aws_kms_key.this
  values = { arn = "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab" }
}

override_resource {
  target = module.query_log.aws_cloudwatch_log_group.this
  values = { arn = "arn:aws:logs:us-east-1:123456789012:log-group:/aws/route53/dev.example.com" }
}

override_resource {
  target = module.dnssec_key.aws_kms_key.this
  values = { arn = "arn:aws:kms:us-east-1:123456789012:key/abcd1234-12ab-34cd-56ef-1234567890ab" }
}

override_resource {
  target = module.zone.aws_route53_zone.this
  values = {
    zone_id      = "Z0123456789ABCDEFGHIJ"
    arn          = "arn:aws:route53:::hostedzone/Z0123456789ABCDEFGHIJ"
    name_servers = ["ns-1.awsdns-01.com", "ns-2.awsdns-02.net", "ns-3.awsdns-03.org", "ns-4.awsdns-04.co.uk"]
  }
}

override_resource {
  target = module.zone.aws_route53_key_signing_key.this
  values = { ds_record = "12345 13 2 0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF" }
}

variables {
  project                       = "tabp"
  env                           = "dev"
  apex_domain                   = "example.com"
  delegations                   = {}
  query_log_retention_days      = 365
  query_log_deletion_protection = true
  notify_topic_arn              = "arn:aws:sns:us-east-1:123456789012:tabp-dev-ops-notification"
}

run "outputs_the_zone_and_the_log_location" {
  command = apply

  assert {
    condition     = output.zone_id == "Z0123456789ABCDEFGHIJ" && output.zone_arn == "arn:aws:route53:::hostedzone/Z0123456789ABCDEFGHIJ"
    error_message = "zone_id・zone_arn が zone の値ではありません。"
  }
  assert {
    condition     = output.zone_name == "dev.example.com"
    error_message = "zone_name が <env>.<apex_domain> ではありません。"
  }
  assert {
    condition     = toset(output.name_servers) == toset(["ns-1.awsdns-01.com", "ns-2.awsdns-02.net", "ns-3.awsdns-03.org", "ns-4.awsdns-04.co.uk"])
    error_message = "name_servers が zone の name server ではありません。"
  }
  assert {
    condition     = output.ds_record == "12345 13 2 0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF"
    error_message = "ds_record が KSK の DS record ではありません。"
  }
  assert {
    condition     = output.query_log_group_arn == "arn:aws:logs:us-east-1:123456789012:log-group:/aws/route53/dev.example.com"
    error_message = "ログの所在がクエリログの Log Group ではありません。"
  }
}
