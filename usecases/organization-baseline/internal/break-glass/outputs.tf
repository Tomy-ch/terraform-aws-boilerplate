output "arns" {
  description = "管理アカウントの break-glass の IAM user 2つ・group・role の ARN。path と名前から組むので、plan の時点で決まる。"
  value       = local.arns
}

output "role_arn" {
  description = "管理アカウントの break-glass の role の ARN。"
  value       = aws_iam_role.this.arn
}
