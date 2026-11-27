output "root_id" {
  description = "組織の Root の ID。メンバーの break-glass の role を配る StackSet の展開先。"
  value       = local.root_id
}

output "account_ids" {
  description = "メンバーアカウントの env から ID への map。"
  value       = { for k, a in aws_organizations_account.this : k => a.id }
}

output "bootstrapping_account_ids" {
  description = "立ち上げ中のメンバーアカウントの env から ID への map。"
  value       = { for k, a in aws_organizations_account.this : k => a.id if contains(var.bootstrapping, k) }
}

output "delegated_admin" {
  description = "委任管理者にしたアカウントの ID、指定したサービスの主体、GuardDuty・Inspector・Security Hub を指定したリージョンの集合。usecase の Contract Test が配線を確かめるのに使う。"
  value = {
    account_id         = aws_cloudtrail_organization_delegated_admin_account.this.account_id
    service_principals = sort(concat(["cloudtrail.amazonaws.com", "guardduty.amazonaws.com", "inspector2.amazonaws.com", "securityhub.amazonaws.com"], keys(aws_organizations_delegated_administrator.this)))
    regions            = toset(keys(aws_guardduty_organization_admin_account.this))
  }
}

output "management_account_id" {
  description = "組織の管理アカウントの ID。"
  value       = aws_organizations_organization.this.master_account_id
}
