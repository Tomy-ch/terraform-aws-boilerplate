output "bucket_name" {
  description = "state Bucket の完全な名前。backend の `bucket` と、bootstrap の段2 の `import` block の id に使う。"
  value       = module.s3.bucket_name
}

output "bucket_arn" {
  description = "state Bucket の ARN。CI の role の IAM で、S3 の Action の Resource を state Bucket に絞るのに使う。"
  value       = module.s3.bucket_arn
}

output "state_key_arn" {
  description = "state の鍵の ARN。CI の role の IAM で、鍵の Action と apply の role の鍵の Deny の Resource をこの鍵に絞るのに使う。Key Policy の置き換えを待たないので、鍵を使う resource には渡さない。"
  value       = module.kms.key_arn_for_iam
}

output "state_key_alias_name" {
  description = "state の鍵の alias の名前（`alias/<project>-<env>-tfstate`）。入力だけから決まり、鍵より前に参照できる。bootstrap の手順が鍵を名指す名前である。"
  value       = module.kms.alias_name
}

output "plan_output_bucket_name" {
  description = "CI の plan と apply の出力を置く Bucket の完全な名前。CI の plan の job が、出力の保存先に使う。"
  value       = module.plan_output.bucket_name
}

output "plan_output_bucket_arn" {
  description = "CI の plan と apply の出力を置く Bucket の ARN。CI の role の IAM で、plan と apply の出力の書き込みと Bucket の設定の Resource を絞るのに使う。"
  value       = module.plan_output.bucket_arn
}

output "access_log_group_arn" {
  description = "S3 サーバーアクセスログを受ける Log Group の ARN（API が付ける `:*` を除いた形）。入力と data source だけから決まり、Log Group より前に参照できる。`deployment-identity` の `access_log_group_arn` に渡す。"
  value       = "${local.logs_arn_prefix}:log-group:${local.access_log_name}"
}

output "access_log_destination_arn" {
  description = "S3 サーバーアクセスログの delivery destination の ARN。入力と data source だけから決まる。`deployment-identity` の `access_log_destination_arn` に渡す。"
  value       = "${local.logs_arn_prefix}:delivery-destination:${local.access_log_name}"
}

output "access_log_source_arns" {
  description = "state Bucket と plan の出力の Bucket の delivery source の ARN（この順）。入力と data source だけから決まる。`deployment-identity` の `access_log_source_arns` に渡す。"
  value       = [for n in local.access_log_source_names : "${local.logs_arn_prefix}:delivery-source:${n}"]
}
