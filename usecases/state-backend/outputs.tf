output "bucket_name" {
  description = "state Bucket の完全な名前。backend の `bucket` と、bootstrap の段2 の `import` block の id に使う。"
  value       = module.s3.bucket_name
}

output "bucket_arn" {
  description = "state Bucket の ARN。CI の role の IAM で、S3 の Action の Resource を state Bucket に絞るのに使う。"
  value       = module.s3.bucket_arn
}
