output "service_principals" {
  description = "委任管理者を指定したサービスの主体。usecase の Contract Test が、6つのサービスが揃っていることを確かめるのに使う。"
  value       = sort(concat(["cloudtrail.amazonaws.com", "guardduty.amazonaws.com", "inspector2.amazonaws.com", "securityhub.amazonaws.com"], keys(aws_organizations_delegated_administrator.this)))
}

output "regions" {
  description = "GuardDuty・Inspector・Security Hub の委任管理者を指定したリージョンの集合。usecase の Contract Test が、partition ごとの集合が届いたことを確かめるのに使う。"
  value       = toset(keys(aws_guardduty_organization_admin_account.this))
}

output "admin_account_id" {
  description = "委任管理者にしたアカウントの ID。usecase の Contract Test が、security のアカウントが渡ったことを確かめるのに使う。"
  value       = aws_cloudtrail_organization_delegated_admin_account.this.account_id
}
