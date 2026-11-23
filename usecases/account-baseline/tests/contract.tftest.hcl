mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }
  mock_data "aws_region" {
    defaults = { region = "ap-northeast-1" }
  }
  mock_data "aws_regions" {
    defaults = { names = ["ap-northeast-1", "us-east-1"] }
  }
  mock_resource "aws_iam_service_linked_role" {
    defaults = { arn = "arn:aws:iam::123456789012:role/aws-service-role/config.amazonaws.com/AWSServiceRoleForConfig" }
  }
}

variables {
  project = "tabp"
  env     = "dev"
  config_delivery = {
    bucket_name = "tabp-log-archive-audit-evidence-210987654321-ap-northeast-1-an"
    kms_key_arn = "arn:aws:kms:ap-northeast-1:210987654321:key/11111111-2222-3333-4444-555555555555"
  }
  config_history_retention_days = 2557
  security_contact = {
    name          = "Security Team"
    title         = "Security"
    email_address = "security@example.com"
    phone_number  = "+81 3 0000 0000"
  }
}

run "workload_account" {
  command = plan

  assert {
    condition     = length(module.regional_controls.regions) == 17 && contains(module.regional_controls.regions, "ap-northeast-1") && !contains(module.regional_controls.regions, "af-south-1")
    error_message = "リージョンごとの設定が、aws の partition の既定で有効な17のリージョンに置かれていません。"
  }

  assert {
    condition     = length(module.config_recorder) == 1 && module.config_recorder[0].recorders["ap-northeast-1"] && length([for k, v in module.config_recorder[0].recorders : k if v]) == 1
    error_message = "Config の recorder が置かれていないか、グローバルな resource を記録するのが provider のリージョンだけになっていません。"
  }

  assert {
    condition     = module.config_recorder[0].delivery[0].bucket_name == var.config_delivery.bucket_name && module.config_recorder[0].delivery[0].kms_key_arn == var.config_delivery.kms_key_arn
    error_message = "config_delivery が Config の配信先に渡っていません。"
  }

  assert {
    condition     = module.account_controls.password_policy_count == 1
    error_message = "管理アカウントの外で、IAM の password policy を置いていません。"
  }
}

run "sandbox_has_no_config" {
  command = plan

  variables {
    env = "sandbox"
  }

  assert {
    condition     = length(module.config_recorder) == 0
    error_message = "sandbox に AWS Config を置いています（#86、account-baseline/ADR-0102）。"
  }

  assert {
    condition     = length(module.regional_controls.regions) == 17
    error_message = "sandbox でも、リージョンごとの設定は置かなければなりません。"
  }
}

run "management_account_has_no_password_policy" {
  command = plan

  variables {
    env = "admin"
  }

  assert {
    condition     = module.account_controls.password_policy_count == 0
    error_message = "管理アカウントに password policy を置いています（organization-baseline の break-glass が持つ）。"
  }
}

run "unsupported_partition" {
  command = plan

  override_data {
    target = data.aws_partition.current
    values = { partition = "aws-iso" }
  }

  expect_failures = [data.aws_partition.current]
}
