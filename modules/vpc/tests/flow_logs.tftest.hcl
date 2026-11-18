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
  mock_resource "aws_iam_role" {
    defaults = { arn = "arn:aws:iam::123456789012:role/tabp-dev-network-vpc-flow-logs" }
  }
}

variables {
  name                    = "tabp-dev-network"
  cidr                    = "10.0.0.0/16"
  flow_logs_log_group_arn = "arn:aws:logs:ap-northeast-1:123456789012:log-group:/tabp/dev/network-foundation/flow-logs"
}

run "fixes_flow_log_attributes" {
  command = plan

  assert {
    condition     = aws_flow_log.this.traffic_type == "ALL"
    error_message = "traffic_type が ALL ではありません。"
  }
  assert {
    condition     = aws_flow_log.this.log_destination_type == "cloud-watch-logs"
    error_message = "log_destination_type が cloud-watch-logs ではありません。"
  }
  assert {
    condition     = aws_flow_log.this.log_destination == var.flow_logs_log_group_arn
    error_message = "log_destination が入力の Log Group ではありません。"
  }
  assert {
    condition     = aws_flow_log.this.max_aggregation_interval == 600
    error_message = "max_aggregation_interval が 600 ではありません。"
  }
  assert {
    condition     = aws_flow_log.this.log_format == "$${version} $${account-id} $${interface-id} $${srcaddr} $${dstaddr} $${srcport} $${dstport} $${protocol} $${packets} $${bytes} $${start} $${end} $${action} $${log-status} $${vpc-id} $${subnet-id} $${instance-id} $${tcp-flags} $${type} $${pkt-srcaddr} $${pkt-dstaddr} $${region} $${az-id} $${pkt-src-aws-service} $${pkt-dst-aws-service} $${flow-direction} $${traffic-path} $${reject-reason} $${interface-type}"
    error_message = "log_format が固定の項目集合と違います。"
  }
  assert {
    condition     = aws_iam_role.flow_logs.name == "tabp-dev-network-vpc-flow-logs"
    error_message = "配送 role の名前が <name>-vpc-flow-logs ではありません。"
  }
  assert {
    condition     = aws_iam_role_policy.flow_logs.name == "deliver-flow-logs"
    error_message = "配送の policy の名前が deliver-flow-logs ではありません。"
  }
  assert {
    condition     = aws_iam_role.flow_logs.tags == null && aws_flow_log.this.tags == null
    error_message = "配送 role か Flow Logs に tag が付いています。"
  }
}

run "connects_flow_log_to_this_vpc_and_role" {
  command = apply

  assert {
    condition     = aws_flow_log.this.vpc_id == aws_vpc.this.id
    error_message = "Flow Logs がこの VPC を対象にしていません。"
  }
  assert {
    condition     = aws_flow_log.this.iam_role_arn == aws_iam_role.flow_logs.arn
    error_message = "Flow Logs の配送 role がこの module の role ではありません。"
  }
  assert {
    condition     = aws_iam_role_policies_exclusive.flow_logs.role_name == aws_iam_role.flow_logs.name
    error_message = "inline policy の排他がこの role を名指していません。"
  }
  assert {
    condition     = aws_iam_role_policies_exclusive.flow_logs.policy_names == toset(["deliver-flow-logs"])
    error_message = "inline policy の排他が配送の policy だけを残していません。"
  }
  assert {
    condition     = aws_iam_role_policy_attachments_exclusive.flow_logs.role_name == aws_iam_role.flow_logs.name
    error_message = "managed policy の排他がこの role を名指していません。"
  }
  assert {
    condition     = length(aws_iam_role_policy_attachments_exclusive.flow_logs.policy_arns) == 0
    error_message = "配送 role に managed policy が付いています。"
  }
  assert {
    condition     = aws_iam_role_policy.flow_logs.role == aws_iam_role.flow_logs.name
    error_message = "配送の policy がこの role に付いていません。"
  }
}
