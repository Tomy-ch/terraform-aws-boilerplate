output "break_glass_arns" {
  description = "管理アカウントの break-glass の IAM user・group・role の ARN。`deployment-identity` が apply の role にこれらの変更を拒ませる。"
  value       = module.break_glass.arns
}

output "account_ids" {
  description = "メンバーアカウントの env から ID への map。"
  value       = module.organization.account_ids
}

output "account_ids_parameter_arn" {
  description = "メンバーアカウントの env から ID への map（JSON）を持ち、RAM で組織へ共有する parameter の ARN。メンバーの root がこの ARN で読む（repository-wide/ADR-0605）。"
  value       = module.account_ids.parameter_arn
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
