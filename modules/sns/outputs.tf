output "topic_arn" {
  description = "Topic の ARN。"
  value       = aws_sns_topic.this.arn

  # 送り手と購読者の設定は、Topic Policy が既定から置き換わった後に作る。
  depends_on = [aws_sns_topic_policy.this]
}
