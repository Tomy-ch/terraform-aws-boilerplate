output "arns" {
  description = "管理アカウントの break-glass の IAM user・group・role の ARN。"
  value       = concat(sort([for u in aws_iam_user.this : u.arn]), [aws_iam_group.this.arn, aws_iam_role.this.arn])
}

output "role_arn" {
  description = "管理アカウントの break-glass の role の ARN。"
  value       = aws_iam_role.this.arn
}
