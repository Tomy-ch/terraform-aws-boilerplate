output "role_arns" {
  description = "役割（plan / apply / apply_identity / unlock_identity）ごとの role の ARN。"
  # computed の arn を読まず名前から組む。role の置換でも plan で決まり、Key Policy の文書が unknown にならない。
  # 名前は resource から読み、role の作成を待つ順序は保つ（KMS は存在しない principal を名指す Key Policy を拒む）。
  value = { for k, r in local.all_roles : k => "arn:${data.aws_partition.current.partition}:iam::${data.aws_caller_identity.current.account_id}:role/${r.name}" }
}

output "roles" {
  description = "役割ごとの role の名前と、信頼条件が受け入れる GitHub の environment 名。"
  value       = { for k, r in local.roles : k => { name = local.all_roles[k].name, environment = r.environment } }
}

output "role_policies" {
  description = "役割ごとの inline policy の文書（JSON）。"
  value       = { for k, p in local.all_policies : k => p.policy }
}
