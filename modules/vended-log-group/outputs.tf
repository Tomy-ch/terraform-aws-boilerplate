output "log_group_name" {
  description = "Log Group の名前。"
  value       = aws_cloudwatch_log_group.this.name
}

output "log_group_arn" {
  description = "Log Group の ARN。API が付ける :* を除いた形で、log stream への Action の Resource には <arn>:log-stream:* を付ける。"
  value       = aws_cloudwatch_log_group.this.arn
}

output "delivery_destination_arn" {
  description = "delivery destination の ARN。送り側の delivery が名指す。resource policy の後に決まる。"
  value       = aws_cloudwatch_log_delivery_destination.this.arn
}
