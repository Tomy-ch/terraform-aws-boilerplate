output "bucket_name" {
  description = "Bucket の完全な名前。"
  value       = local.bucket_name
}

output "bucket_arn" {
  description = "Bucket の ARN。"
  value       = aws_s3_bucket.this.arn
}

output "retention_days" {
  description = "lifecycle が現行版と旧版を失効させる日数。"
  value = {
    current    = one(aws_s3_bucket_lifecycle_configuration.this.rule[0].expiration).days
    noncurrent = one(aws_s3_bucket_lifecycle_configuration.this.rule[0].noncurrent_version_expiration).noncurrent_days
  }
}

output "kms_key_arn" {
  description = "既定の暗号化が使う KMS の鍵の ARN。"
  value       = one(one(aws_s3_bucket_server_side_encryption_configuration.this.rule).apply_server_side_encryption_by_default).kms_master_key_id
}
