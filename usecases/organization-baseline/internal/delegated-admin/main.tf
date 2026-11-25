# 信頼アクセスの有効化は、GuardDuty・Inspector・Security Hub では指定の API が呼び手の権限で行い、CloudTrail・Config・
# Access Analyzer では人の手順が先に行う（organization-baseline/ADR-0001 決定5）。

# GuardDuty・Inspector・Security Hub の指定はリージョンごとで、委任管理者はすべてのリージョンで同じアカウントでなければならない。
resource "aws_guardduty_organization_admin_account" "this" {
  for_each = var.regions

  region           = each.key
  admin_account_id = var.admin_account_id
}

resource "aws_inspector2_delegated_admin_account" "this" {
  for_each = var.regions

  region     = each.key
  account_id = var.admin_account_id
}

# Organizations の API で指定すると Security Hub の側に反映されないので、Security Hub の API で指定する。
resource "aws_securityhub_organization_admin_account" "this" {
  for_each = var.regions

  region           = each.key
  admin_account_id = var.admin_account_id
}

# Organizations の API で登録すると CloudTrail の service-linked role が作られないので、CloudTrail の API で登録する。
resource "aws_cloudtrail_organization_delegated_admin_account" "this" {
  account_id = var.admin_account_id
}

resource "aws_organizations_delegated_administrator" "this" {
  for_each = toset(["access-analyzer.amazonaws.com", "config.amazonaws.com"])

  account_id        = var.admin_account_id
  service_principal = each.key
}
