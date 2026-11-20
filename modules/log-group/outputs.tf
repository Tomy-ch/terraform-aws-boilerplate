output "log_group_name" {
  description = "Log Group の名前。"
  value       = aws_cloudwatch_log_group.this.name
}

output "log_group_arn" {
  description = "Log Group の ARN。API が付ける :* を除いた形で、log stream への Action の Resource には <arn>:log-stream:* を付ける。"
  value       = aws_cloudwatch_log_group.this.arn
}

output "retention_days" {
  description = "Log Group に置いた保持日数。"
  value       = aws_cloudwatch_log_group.this.retention_in_days
}

output "deletion_protection" {
  description = "Log Group に置いた削除の保護。"
  value       = aws_cloudwatch_log_group.this.deletion_protection_enabled
}
