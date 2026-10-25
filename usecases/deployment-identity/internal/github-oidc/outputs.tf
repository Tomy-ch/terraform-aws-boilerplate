output "role_arns" {
  description = "役割（plan / apply / apply_identity）ごとの role の ARN。"
  value       = { for k, r in aws_iam_role.this : k => r.arn }
}
