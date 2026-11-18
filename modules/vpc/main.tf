# kics-scan disable=fd632aaf-b8a1-424d-a4d1-0de22fd3247a,e592a0c5-5bdb-414c-9066-5dba7cdea370,e38a8e0a-b88b-4902-b3fe-b0fcb17d5c10
# fd632aaf（Network Firewall 無し）: この module の VPC は外部 egress の経路を持たない。egress は usecase ごとに必要な範囲だけを持つ（ADR-0301 決定16）。Network Firewall は、外部 egress を有効にする環境で経路を持つ usecase が置く。撤回: 外部 egress を持たない VPC が既定でなくなったとき。
# e592a0c5（IAM Access Analyzer）: アカウント単位の統制で、この module の resource を親に取らない。撤回: account-baseline を実装したとき。
# e38a8e0a（tag）: tag は共通の単一入力でだけ付ける（ADR-0204 決定5）。この module はその入力を持たない。撤回: 共通の tag 入力を配線したとき。

data "aws_caller_identity" "current" {}
data "aws_region" "current" {}
data "aws_partition" "current" {}

locals {
  log_format = join(" ", [for field in [
    "version", "account-id", "interface-id", "srcaddr", "dstaddr", "srcport", "dstport", "protocol",
    "packets", "bytes", "start", "end", "action", "log-status",
    "vpc-id", "subnet-id", "instance-id", "tcp-flags", "type", "pkt-srcaddr", "pkt-dstaddr",
    "region", "az-id",
    "pkt-src-aws-service", "pkt-dst-aws-service", "flow-direction", "traffic-path",
    "reject-reason",
    "interface-type",
  ] : "$${${field}}"])
}

resource "aws_vpc" "this" {
  cidr_block                       = var.cidr
  instance_tenancy                 = "default"
  enable_dns_support               = true
  enable_dns_hostnames             = true
  assign_generated_ipv6_cidr_block = false
}

resource "aws_default_route_table" "this" {
  default_route_table_id = aws_vpc.this.default_route_table_id
  route                  = []
  propagating_vgws       = []
}

resource "aws_flow_log" "this" {
  vpc_id                   = aws_vpc.this.id
  traffic_type             = "ALL"
  log_destination_type     = "cloud-watch-logs"
  log_destination          = var.flow_logs_log_group_arn
  iam_role_arn             = aws_iam_role.flow_logs.arn
  max_aggregation_interval = 600
  log_format               = local.log_format

  # 配送の権限を role に付けてから作り、外してから消す。
  depends_on = [aws_iam_role_policy.flow_logs]

  lifecycle {
    # validation が他の object を参照できるのは Terraform 1.9 からで、下限の 1.7 では precondition に置く。
    precondition {
      condition     = startswith(var.flow_logs_log_group_arn, "arn:${data.aws_partition.current.partition}:logs:${data.aws_region.current.region}:${data.aws_caller_identity.current.account_id}:log-group:")
      error_message = "flow_logs_log_group_arn が、VPC と同じ partition・リージョン・アカウントの Log Group ではありません。"
    }
  }
}
