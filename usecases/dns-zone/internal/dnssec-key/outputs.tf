output "key_arn" {
  description = "KSK の鍵の ARN（us-east-1）。Key Policy の書き込みの後に決まる値として渡し、KSK の作成を Key Policy の後にする。"
  value       = aws_kms_key.this.arn
  depends_on  = [aws_kms_key_policy.this]
}

output "alias_name" {
  description = "鍵の alias の名前。"
  value       = aws_kms_alias.this.name
}
