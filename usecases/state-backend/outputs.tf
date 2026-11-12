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
  description = "state の鍵の alias の名前（`alias/<project>-<env>-tfstate`）。入力だけから決まり、鍵より前に参照できる。bootstrap の手順が鍵を名指す名前である。手順と identity root のテストがそれぞれ同じ値を直書きし、テストはこの出力をその直書きで固定する。手順とテストの直書きの一致は検査しない。"
  value       = module.kms.alias_name
}

output "plan_output_bucket_name" {
  description = "CI の plan の出力を置く Bucket の完全な名前。CI の plan の job が、出力の保存先に使う。"
  value       = module.plan_output.bucket_name
}

output "plan_output_bucket_arn" {
  description = "CI の plan の出力を置く Bucket の ARN。CI の role の IAM で、plan の出力の書き込みと Bucket の設定の Resource を絞るのに使う。"
  value       = module.plan_output.bucket_arn
}
