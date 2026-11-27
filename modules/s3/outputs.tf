output "bucket_name" {
  description = "Bucket の完全な名前。"
  value       = local.bucket_name
}

output "bucket_arn" {
  description = "Bucket の ARN。Bucket を作る plan でも決まる。"
  # computed の arn を読まず名前から組む。Bucket を作る plan でも、この ARN を読む Key Policy の文書が決まる（PLAN-KMS-09）。
  # aws_s3_bucket.this を読むのは Bucket の作成を待つ依存のためである。
  value = aws_s3_bucket.this.bucket == local.bucket_name ? local.bucket_arn : null
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
