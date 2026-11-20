output "log_group_arn" {
  description = "クエリログの Log Group の ARN（us-east-1。末尾の :* を持たない）。resource policy の書き込みの後に決まる値として渡し、query log の作成を policy の後にする。"
  value       = aws_cloudwatch_log_group.this.arn
  depends_on  = [aws_cloudwatch_log_resource_policy.this]
}

output "log_group_name" {
  description = "クエリログの Log Group の名前。"
  value       = aws_cloudwatch_log_group.this.name
}

output "retention_days" {
  description = "クエリログの Log Group に置いた保持日数。"
  value       = aws_cloudwatch_log_group.this.retention_in_days
}

output "deletion_protection" {
  description = "クエリログの Log Group に置いた削除の保護。"
  value       = aws_cloudwatch_log_group.this.deletion_protection_enabled
}

output "alias_name" {
  description = "クエリログの鍵の alias の名前。"
  value       = aws_kms_alias.this.name
}
