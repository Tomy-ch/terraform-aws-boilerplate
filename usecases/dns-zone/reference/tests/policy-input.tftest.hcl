# Policy Test の入力を作る（make tf-policy）。ARN を与える理由は conftest/plan/README.md の「入力」。
mock_provider "aws" {
  override_during = plan

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

# Log Group の kms_key_arn の validation と、Key Policy の Resource が鍵の ARN を持つ。
override_resource {
  target          = module.dns_zone.module.query_log.module.kms.aws_kms_key.this
  override_during = plan
  values = {
    arn    = "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
    policy = null
  }
}

# resource policy の Resource と query log が Log Group の ARN を持つ。
override_resource {
  target          = module.dns_zone.module.query_log.module.log_group.aws_cloudwatch_log_group.this
  override_during = plan
  values = {
    arn = "arn:aws:logs:us-east-1:123456789012:log-group:/aws/route53/example.com"
  }
}

# KSK の precondition が鍵の ARN を持つ。
override_resource {
  target          = module.dns_zone.module.dnssec_key.aws_kms_key.this
  override_during = plan
  values = {
    arn    = "arn:aws:kms:us-east-1:123456789012:key/abcd1234-12ab-34cd-56ef-1234567890ab"
    policy = null
  }
}

run "policy_input" {
  command = plan
}
