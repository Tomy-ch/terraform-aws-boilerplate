output "bucket_name" {
  description = "証跡を保管する Bucket の名前。組織の trail の s3_bucket_name と、Config の delivery channel の s3_bucket_name に渡す。Bucket Policy の後に決まる。"
  value       = module.archive.bucket_name
}

output "bucket_arn" {
  description = "証跡を保管する Bucket の ARN。組織の trail の S3 data events の対象から、この Bucket を除くのに使う。"
  value       = module.archive.bucket_arn
}

output "kms_key_arn" {
  description = "証跡を暗号化する鍵の ARN。組織の trail の kms_key_id と、Config の delivery channel の s3_kms_key_arn に渡す。Key Policy の後に決まる。"
  value       = module.archive_key.key_arn
}

output "access_log_group_arn" {
  description = "Bucket の S3 サーバーアクセスログを受ける Log Group の ARN。"
  value       = module.access_log.log_group_arn
}
