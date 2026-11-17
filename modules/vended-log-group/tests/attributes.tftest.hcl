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
  mock_resource "aws_cloudwatch_log_group" {
    defaults = { arn = "arn:aws:logs:ap-northeast-1:123456789012:log-group:tabp-admin-state-access" }
  }
  mock_resource "aws_cloudwatch_log_delivery_destination" {
    defaults = { arn = "arn:aws:logs:ap-northeast-1:123456789012:delivery-destination:tabp-admin-state-access" }
  }
}

variables {
  name                = "tabp-admin-state-access"
  retention_days      = 365
  kms_key_arn         = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  deletion_protection = true
  delivery_sources    = ["tabp-admin-tfstate"]
}

run "fixes_class_and_destroy" {
  command = plan

  assert {
    condition     = aws_cloudwatch_log_group.this.log_group_class == "STANDARD"
    error_message = "log_group_class が STANDARD ではありません。"
  }
  assert {
    condition     = aws_cloudwatch_log_group.this.skip_destroy == false
    error_message = "skip_destroy が false ではありません。"
  }
  assert {
    condition     = aws_cloudwatch_log_group.this.tags == null
    error_message = "Log Group に tag が付いています。"
  }
}

run "passes_inputs_through" {
  command = plan

  variables {
    retention_days = 1096
  }

  assert {
    condition     = aws_cloudwatch_log_group.this.name == var.name
    error_message = "Log Group の名前が入力と違います。"
  }
  assert {
    condition     = aws_cloudwatch_log_group.this.retention_in_days == 1096
    error_message = "retention_in_days が入力と違います。"
  }
  assert {
    condition     = aws_cloudwatch_log_group.this.kms_key_id == var.kms_key_arn
    error_message = "kms_key_id が入力と違います。"
  }
  assert {
    condition     = output.log_group_name == var.name
    error_message = "log_group_name が入力と違います。"
  }
}

run "deletion_protection_enabled" {
  command = plan

  assert {
    condition     = aws_cloudwatch_log_group.this.deletion_protection_enabled == true
    error_message = "deletion_protection = true が Log Group に届いていません。"
  }
}

run "deletion_protection_disabled" {
  command = plan

  variables {
    deletion_protection = false
  }

  assert {
    condition     = aws_cloudwatch_log_group.this.deletion_protection_enabled == false
    error_message = "deletion_protection = false が Log Group に届いていません。"
  }
}

run "outputs_arn_after_apply" {
  command = apply

  assert {
    condition     = output.log_group_arn == aws_cloudwatch_log_group.this.arn
    error_message = "log_group_arn が Log Group の ARN と違います。"
  }
}

run "names_destination_after_log_group" {
  command = apply

  assert {
    condition     = aws_cloudwatch_log_delivery_destination.this.name == var.name
    error_message = "delivery destination の名前が入力と違います。"
  }
  assert {
    condition     = aws_cloudwatch_log_delivery_destination.this.output_format == null
    error_message = "delivery destination に output_format が付いています。"
  }
  assert {
    condition     = aws_cloudwatch_log_delivery_destination.this.tags == null
    error_message = "delivery destination に tag が付いています。"
  }
  assert {
    condition     = one(aws_cloudwatch_log_delivery_destination.this.delivery_destination_configuration).destination_resource_arn == aws_cloudwatch_log_group.this.arn
    error_message = "delivery destination が Log Group を指していません。"
  }
  assert {
    condition     = output.delivery_destination_arn == aws_cloudwatch_log_delivery_destination.this.arn
    error_message = "delivery_destination_arn が delivery destination の ARN と違います。"
  }
}
