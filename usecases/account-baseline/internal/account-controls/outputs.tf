output "password_policy_count" {
  description = "置いた IAM の password policy の数（0 か 1）。usecase の Contract Test が、env ごとの合成を確かめるのに使う。"
  value       = length(aws_iam_account_password_policy.this)
}
