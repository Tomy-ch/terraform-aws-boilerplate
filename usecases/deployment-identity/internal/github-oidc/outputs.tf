output "role_arns" {
  description = "役割（plan / apply / apply_identity）ごとの role の ARN。"
  value       = { for k, r in aws_iam_role.this : k => r.arn }
}

output "roles" {
  description = "役割ごとの role の名前と、信頼条件が受け入れる GitHub の environment 名。"
  value       = { for k, r in local.roles : k => { name = aws_iam_role.this[k].name, environment = r.environment } }
}

output "role_policies" {
  description = "役割ごとの inline policy の文書（JSON）。"
  value       = { for k, p in aws_iam_role_policy.this : k => p.policy }
}
