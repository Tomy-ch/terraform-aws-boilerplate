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

# ARN は plan では決まらないので、名前の規則どおりの値を与え、配線と validation を通す。
override_resource {
  target = module.audit_evidence.module.archive_key.aws_kms_key.this
  values = { arn = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab" }
}

override_resource {
  target = module.audit_evidence.module.access_log_key.aws_kms_key.this
  values = { arn = "arn:aws:kms:ap-northeast-1:123456789012:key/5678abcd-12ab-34cd-56ef-1234567890ab" }
}

override_resource {
  target = module.audit_evidence.module.access_log.aws_cloudwatch_log_delivery_destination.this
  values = { arn = "arn:aws:logs:ap-northeast-1:123456789012:delivery-destination:tabp-log-archive-audit-access-log" }
}

override_resource {
  target = module.audit_evidence.module.access_log.aws_cloudwatch_log_group.this
  values = { arn = "arn:aws:logs:ap-northeast-1:123456789012:log-group:tabp-log-archive-audit-access-log" }
}

override_resource {
  target = module.audit_evidence.module.archive.aws_s3_bucket.this
  values = {
    id  = "tabp-log-archive-audit-123456789012-ap-northeast-1-an"
    arn = "arn:aws:s3:::tabp-log-archive-audit-123456789012-ap-northeast-1-an"
  }
}

# 出力は Bucket Policy と Key Policy の後に決まるので、mock の apply で見る。
run "reference_values" {
  command = apply

  assert {
    condition     = module.audit_evidence.bucket_name == "tabp-log-archive-audit-123456789012-ap-northeast-1-an"
    error_message = "reference の値から保管の Bucket の名前が組まれていません。"
  }

  assert {
    condition     = module.audit_evidence.kms_key_arn == "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
    error_message = "reference の kms_key_arn が証跡の鍵を指していません。"
  }

  assert {
    condition     = module.audit_evidence.access_log_group_arn == "arn:aws:logs:ap-northeast-1:123456789012:log-group:tabp-log-archive-audit-access-log"
    error_message = "reference の access_log_group_arn がアクセスログの Log Group を指していません。"
  }
}
