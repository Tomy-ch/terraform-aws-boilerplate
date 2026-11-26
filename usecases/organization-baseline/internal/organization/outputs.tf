output "root_id" {
  description = "組織の Root の ID。メンバーの break-glass の role を配る StackSet の展開先。"
  value       = local.root_id
}

output "account_ids" {
  description = "メンバーアカウントの env から ID への map。"
  value       = { for k, a in aws_organizations_account.this : k => a.id }
}

output "bootstrapping_account_ids" {
  description = "立ち上げ中のメンバーアカウントの env から ID への map。"
  value       = { for k, a in aws_organizations_account.this : k => a.id if contains(var.bootstrapping, k) }
}
