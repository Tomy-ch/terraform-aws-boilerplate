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
