output "vpc_id" {
  description = "VPC の ID。VPC の中に subnet の外の資源（Security Group、VPC Endpoint）を置く usecase が受ける。"
  value       = module.vpc.vpc_id
}

output "private_subnet_ids" {
  description = "workload を置く private の tier の subnet の ID。availability_zone_ids の順。VPC の外への経路を持たない。"
  value       = module.subnet_tiers.private_subnet_ids
}

output "isolated_subnet_ids" {
  description = "データの層を置く isolated の tier の subnet の ID。availability_zone_ids の順。外部 egress を有効にした環境でも、VPC の外への経路を持たない。"
  value       = module.subnet_tiers.isolated_subnet_ids
}

output "private_route_table_ids" {
  description = "private の tier の Route Table の ID。availability_zone_ids の順。gateway 型の VPC Endpoint の関連付けに使う。"
  value       = module.subnet_tiers.private_route_table_ids
}

output "flow_logs_log_group_arn" {
  description = "VPC Flow Logs の Log Group の ARN（ログの所在）。末尾の :* を持たない。"
  value       = module.flow_logs.log_group_arn
}
