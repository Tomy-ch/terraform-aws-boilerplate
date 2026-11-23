output "recorders" {
  description = "リージョンごとの recorder の、グローバルな resource を記録するか。usecase の Contract Test が、home のリージョンだけが記録することを確かめるのに使う。"
  value       = { for k, r in aws_config_configuration_recorder.this : k => r.recording_group[0].include_global_resource_types }
}

output "delivery" {
  description = "delivery channel の配信先（どのリージョンも同じ）。usecase の Contract Test が、公開入力の届き先を確かめるのに使う。"
  value       = distinct([for c in aws_config_delivery_channel.this : { bucket_name = c.s3_bucket_name, kms_key_arn = c.s3_kms_key_arn }])
}
