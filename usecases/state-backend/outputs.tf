output "bucket_name" {
  description = "state Bucket の完全な名前。backend の `bucket` と、bootstrap の段2 の `import` block の id に使う。"
  value       = module.s3.bucket_name
}

output "bucket_arn" {
  description = "state Bucket の ARN。CI の role の IAM で、S3 の Action の Resource を state Bucket に絞るのに使う。"
  value       = module.s3.bucket_arn
}

output "plan_output_bucket_name" {
  description = "CI の plan の出力を置く Bucket の完全な名前。CI の plan の job が、出力の保存先に使う。"
  value       = module.plan_output.bucket_name
}

output "plan_output_bucket_arn" {
  description = "CI の plan の出力を置く Bucket の ARN。CI の role の IAM で、plan の出力の書き込みと Bucket の設定の Resource を絞るのに使う。"
  value       = module.plan_output.bucket_arn
}
