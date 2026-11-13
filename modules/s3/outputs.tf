output "bucket_name" {
  description = "Bucket の完全な名前。"
  value       = local.bucket_name
}

output "bucket_arn" {
  description = "Bucket の ARN。"
  value       = aws_s3_bucket.this.arn
}

output "noncurrent_version_retention_days" {
  description = "lifecycle が上書きされた版を消す日数。"
  value       = one(aws_s3_bucket_lifecycle_configuration.this.rule[0].noncurrent_version_expiration).noncurrent_days
}

output "kms_key_arn" {
  description = "既定の暗号化が使う KMS の鍵の ARN。"
  value       = one(one(aws_s3_bucket_server_side_encryption_configuration.this.rule).apply_server_side_encryption_by_default).kms_master_key_id
}

output "object_writers" {
  description = "Bucket Policy が object の書き込みを許す主体。key は object_writers の key と、bootstrap_executor が在るときの bootstrapExecutor。値は主体の ARN、書ける key、access。"
  value       = local.object_writers
}
