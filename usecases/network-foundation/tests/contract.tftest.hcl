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

# modules/log-group の kms_key_arn の validation と、modules/vpc の flow_logs_log_group_arn の validation を通すため、ARN を形のある値にする。
override_resource {
  target = module.kms.aws_kms_key.this
  values = { arn = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab" }
}

override_resource {
  target = module.flow_logs.aws_cloudwatch_log_group.this
  values = { arn = "arn:aws:logs:ap-northeast-1:123456789012:log-group:/tabp/dev/network-foundation/vpc-flow-logs" }
}

override_resource {
  target = module.vpc.aws_iam_role.flow_logs
  values = { arn = "arn:aws:iam::123456789012:role/tabp-dev-vpc-flow-logs" }
}

variables {
  project                       = "tabp"
  env                           = "dev"
  cidr                          = "10.0.0.0/16"
  availability_zone_ids         = ["apne1-az1", "apne1-az2", "apne1-az4"]
  flow_logs_retention_days      = 365
  flow_logs_deletion_protection = true
}

run "outputs_return_the_module_identifiers" {
  command = apply

  assert {
    condition     = output.vpc_id == module.vpc.vpc_id
    error_message = "vpc_id が VPC の ID ではありません。"
  }
  assert {
    condition     = output.private_subnet_ids == module.subnet_tiers.private_subnet_ids && length(output.private_subnet_ids) == 3
    error_message = "private_subnet_ids が private の tier の subnet を AZ ごとに返していません。"
  }
  assert {
    condition     = output.isolated_subnet_ids == module.subnet_tiers.isolated_subnet_ids && length(output.isolated_subnet_ids) == 3
    error_message = "isolated_subnet_ids が isolated の tier の subnet を AZ ごとに返していません。"
  }
  assert {
    condition     = length(setintersection(toset(output.private_subnet_ids), toset(output.isolated_subnet_ids))) == 0
    error_message = "private と isolated の subnet が重なっています。"
  }
  assert {
    condition     = output.private_route_table_ids == module.subnet_tiers.private_route_table_ids && length(output.private_route_table_ids) == 3
    error_message = "private_route_table_ids が private の tier の Route Table を AZ ごとに返していません。"
  }
  assert {
    condition     = output.flow_logs_log_group_arn == "arn:aws:logs:ap-northeast-1:123456789012:log-group:/tabp/dev/network-foundation/vpc-flow-logs"
    error_message = "flow_logs_log_group_arn が Flow Logs の Log Group の ARN ではありません。"
  }
}
