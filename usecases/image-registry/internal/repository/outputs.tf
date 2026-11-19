output "repository" {
  description = "repository の ARN と URL。"
  value = {
    arn = local.repository_arn
    url = aws_ecr_repository.this.repository_url
  }
}

output "publisher_role_arn" {
  description = "発火側が assume する push の role の ARN。"
  # computed の arn を読まず名前から組み、plan で決まる値にする。role の作成は依存で待つ。
  value      = local.publisher_arn
  depends_on = [aws_iam_role.publisher, aws_iam_role_policy.publisher]
}
