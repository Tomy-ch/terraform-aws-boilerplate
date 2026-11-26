output "break_glass_arns" {
  description = "管理アカウントの break-glass の IAM user・group・role の ARN。`deployment-identity` が apply の role にこれらの変更を拒ませる。"
  value       = module.break_glass.arns
}

output "account_ids" {
  description = "メンバーアカウントの env から ID への map。"
  value       = module.organization.account_ids
}

output "bootstrapping_account_ids" {
  description = "`bootstrapping` に入れたメンバーアカウントの env から ID への map。管理アカウントの identity root が、立ち上げの provider の引き受け先に使う。"
  value       = module.organization.bootstrapping_account_ids
}
