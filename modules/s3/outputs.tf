output "bucket_name" {
  description = "Bucket の完全な名前。"
  value       = aws_s3_bucket.this.bucket
}

output "bucket_arn" {
  description = "Bucket の ARN。"
  value       = aws_s3_bucket.this.arn
}
