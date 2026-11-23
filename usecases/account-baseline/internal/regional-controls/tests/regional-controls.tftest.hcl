mock_provider "aws" {
  mock_data "aws_regions" {
    defaults = { names = ["ap-northeast-1", "us-east-1"] }
  }
}

variables {
  name_prefix = "tabp-dev"
  regions     = ["ap-northeast-1", "us-east-1", "us-west-2"]
}

run "every_region" {
  command = plan

  assert {
    condition     = toset(keys(aws_ebs_encryption_by_default.this)) == var.regions && alltrue([for k, r in aws_ebs_encryption_by_default.this : r.enabled && r.region == k])
    error_message = "EBS の既定の暗号化が、各リージョンで有効になっていません（EC2.7）。"
  }

  assert {
    condition     = toset(keys(aws_accessanalyzer_analyzer.this)) == var.regions && alltrue([for k, a in aws_accessanalyzer_analyzer.this : a.type == "ACCOUNT" && a.analyzer_name == "tabp-dev-external" && a.region == k])
    error_message = "各リージョンに、type ACCOUNT の analyzer が <name_prefix>-external の名前で置かれていません（IAM.28）。"
  }

  assert {
    condition = alltrue([for k, s in aws_ssm_service_setting.document_public_sharing :
    s.setting_value == "Disable" && s.setting_id == "/ssm/documents/console/public-sharing-permission" && s.region == k])
    error_message = "SSM document の公開共有が、各リージョンで Disable になっていません（SSM.7）。"
  }

  assert {
    condition = alltrue([for k, s in aws_ssm_service_setting.automation_log_destination :
    s.setting_value == "CloudWatch" && s.setting_id == "/ssm/automation/customer-script-log-destination" && s.region == k])
    error_message = "SSM Automation のログの出力先が、各リージョンで CloudWatch になっていません（SSM.6）。"
  }

  assert {
    condition = alltrue([for k, e in aws_emr_block_public_access_configuration.this :
    e.block_public_security_group_rules && e.permitted_public_security_group_rule_range[0].min_range == 22 && e.permitted_public_security_group_rule_range[0].max_range == 22 && e.region == k])
    error_message = "EMR の block public access が、22 だけを許す形で各リージョンに置かれていません（EMR.2）。"
  }

  assert {
    condition     = length(aws_ssm_service_setting.document_public_sharing) == 3 && length(aws_ssm_service_setting.automation_log_destination) == 3 && length(aws_emr_block_public_access_configuration.this) == 3
    error_message = "リージョンごとの設定の数が、regions の数と合いません。"
  }
}

run "opt_in_region_enabled" {
  command = plan

  override_data {
    target = data.aws_regions.enabled
    values = { names = ["ap-northeast-1", "af-south-1"] }
  }

  expect_failures = [data.aws_regions.enabled]
}

run "empty_regions" {
  command = plan

  variables {
    regions = []
  }

  expect_failures = [var.regions]
}
