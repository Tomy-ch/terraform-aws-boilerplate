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

override_resource {
  target = module.dns_zone.module.query_log.module.kms.aws_kms_key.this
  values = { arn = "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab" }
}

override_resource {
  target = module.dns_zone.module.query_log.module.log_group.aws_cloudwatch_log_group.this
  values = { arn = "arn:aws:logs:us-east-1:123456789012:log-group:/aws/route53/example.com" }
}

override_resource {
  target = module.dns_zone.module.dnssec_key.aws_kms_key.this
  values = { arn = "arn:aws:kms:us-east-1:123456789012:key/abcd1234-12ab-34cd-56ef-1234567890ab" }
}

run "reference_is_the_apex_zone" {
  command = apply

  assert {
    condition     = module.dns_zone.zone_name == "example.com"
    error_message = "reference の zone が apex ではありません。"
  }
  assert {
    condition     = module.dns_zone.query_log_group_arn == "arn:aws:logs:us-east-1:123456789012:log-group:/aws/route53/example.com"
    error_message = "reference の構成のログの所在がクエリログの Log Group ではありません。"
  }
}
