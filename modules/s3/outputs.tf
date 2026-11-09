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
