output "private_subnet_ids" {
  description = "private の tier の subnet の ID。availability_zone_ids の順。"
  value       = [for az in var.availability_zone_ids : aws_subnet.this["private-${az}"].id]
}

output "isolated_subnet_ids" {
  description = "isolated の tier の subnet の ID。availability_zone_ids の順。"
  value       = [for az in var.availability_zone_ids : aws_subnet.this["isolated-${az}"].id]
}

output "private_route_table_ids" {
  description = "private の tier の Route Table の ID。availability_zone_ids の順。"
  value       = [for az in var.availability_zone_ids : aws_route_table.this["private-${az}"].id]
}
