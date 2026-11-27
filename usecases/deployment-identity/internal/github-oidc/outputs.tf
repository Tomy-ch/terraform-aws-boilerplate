output "role_arns" {
  description = "役割（plan / apply / apply_identity / unlock_identity / unlock）ごとの role の ARN。"
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

output "governance_read_policy" {
  description = "管理アカウントの plan と apply-identity の role が共有する、統制の読み取りの customer managed policy の文書（JSON）。管理アカウントの外では null。"
  value       = one(aws_iam_policy.governance_read[*].policy)
}

output "oidc_provider_arn" {
  description = "GitHub Actions の OIDC provider の ARN。"
  value       = aws_iam_openid_connect_provider.github.arn
}

output "permissions_boundary_arns" {
  description = "IAM の role を作る workload root の usecase ごとの permissions boundary の ARN（usecase の名前を key にした map）。"
  # 名前から組み、policy の作成を待つ。
  value = { for k, p in aws_iam_policy.boundary : k => local.boundary_policy_arns[k] if p.name == local.boundary_policy_names[k] }
}
