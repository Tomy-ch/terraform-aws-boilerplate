output "parameter_arn" {
  description = "メンバーアカウントの env から ID への map を持つ parameter の ARN。メンバーの root がこの ARN で読む。"
  value       = aws_ssm_parameter.this.arn
}
