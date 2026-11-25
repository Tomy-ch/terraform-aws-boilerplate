mock_provider "aws" {}

variables {
  admin_account_id = "111111111111"
  regions          = ["ap-northeast-1", "us-east-1", "us-west-2"]
}

run "every_service" {
  command = plan

  assert {
    condition     = toset(keys(aws_guardduty_organization_admin_account.this)) == var.regions && alltrue([for k, a in aws_guardduty_organization_admin_account.this : a.region == k && a.admin_account_id == "111111111111"])
    error_message = "GuardDuty の委任管理者が、各リージョンで同じアカウントに指定されていません。"
  }

  assert {
    condition     = toset(keys(aws_inspector2_delegated_admin_account.this)) == var.regions && alltrue([for k, a in aws_inspector2_delegated_admin_account.this : a.region == k && a.account_id == "111111111111"])
    error_message = "Inspector の委任管理者が、各リージョンで同じアカウントに指定されていません。"
  }

  assert {
    condition     = toset(keys(aws_securityhub_organization_admin_account.this)) == var.regions && alltrue([for k, a in aws_securityhub_organization_admin_account.this : a.region == k && a.admin_account_id == "111111111111"])
    error_message = "Security Hub の委任管理者が、各リージョンで同じアカウントに指定されていません。"
  }

  assert {
    condition     = aws_cloudtrail_organization_delegated_admin_account.this.account_id == "111111111111"
    error_message = "CloudTrail の委任管理者が、CloudTrail の API で登録されていません。"
  }

  assert {
    condition     = { for k, a in aws_organizations_delegated_administrator.this : k => a.account_id } == { "access-analyzer.amazonaws.com" = "111111111111", "config.amazonaws.com" = "111111111111" }
    error_message = "AWS Config と IAM Access Analyzer の委任管理者が、Organizations の API で登録されていません。"
  }

  assert {
    condition     = jsonencode(output.service_principals) == jsonencode(["access-analyzer.amazonaws.com", "cloudtrail.amazonaws.com", "config.amazonaws.com", "guardduty.amazonaws.com", "inspector2.amazonaws.com", "securityhub.amazonaws.com"])
    error_message = "委任管理者を指定したサービスが、6つのサービスになっていません。"
  }
}

run "account_id_not_12_digits" {
  command = plan

  variables {
    admin_account_id = "11111111111"
  }

  expect_failures = [var.admin_account_id]
}

run "empty_regions" {
  command = plan

  variables {
    regions = []
  }

  expect_failures = [var.regions]
}
