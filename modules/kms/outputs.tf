output "key_arn" {
  description = "鍵の ARN。消費者は alias ではなくこの値で鍵を指す。"
  value       = aws_kms_key.this.arn
}
