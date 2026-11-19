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

override_resource {
  target          = module.audit_evidence.module.archive.aws_s3_bucket.this
  override_during = plan
  values = {
    id                  = "tabp-log-archive-audit-123456789012-ap-northeast-1-an"
    arn                 = "arn:aws:s3:::tabp-log-archive-audit-123456789012-ap-northeast-1-an"
    acceleration_status = null
    acl                 = null
    policy              = null
    request_payer       = null
  }
}

override_resource {
  target          = module.audit_evidence.module.archive_key.aws_kms_key.this
  override_during = plan
  values = {
    arn    = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
    policy = null
  }
}

override_resource {
  target          = module.audit_evidence.module.access_log_key.aws_kms_key.this
  override_during = plan
  values = {
    arn    = "arn:aws:kms:ap-northeast-1:123456789012:key/5678abcd-12ab-34cd-56ef-1234567890ab"
    policy = null
  }
}

override_resource {
  target          = module.audit_evidence.module.access_log.aws_cloudwatch_log_group.this
  override_during = plan
  values          = { arn = "arn:aws:logs:ap-northeast-1:123456789012:log-group:tabp-log-archive-audit-access-log" }
}

override_resource {
  target          = module.audit_evidence.module.access_log.aws_cloudwatch_log_delivery_destination.this
  override_during = plan
  values          = { arn = "arn:aws:logs:ap-northeast-1:123456789012:delivery-destination:tabp-log-archive-audit-access-log" }
}

run "policy_input" {
  command = plan
}
