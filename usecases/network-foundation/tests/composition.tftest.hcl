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

run "names_the_flow_logs_resources_from_project_and_env" {
  command = plan

  variables {
    project = "acme"
    env     = "sandbox"
  }

  assert {
    condition     = module.flow_logs.log_group_name == "/acme/sandbox/network-foundation/vpc-flow-logs"
    error_message = "Flow Logs の Log Group の名前が /<project>/<env>/network-foundation/vpc-flow-logs ではありません。"
  }
  assert {
    condition     = module.kms.alias_name == "alias/acme-sandbox-network-flow-logs"
    error_message = "Flow Logs の鍵の alias が <project>-<env>-network-flow-logs ではありません。"
  }
}

run "passes_the_cidr_to_the_vpc" {
  command = plan

  variables {
    cidr = "192.168.0.0/24"
  }

  assert {
    condition     = module.vpc.vpc_cidr == "192.168.0.0/24"
    error_message = "VPC の CIDR が入力と違います。"
  }
}
