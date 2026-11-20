mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }
}

variables {
  name      = "tabp-prd-dns-dnssec"
  zone_name = "example.com"
}

run "name_rejects_uppercase" {
  command = plan

  variables {
    name = "Tabp-prd-dns-dnssec"
  }

  expect_failures = [var.name]
}

run "name_rejects_slash" {
  command = plan

  variables {
    name = "tabp/prd"
  }

  expect_failures = [var.name]
}
