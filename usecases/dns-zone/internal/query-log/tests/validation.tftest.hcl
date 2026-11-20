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

run "name_rejects_uppercase" {
  command = plan

  variables {
    name = "Tabp-dev"
  }

  expect_failures = [var.name]
}

run "name_rejects_empty" {
  command = plan

  variables {
    name = ""
  }

  expect_failures = [var.name]
}

run "name_rejects_201_characters" {
  command = plan

  variables {
    name = join("", [for i in range(201) : "a"])
  }

  expect_failures = [var.name]
}

run "name_accepts_200_characters" {
  command = plan

  variables {
    name = join("", [for i in range(200) : "a"])
  }

  assert {
    condition     = length(aws_cloudwatch_log_resource_policy.this.policy_name) > 200
    error_message = "200 文字の name が policy 名へ渡っていません。"
  }
}
