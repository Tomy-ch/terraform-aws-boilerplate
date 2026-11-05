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
