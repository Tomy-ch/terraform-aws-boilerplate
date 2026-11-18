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

run "trust_policy_is_limited_to_flow_logs_of_this_account" {
  command = apply

  assert {
    condition = jsondecode(aws_iam_role.flow_logs.assume_role_policy) == {
      Version = "2012-10-17"
      Statement = [{
        Sid       = "AllowVpcFlowLogsOfThisAccount"
        Effect    = "Allow"
        Principal = { Service = "vpc-flow-logs.amazonaws.com" }
        Action    = "sts:AssumeRole"
        Condition = {
          StringEquals = { "aws:SourceAccount" = "123456789012" }
          ArnLike      = { "aws:SourceArn" = "arn:aws:ec2:ap-northeast-1:123456789012:vpc-flow-log/*" }
        }
      }]
    }
    error_message = "配送 role の trust policy が期待と違います。"
  }
}

run "permission_policy_names_the_log_group_except_describe_log_groups" {
  command = apply

  assert {
    condition = jsondecode(aws_iam_role_policy.flow_logs.policy) == {
      Version = "2012-10-17"
      Statement = [
        {
          Sid      = "WriteLogEvents"
          Effect   = "Allow"
          Action   = ["logs:CreateLogStream", "logs:PutLogEvents"]
          Resource = "arn:aws:logs:ap-northeast-1:123456789012:log-group:/tabp/dev/network-foundation/flow-logs:log-stream:*"
        },
        {
          Sid      = "DescribeLogStreams"
          Effect   = "Allow"
          Action   = "logs:DescribeLogStreams"
          Resource = "arn:aws:logs:ap-northeast-1:123456789012:log-group:/tabp/dev/network-foundation/flow-logs"
        },
        {
          Sid      = "DescribeLogGroups"
          Effect   = "Allow"
          Action   = "logs:DescribeLogGroups"
          Resource = "*"
        },
      ]
    }
    error_message = "配送 role の permission policy が期待と違います。"
  }
}

run "documents_follow_partition_region_and_account" {
  command = apply

  override_data {
    target = data.aws_caller_identity.current
    values = { account_id = "210987654321" }
  }
  override_data {
    target = data.aws_region.current
    values = { region = "cn-north-1" }
  }
  override_data {
    target = data.aws_partition.current
    values = { partition = "aws-cn" }
  }

  variables {
    flow_logs_log_group_arn = "arn:aws-cn:logs:cn-north-1:210987654321:log-group:/tabp/dev/network-foundation/flow-logs"
  }

  assert {
    condition     = jsondecode(aws_iam_role.flow_logs.assume_role_policy).Statement[0].Condition.StringEquals["aws:SourceAccount"] == "210987654321"
    error_message = "trust policy の aws:SourceAccount が呼んだアカウントに追随していません。"
  }
  assert {
    condition     = jsondecode(aws_iam_role.flow_logs.assume_role_policy).Statement[0].Condition.ArnLike["aws:SourceArn"] == "arn:aws-cn:ec2:cn-north-1:210987654321:vpc-flow-log/*"
    error_message = "trust policy の aws:SourceArn が呼んだ partition・リージョン・アカウントに追随していません。"
  }
  assert {
    condition     = jsondecode(aws_iam_role_policy.flow_logs.policy).Statement[0].Resource == "arn:aws-cn:logs:cn-north-1:210987654321:log-group:/tabp/dev/network-foundation/flow-logs:log-stream:*"
    error_message = "permission policy の log stream の Resource が入力の Log Group に追随していません。"
  }
  assert {
    condition     = jsondecode(aws_iam_role_policy.flow_logs.policy).Statement[1].Resource == "arn:aws-cn:logs:cn-north-1:210987654321:log-group:/tabp/dev/network-foundation/flow-logs"
    error_message = "permission policy の Log Group の Resource が入力の Log Group に追随していません。"
  }
  assert {
    condition     = aws_flow_log.this.log_destination == "arn:aws-cn:logs:cn-north-1:210987654321:log-group:/tabp/dev/network-foundation/flow-logs"
    error_message = "Flow Logs の宛先が入力の Log Group に追随していません。"
  }
}
