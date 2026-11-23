mock_provider "aws" {
  mock_data "aws_region" {
    defaults = { region = "ap-northeast-1" }
  }
  mock_resource "aws_iam_service_linked_role" {
    defaults = { arn = "arn:aws:iam::123456789012:role/aws-service-role/config.amazonaws.com/AWSServiceRoleForConfig" }
  }
}

variables {
  name_prefix = "tabp-dev"
  regions     = ["ap-northeast-1", "us-east-1"]
  delivery = {
    bucket_name = "tabp-log-archive-audit-evidence-210987654321-ap-northeast-1-an"
    kms_key_arn = "arn:aws:kms:ap-northeast-1:210987654321:key/11111111-2222-3333-4444-555555555555"
  }
  retention_days = 2557
}

run "recorder_per_region" {
  command = plan

  assert {
    condition     = aws_iam_service_linked_role.config.aws_service_name == "config.amazonaws.com"
    error_message = "Config の service-linked role を作っていません（Config.1）。"
  }

  assert {
    condition = alltrue([for k, r in aws_config_configuration_recorder.this :
    r.region == k && r.name == "tabp-dev" && r.recording_group[0].all_supported && r.recording_mode[0].recording_frequency == "CONTINUOUS"])
    error_message = "各リージョンの recorder が、すべての resource を継続して記録していません。"
  }

  assert {
    condition     = output.recorders == { "ap-northeast-1" = true, "us-east-1" = false }
    error_message = "グローバルな resource を記録するのが、home のリージョンだけになっていません。"
  }

  assert {
    condition     = length(output.delivery) == 1 && output.delivery[0].bucket_name == var.delivery.bucket_name && output.delivery[0].kms_key_arn == var.delivery.kms_key_arn
    error_message = "delivery が、各リージョンの delivery channel の配信先に渡っていません。"
  }

  assert {
    condition     = toset(keys(aws_config_configuration_recorder_status.this)) == var.regions && alltrue([for k, s in aws_config_configuration_recorder_status.this : s.is_enabled && s.name == "tabp-dev" && s.region == k])
    error_message = "各リージョンの recorder が有効になっていません。"
  }

  assert {
    condition     = toset(keys(aws_config_retention_configuration.this)) == var.regions && alltrue([for k, r in aws_config_retention_configuration.this : r.retention_period_in_days == 2557 && r.region == k])
    error_message = "retention_days が、各リージョンの履歴の保持日数に渡っていません。"
  }
}

run "home_region_outside" {
  command = plan

  variables {
    regions = ["us-east-1", "us-west-2"]
  }

  expect_failures = [aws_config_configuration_recorder.this]
}

run "empty_regions" {
  command = plan

  variables {
    regions = []
  }

  expect_failures = [var.regions]
}
