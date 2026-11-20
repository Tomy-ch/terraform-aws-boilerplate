output "zone_id" {
  description = "Hosted Zone の ID。"
  value       = aws_route53_zone.this.zone_id
}

output "zone_arn" {
  description = "Hosted Zone の ARN。"
  value       = aws_route53_zone.this.arn
}

output "name_servers" {
  description = "Hosted Zone の name server。"
  value       = aws_route53_zone.this.name_servers
}

output "ds_record" {
  description = "KSK の DS record（`<key tag> 13 2 <digest>`）。"
  value       = aws_route53_key_signing_key.this.ds_record
}
