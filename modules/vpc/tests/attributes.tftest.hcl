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

run "fixes_vpc_attributes" {
  command = plan

  assert {
    condition     = aws_vpc.this.cidr_block == "10.0.0.0/16"
    error_message = "cidr_block が入力と違います。"
  }
  assert {
    condition     = aws_vpc.this.enable_dns_support == true
    error_message = "enable_dns_support が true ではありません。"
  }
  assert {
    condition     = aws_vpc.this.enable_dns_hostnames == true
    error_message = "enable_dns_hostnames が true ではありません。"
  }
  assert {
    condition     = aws_vpc.this.assign_generated_ipv6_cidr_block == false
    error_message = "assign_generated_ipv6_cidr_block が false ではありません。"
  }
  assert {
    condition     = aws_vpc.this.instance_tenancy == "default"
    error_message = "instance_tenancy が default ではありません。"
  }
  assert {
    condition     = aws_vpc.this.tags == null
    error_message = "VPC に tag が付いています。"
  }
}

# 空の集合は plan で見る。宣言から [] を消すと値が plan で unknown になり、この run が落ちる。
run "empties_default_security_group_and_route_table" {
  command = plan

  assert {
    condition     = length(aws_default_security_group.this.ingress) == 0
    error_message = "既定 Security Group に ingress の規則があります。"
  }
  assert {
    condition     = length(aws_default_security_group.this.egress) == 0
    error_message = "既定 Security Group に egress の規則があります。"
  }
  assert {
    condition     = length(aws_default_route_table.this.route) == 0
    error_message = "既定 Route Table に経路があります。"
  }
  assert {
    condition     = length(aws_default_route_table.this.propagating_vgws) == 0
    error_message = "既定 Route Table に経路の伝播があります。"
  }
}

run "adopts_defaults_of_this_vpc" {
  command = apply

  assert {
    condition     = aws_default_security_group.this.vpc_id == aws_vpc.this.id
    error_message = "既定 Security Group がこの VPC のものではありません。"
  }
  assert {
    condition     = aws_default_route_table.this.default_route_table_id == aws_vpc.this.default_route_table_id
    error_message = "既定 Route Table がこの VPC の main Route Table ではありません。"
  }
}

run "outputs_vpc_identifiers" {
  command = apply

  assert {
    condition     = output.vpc_id == aws_vpc.this.id
    error_message = "vpc_id が VPC の ID ではありません。"
  }
  assert {
    condition     = output.vpc_cidr == "10.0.0.0/16"
    error_message = "vpc_cidr が入力と違います。"
  }
}

run "passes_non_default_inputs_through" {
  command = plan

  variables {
    name = "x-y"
    cidr = "192.168.4.0/24"
  }

  assert {
    condition     = aws_vpc.this.cidr_block == "192.168.4.0/24"
    error_message = "cidr_block が入力に追随していません。"
  }
  assert {
    condition     = output.vpc_cidr == "192.168.4.0/24"
    error_message = "vpc_cidr が入力に追随していません。"
  }
  assert {
    condition     = aws_iam_role.flow_logs.name == "x-y-vpc-flow-logs"
    error_message = "配送 role の名前が入力の name に追随していません。"
  }
}
