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

variables {
  repository_filter = "tabp-dev/*"
}

run "enhanced_continuous_on_prefix" {
  command = plan

  assert {
    condition     = aws_ecr_registry_scanning_configuration.this.scan_type == "ENHANCED"
    error_message = "registry のスキャンが ENHANCED ではありません。"
  }

  assert {
    condition = [for r in aws_ecr_registry_scanning_configuration.this.rule : {
      frequency = r.scan_frequency
      filters   = [for f in r.repository_filter : { filter = f.filter, type = f.filter_type }]
      }] == [{
      frequency = "CONTINUOUS_SCAN"
      filters   = [{ filter = "tabp-dev/*", type = "WILDCARD" }]
    }]
    error_message = "registry のスキャンの規則が、接頭辞の WILDCARD への CONTINUOUS_SCAN の1つになっていません。"
  }
}

run "rejects_bare_wildcard" {
  command = plan

  variables {
    repository_filter = "*"
  }

  expect_failures = [var.repository_filter]
}
