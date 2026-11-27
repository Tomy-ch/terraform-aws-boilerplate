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

output "bootstrap_read_role" {
  description = "メンバーアカウントへ配る、立ち上げの plan の読み取りの role の名前と path。`deployment-identity` が、その role を配る StackSet の権限と、plan の role の引き受けの Resource に使う。"
  value = {
    name = local.bootstrap_read_role_name
    path = local.bootstrap_read_path
  }
}

output "member_break_glass_role" {
  description = "メンバーアカウントへ配る break-glass の role の名前と path。管理アカウントの identity root が、立ち上げの中のメンバーの `deployment-identity` に渡す break-glass の ARN を組むのに使う。"
  value = {
    name = local.break_glass_role_name
    path = local.break_glass_path
  }
}
