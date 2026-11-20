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
}

override_resource {
  target = module.kms.aws_kms_key.this
  values = { arn = "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab" }
}

override_resource {
  target = module.log_group.aws_cloudwatch_log_group.this
  values = { arn = "arn:aws:logs:us-east-1:123456789012:log-group:/aws/route53/dev.example.com" }
}

variables {
  name                = "tabp-dev"
  zone_name           = "dev.example.com"
  retention_days      = 365
  deletion_protection = true
}

run "alias_log_group_and_policy_follow_name_and_zone_in_us_east_1" {
  command = plan

  assert {
    condition     = module.kms.alias_name == "alias/tabp-dev-dns-query-logs"
    error_message = "クエリログの鍵の alias が <name>-dns-query-logs ではありません。"
  }
  assert {
    condition     = module.log_group.log_group_name == "/aws/route53/dev.example.com" && output.log_group_name == "/aws/route53/dev.example.com"
    error_message = "Log Group の名前が /aws/route53/<zone_name> ではありません。"
  }
  assert {
    condition     = aws_cloudwatch_log_resource_policy.this.region == "us-east-1"
    error_message = "resource policy が us-east-1 に置かれていません。"
  }
}

run "resource_policy_allows_route53_to_write_only_this_log_group" {
  command = apply

  assert {
    condition     = aws_cloudwatch_log_resource_policy.this.policy_name == "tabp-dev-dns-query-logs"
    error_message = "resource policy の名前が <name>-dns-query-logs ではありません。"
  }
  assert {
    condition = jsondecode(aws_cloudwatch_log_resource_policy.this.policy_document) == {
      Version = "2012-10-17"
      Statement = [{
        Sid       = "Route53QueryLogs"
        Effect    = "Allow"
        Principal = { Service = "route53.amazonaws.com" }
        Action    = ["logs:CreateLogStream", "logs:PutLogEvents"]
        Resource  = "arn:aws:logs:us-east-1:123456789012:log-group:/aws/route53/dev.example.com:*"
        Condition = { StringEquals = { "aws:SourceAccount" = "123456789012" } }
      }]
    }
    error_message = "resource policy が、Route 53 にこの Log Group への書き込みだけをアカウントで限って許す形になっていません。"
  }
  assert {
    condition     = output.log_group_arn == "arn:aws:logs:us-east-1:123456789012:log-group:/aws/route53/dev.example.com"
    error_message = "log_group_arn が Log Group の ARN ではありません。"
  }
}

run "passes_retention_and_deletion_protection_to_the_log_group" {
  command = plan

  variables {
    retention_days      = 731
    deletion_protection = false
  }

  assert {
    condition     = output.retention_days == 731
    error_message = "保持日数が Log Group に届いていません。"
  }
  assert {
    condition     = output.deletion_protection == false
    error_message = "deletion_protection = false が Log Group に届いていません。"
  }
}

run "passes_deletion_protection_enabled_to_the_log_group" {
  command = plan

  assert {
    condition     = output.deletion_protection == true
    error_message = "deletion_protection = true が Log Group に届いていません。"
  }
}
