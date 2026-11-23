output "topic_arn" {
  description = "us-east-1 の Topic の ARN。"
  value       = aws_sns_topic.this.arn

  # 送り手と購読者の設定は、Topic Policy が既定から置き換わった後に作る。
  depends_on = [aws_sns_topic_policy.this]
}

output "key_policy" {
  description = "Key Policy の文書（JSON の文字列）。呼び出し側のテストが文を確かめるための出力。"
  value       = aws_kms_key_policy.this.policy
}
