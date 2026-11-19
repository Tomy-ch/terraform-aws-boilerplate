output "break_glass_arns" {
  description = "管理アカウントの break-glass の IAM user・group・role の ARN。`deployment-identity` が apply の role にこれらの変更を拒ませる。"
  value       = module.break_glass.arns
}
