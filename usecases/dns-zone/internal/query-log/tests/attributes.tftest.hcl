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
  target = aws_kms_key.this
  values = { arn = "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab" }
}

override_resource {
  target = aws_cloudwatch_log_group.this
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
    condition     = output.alias_name == "alias/tabp-dev-dns-query-logs"
    error_message = "クエリログの鍵の alias が <name>-dns-query-logs ではありません。"
  }
  assert {
    condition     = aws_cloudwatch_log_group.this.name == "/aws/route53/dev.example.com" && output.log_group_name == "/aws/route53/dev.example.com"
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

run "key_and_log_group_are_in_us_east_1" {
  command = plan

  assert {
    condition     = aws_kms_key.this.region == "us-east-1" && aws_kms_alias.this.region == "us-east-1" && aws_kms_key_policy.this.region == "us-east-1" && aws_cloudwatch_log_group.this.region == "us-east-1"
    error_message = "クエリログの鍵・alias・Key Policy・Log Group が us-east-1 に置かれていません。"
  }
  assert {
    condition     = aws_kms_key.this.key_usage == "ENCRYPT_DECRYPT" && aws_kms_key.this.customer_master_key_spec == "SYMMETRIC_DEFAULT" && aws_kms_key.this.multi_region == false
    error_message = "クエリログの鍵が単一リージョンの対称鍵ではありません。"
  }
  assert {
    condition     = aws_kms_key.this.enable_key_rotation == true && aws_kms_key.this.rotation_period_in_days == 365 && aws_kms_key.this.deletion_window_in_days == 30
    error_message = "クエリログの鍵の rotation か削除の猶予が想定と違います。"
  }
  assert {
    condition     = aws_kms_key.this.bypass_policy_lockout_safety_check == false && aws_kms_key_policy.this.bypass_policy_lockout_safety_check == false
    error_message = "lockout の安全チェックを外しています。"
  }
  assert {
    condition     = aws_cloudwatch_log_group.this.log_group_class == "STANDARD" && aws_cloudwatch_log_group.this.skip_destroy == false
    error_message = "Log Group の class か skip_destroy が想定と違います。"
  }
  assert {
    condition     = aws_cloudwatch_log_group.this.kms_key_id == "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
    error_message = "Log Group がクエリログの鍵で暗号化されていません。"
  }
}

run "key_policy_allows_logs_only_for_this_log_group" {
  command = plan

  assert {
    condition = [for s in jsondecode(aws_kms_key_policy.this.policy).Statement : s if s.Sid == "LogsService"][0] == {
      Sid       = "LogsService"
      Effect    = "Allow"
      Principal = { Service = "logs.us-east-1.amazonaws.com" }
      Action    = ["kms:Decrypt", "kms:Describe*", "kms:Encrypt", "kms:GenerateDataKey*", "kms:ReEncrypt*"]
      Resource  = "*"
      Condition = { ArnEquals = { "kms:EncryptionContext:aws:logs:arn" = [
        "arn:aws:logs:us-east-1:123456789012:log-group:/aws/route53/dev.example.com",
      ] } }
    }
    error_message = "Key Policy の CloudWatch Logs の文が、us-east-1 の service principal とこの Log Group の ARN に限った形になっていません。"
  }
  assert {
    condition     = length(jsondecode(aws_kms_key_policy.this.policy).Statement) == 2
    error_message = "Key Policy が、管理の委任と CloudWatch Logs の2文のほかの文を持っています。"
  }
  assert {
    condition     = [for s in jsondecode(aws_kms_key_policy.this.policy).Statement : s.Principal if s.Sid == "AccountKeyAdministration"][0] == { AWS = "arn:aws:iam::123456789012:root" }
    error_message = "管理の委任の相手がアカウントの root ではありません。"
  }
  assert {
    condition = length(setintersection([for s in jsondecode(aws_kms_key_policy.this.policy).Statement : s.Action if s.Sid == "AccountKeyAdministration"][0], [
      "kms:Decrypt", "kms:Encrypt", "kms:GenerateDataKey", "kms:ReEncrypt*", "kms:CreateGrant", "kms:*",
    ])) == 0
    error_message = "管理の委任が暗号操作か grant を含んでいます（ADR-0301 決定12）。"
  }
}
