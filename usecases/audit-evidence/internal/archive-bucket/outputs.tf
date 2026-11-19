output "bucket_name" {
  description = "Bucket の名前。Bucket Policy の後に決まり、配信を設定する消費者が Bucket Policy の前に書き始めない。"
  value       = aws_s3_bucket_policy.this.bucket
}

output "bucket_arn" {
  description = "Bucket の ARN。"
  value       = aws_s3_bucket.this.arn
}

output "retention" {
  description = "Object Lock の既定の保持（mode と days）。"
  value = {
    mode = one(one(aws_s3_bucket_object_lock_configuration.this.rule).default_retention).mode
    days = one(one(aws_s3_bucket_object_lock_configuration.this.rule).default_retention).days
  }
}

output "force_destroy" {
  description = "destroy のとき保持中の版を含む object を消すか。"
  value       = aws_s3_bucket.this.force_destroy
}
