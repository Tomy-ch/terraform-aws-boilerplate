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
  mock_data "aws_availability_zones" {
    defaults = { zone_ids = ["apne1-az1", "apne1-az2", "apne1-az4"] }
  }
}

override_resource {
  target = module.network_foundation.module.flow_logs.aws_cloudwatch_log_group.this
  values = { arn = "arn:aws:logs:ap-northeast-1:123456789012:log-group:/tabp/prd/network-foundation/vpc-flow-logs" }
}

override_resource {
  target = module.network_foundation.module.kms.aws_kms_key.this
  values = { arn = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab" }
}

override_resource {
  target = module.network_foundation.module.vpc.aws_iam_role.flow_logs
  values = { arn = "arn:aws:iam::123456789012:role/tabp-prd-vpc-flow-logs" }
}

run "reference_spans_three_availability_zones" {
  command = apply

  assert {
    condition     = length(module.network_foundation.private_subnet_ids) == 3 && length(module.network_foundation.isolated_subnet_ids) == 3 && length(module.network_foundation.private_route_table_ids) == 3
    error_message = "reference の構成が 3 つの AZ に tier ごとの subnet と Route Table を持っていません。"
  }
  assert {
    condition     = module.network_foundation.flow_logs_log_group_arn == "arn:aws:logs:ap-northeast-1:123456789012:log-group:/tabp/prd/network-foundation/vpc-flow-logs"
    error_message = "reference の構成のログの所在が Flow Logs の Log Group ではありません。"
  }
}
