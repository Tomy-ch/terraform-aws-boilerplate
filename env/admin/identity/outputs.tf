output "plan_output_bucket_name" {
  description = "CI の plan の出力を置く Bucket の名前。CI の plan の job が init の後に読み、出力の保存先にする。"
  value       = module.state_backend.plan_output_bucket_name
}
