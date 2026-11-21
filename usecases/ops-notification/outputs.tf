output "topic_arn" {
  description = "通知の Topic の ARN。同じアカウントの送り手（CloudWatch の alarm、EventBridge の rule、Budgets、Cost Anomaly Detection）の通知先にする。"
  value       = module.sns.topic_arn
}

output "us_east_1_topic_arn" {
  description = "us-east-1 の通知の Topic の ARN。us-east-1 にしか出ない metric（Route 53 の DNSSEC、CloudFront など）の CloudWatch の alarm の通知先にする。送り手は CloudWatch だけ。"
  value       = module.us_east_1_topic.topic_arn
}
