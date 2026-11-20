output "log_group_arn" {
  description = "クエリログの Log Group の ARN（us-east-1。末尾の :* を持たない）。resource policy の書き込みの後に決まる値として渡し、query log の作成を policy の後にする。"
  value       = module.log_group.log_group_arn
  depends_on  = [aws_cloudwatch_log_resource_policy.this]
}

output "log_group_name" {
  description = "クエリログの Log Group の名前。"
  value       = module.log_group.log_group_name
}

output "retention_days" {
  description = "クエリログの Log Group に置いた保持日数。"
  value       = module.log_group.retention_days
}

output "deletion_protection" {
  description = "クエリログの Log Group に置いた削除の保護。"
  value       = module.log_group.deletion_protection
}
