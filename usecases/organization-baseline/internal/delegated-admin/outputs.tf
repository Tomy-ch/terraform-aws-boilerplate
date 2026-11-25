output "service_principals" {
  description = "委任管理者を指定したサービスの主体。usecase の Contract Test が、6つのサービスが揃っていることを確かめるのに使う。"
  value       = sort(concat(["cloudtrail.amazonaws.com", "guardduty.amazonaws.com", "inspector2.amazonaws.com", "securityhub.amazonaws.com"], keys(aws_organizations_delegated_administrator.this)))
}
