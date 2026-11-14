output "role_arns" {
  description = "役割（plan / apply / apply_identity / unlock_identity）ごとの role の ARN。"
  # computed の arn を読まず名前から組む。role の置換でも plan で決まり、Key Policy の文書が unknown にならない。
  # r を読むのは role の作成を待つ依存のためで、KMS は存在しない principal を名指す Key Policy を拒む。
  value = { for k, r in local.all_roles : k => local.role_arns[k] if r.name == local.roles[k].name }
}

output "roles" {
  description = "役割ごとの role の名前と、信頼条件が受け入れる GitHub の environment 名。"
  value       = { for k, r in local.roles : k => { name = local.all_roles[k].name, environment = r.environment } }
}

output "role_policies" {
  description = "役割ごとの inline policy の文書（JSON）。"
  value       = { for k, p in local.all_policies : k => p.policy }
}
