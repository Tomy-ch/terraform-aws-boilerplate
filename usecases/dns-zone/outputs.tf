output "zone_id" {
  description = "Hosted Zone の ID。record を書く usecase が受ける（`dns = { zone_id, name }`）。"
  value       = module.zone.zone_id
}

output "zone_arn" {
  description = "Hosted Zone の ARN。record を書く role の権限の Resource に使う。"
  value       = module.zone.zone_arn
}

output "zone_name" {
  description = "Hosted Zone の名前（末尾のドットを持たない）。record を書く usecase は、この下の名前だけを書く。"
  value       = local.zone_name

  # 委任は直近の親からだけ行う（Route 53 Developer Guide「Best practices for Amazon Route 53 DNS」）。apex の zone（prd）だけが子を持つ。
  precondition {
    condition     = length(var.delegations) == 0 || var.env == "prd"
    error_message = "delegations を渡せるのは env が prd のときだけです。"
  }
}

output "name_servers" {
  description = "Hosted Zone の name server。dev・stg では prd の delegations へ、prd ではドメインの registrar へ写す。"
  value       = module.zone.name_servers
}

output "ds_record" {
  description = "KSK の DS record。dev・stg では prd の delegations へ、prd ではドメインの registrar へ写す。"
  value       = module.zone.ds_record
}

output "query_log_group_arn" {
  description = "公開 DNS のクエリログの Log Group の ARN（ログの所在。us-east-1。末尾の :* を持たない）。"
  value       = module.query_log.log_group_arn
}
