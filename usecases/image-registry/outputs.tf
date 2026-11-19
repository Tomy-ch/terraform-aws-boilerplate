output "repositories" {
  description = "論理名ごとの repository の ARN と URL。URL は発火側の push の宛先と Compute のユースケースの image の入力に、ARN は pull の権限の Resource に使う。"
  value       = { for k, m in module.repository : k => m.repository }
}

output "publisher_role_arns" {
  description = "論理名ごとの、発火側が assume する push の role の ARN。"
  value       = { for k, m in module.repository : k => m.publisher_role_arn }
}
