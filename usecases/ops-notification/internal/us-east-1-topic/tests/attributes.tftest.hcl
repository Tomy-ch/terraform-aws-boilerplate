mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }
}

variables {
  name               = "tabp-sandbox-ops-notification-us-east-1"
  heartbeat_role_arn = "arn:aws:iam::123456789012:role/tabp-sandbox-ops-heartbeat"
}

run "region" {
  command = plan

  assert {
    condition = [
      aws_kms_key.this.region,
      aws_kms_alias.this.region,
      aws_kms_key_policy.this.region,
      aws_sns_topic.this.region,
      aws_sns_topic_policy.this.region,
    ] == ["us-east-1", "us-east-1", "us-east-1", "us-east-1", "us-east-1"]
    error_message = "鍵・alias・Key Policy・Topic・Topic Policy が us-east-1 に置かれていません（repository-wide/ADR-0208 決定1）。"
  }
}

run "same_values_as_general_modules" {
  command = plan

  assert {
    condition = [
      aws_kms_key.this.key_usage,
      aws_kms_key.this.customer_master_key_spec,
      tostring(aws_kms_key.this.multi_region),
      tostring(aws_kms_key.this.is_enabled),
      tostring(aws_kms_key.this.enable_key_rotation),
      tostring(aws_kms_key.this.rotation_period_in_days),
      tostring(aws_kms_key.this.deletion_window_in_days),
      tostring(aws_kms_key.this.bypass_policy_lockout_safety_check),
      tostring(aws_kms_key_policy.this.bypass_policy_lockout_safety_check),
    ] == ["ENCRYPT_DECRYPT", "SYMMETRIC_DEFAULT", "false", "true", "true", "365", "30", "false", "false"]
    error_message = "鍵の属性が modules/kms と同じ値になっていません。"
  }

  assert {
    condition = [
      aws_sns_topic.this.name,
      tostring(aws_sns_topic.this.fifo_topic),
      tostring(aws_sns_topic.this.signature_version),
      aws_sns_topic.this.tracing_config,
    ] == ["tabp-sandbox-ops-notification-us-east-1", "false", "2", "PassThrough"]
    error_message = "Topic の名前か属性が modules/sns と同じ値になっていません。"
  }

  assert {
    condition     = aws_kms_alias.this.name == "alias/tabp-sandbox-ops-notification-us-east-1"
    error_message = "鍵の alias が alias/<name> になっていません。"
  }
}

override_resource {
  target = aws_kms_key.this
  values = { arn = "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ac" }
}

override_resource {
  target = aws_sns_topic.this
  values = { arn = "arn:aws:sns:us-east-1:123456789012:tabp-sandbox-ops-notification-us-east-1" }
}

# 鍵の ARN と Topic の ARN は plan では決まらないので、mock の apply で配線を見る。
run "wiring" {
  command = apply

  assert {
    condition     = aws_sns_topic.this.kms_master_key_id == aws_kms_key.this.arn
    error_message = "Topic が us-east-1 の鍵で暗号化されていません。"
  }

  # Key Policy の暗号化コンテキストと Topic Policy の Resource は、名前から組んだ ARN を使う。
  assert {
    condition     = local.topic_arn == aws_sns_topic.this.arn
    error_message = "名前から組んだ Topic の ARN が、作った Topic の ARN と一致しません。"
  }

  assert {
    condition     = aws_sns_topic_policy.this.arn == aws_sns_topic.this.arn && aws_kms_alias.this.target_key_id == aws_kms_key.this.key_id && aws_kms_key_policy.this.key_id == aws_kms_key.this.id
    error_message = "Topic Policy・alias・Key Policy が、この module の Topic と鍵に付いていません。"
  }

  assert {
    condition     = output.topic_arn == "arn:aws:sns:us-east-1:123456789012:tabp-sandbox-ops-notification-us-east-1"
    error_message = "topic_arn の出力が us-east-1 の Topic の ARN になっていません。"
  }
}
