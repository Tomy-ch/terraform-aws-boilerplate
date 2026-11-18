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

variables {
  name        = "tabp-dev-ops-notification"
  kms_key_arn = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  publishers  = ["cloudwatch"]
}

run "fixes_topic_attributes" {
  command = plan

  assert {
    condition     = aws_sns_topic.this.name == var.name && aws_sns_topic.this.kms_master_key_id == var.kms_key_arn
    error_message = "Topic の名前か鍵が入力と一致しません。"
  }
  assert {
    condition     = aws_sns_topic.this.fifo_topic == false
    error_message = "fifo_topic が false ではありません。"
  }
  assert {
    condition     = aws_sns_topic.this.signature_version == 2
    error_message = "signature_version が 2（SHA256）ではありません。"
  }
  assert {
    condition     = aws_sns_topic.this.tracing_config == "PassThrough"
    error_message = "tracing_config が PassThrough ではありません。"
  }
  assert {
    condition     = aws_sns_topic.this.delivery_policy == null && aws_sns_topic.this.display_name == null
    error_message = "delivery_policy か display_name を書いています。"
  }
  assert {
    condition     = aws_sns_topic.this.tags == null
    error_message = "tag を書いています。"
  }
}

run "outputs_topic_arn" {
  command = apply

  override_resource {
    target = aws_sns_topic.this
    values = { arn = "arn:aws:sns:ap-northeast-1:123456789012:tabp-dev-ops-notification" }
  }

  assert {
    condition     = output.topic_arn == aws_sns_topic.this.arn
    error_message = "topic_arn が Topic の ARN ではありません。"
  }
  assert {
    condition     = aws_sns_topic_policy.this.arn == aws_sns_topic.this.arn
    error_message = "Topic Policy がこの Topic を指していません。"
  }
}
