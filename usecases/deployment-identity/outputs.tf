output "plan_role_arn" {
  description = "plan の role の ARN。GitHub Actions の plan の job が assume する。"
  value       = module.github_oidc.role_arns["plan"]
}

output "apply_role_arn" {
  description = "apply の role の ARN。workload root を apply する job が assume する。"
  value       = module.github_oidc.role_arns["apply"]
}

output "apply_identity_role_arn" {
  description = "apply-identity の role の ARN。identity root を apply する job が assume する。"
  value       = module.github_oidc.role_arns["apply_identity"]
}

output "unlock_identity_role_arn" {
  description = "unlock-identity の role の ARN。identity root の state に残った lock を外す job が assume する。"
  value       = module.github_oidc.role_arns["unlock_identity"]
}

output "unlock_role_arn" {
  description = "unlock の role の ARN。workload root の state に残った lock を外す job が assume する。"
  value       = module.github_oidc.role_arns["unlock"]
}

output "github_oidc_provider_arn" {
  description = "GitHub Actions の OIDC provider の ARN。このアカウントで、本リポジトリの外の CI を信頼する role（ADR-0602 決定19）が信頼条件の Principal に使う。"
  value       = module.github_oidc.oidc_provider_arn
}
