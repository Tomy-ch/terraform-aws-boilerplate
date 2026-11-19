output "topic_arn" {
  description = "通知の Topic の ARN。同じアカウントの送り手（CloudWatch の alarm、EventBridge の rule、Budgets、Cost Anomaly Detection）の通知先にする。"
  value       = module.sns.topic_arn
}
