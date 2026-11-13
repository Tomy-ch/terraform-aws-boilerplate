output "key_arn" {
  description = "鍵の ARN。消費者は alias ではなくこの値で鍵を指す。"
  value       = aws_kms_key.this.arn

  # 鍵を名指す暗号化の設定は、Key Policy が既定から置き換わった後に作る。
  depends_on = [aws_kms_key_policy.this]
}

output "alias_name" {
  description = "alias の名前（`alias/<name>`）。入力だけから決まり、鍵より前に参照できる。"
  value       = local.alias_name
}

output "key_arn_for_iam" {
  description = "鍵の ARN。key_arn と同じ値だが、Key Policy の置き換えを待たない。鍵を使わず ARN だけを名指す場所（IAM の policy の Resource）にだけ使う。鍵を使う resource に渡すと、既定の Key Policy の間に鍵を使いうる。"
  value       = aws_kms_key.this.arn
}
