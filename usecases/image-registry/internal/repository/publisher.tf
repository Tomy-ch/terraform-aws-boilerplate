# kics-scan disable=e592a0c5-5bdb-414c-9066-5dba7cdea370,e38a8e0a-b88b-4902-b3fe-b0fcb17d5c10
# e592a0c5（IAM Access Analyzer）: KICS はファイルごとに analyzer の宣言を求める。analyzer はアカウント単位の統制で account-baseline が持つ（docs/v1.0-plan/scope.md 第7節）。撤回: KICS がファイルの外の宣言を数えるようになったとき。
# e38a8e0a（tag）: tag は共通の単一入力でだけ付ける（ADR-0204 決定5）。この module はその入力を持たない。撤回: 共通の tag 入力を配線したとき。

locals {
  issuer = "token.actions.githubusercontent.com"

  # immutable 形式の sub（ADR-0602 決定14・19）。
  publisher_sub = "repo:${var.publisher.owner}@${var.publisher.owner_id}/${var.publisher.repository}@${var.publisher.repository_id}:environment:${var.publisher.environment}"

  publisher_trust = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Federated = var.github_oidc_provider_arn }
      Action    = "sts:AssumeRoleWithWebIdentity"
      Condition = {
        StringEquals = {
          "${local.issuer}:aud"                 = "sts.amazonaws.com"
          "${local.issuer}:sub"                 = local.publisher_sub
          "${local.issuer}:repository_id"       = var.publisher.repository_id
          "${local.issuer}:repository_owner_id" = var.publisher.owner_id
        }
      }
    }]
  })

  # push の語彙（image-registry/ADR-0102）。GetAuthorizationToken は resource-level の権限を持たない（ADR-0301 決定9）。
  publisher_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "Publish"
        Effect = "Allow"
        Action = [
          "ecr:BatchCheckLayerAvailability",
          "ecr:BatchGetImage",
          "ecr:CompleteLayerUpload",
          "ecr:DescribeImageScanFindings",
          "ecr:DescribeImages",
          "ecr:InitiateLayerUpload",
          "ecr:PutImage",
          "ecr:UploadLayerPart",
        ]
        Resource = local.repository_arn
      },
      {
        Sid      = "RegistryLogin"
        Effect   = "Allow"
        Action   = "ecr:GetAuthorizationToken"
        Resource = "*"
      },
    ]
  })
}

resource "aws_iam_role" "publisher" {
  #checkov:skip=CKV_AWS_393:Checkov は immutable 形式の sub（repo:<owner>@<id>/<repo>@<id>）を repo の形と認めない。同じ不変条件は conftest/plan の PLAN-PUBTRUST-06・08 が見る。撤回: Checkov がその形を認めたとき
  name                 = var.publisher_role_name
  max_session_duration = 3600
  assume_role_policy   = local.publisher_trust
}

resource "aws_iam_role_policy" "publisher" {
  name   = "publish"
  role   = aws_iam_role.publisher.name
  policy = local.publisher_policy
}

resource "aws_iam_role_policies_exclusive" "publisher" {
  role_name    = aws_iam_role.publisher.name
  policy_names = [aws_iam_role_policy.publisher.name]
}

resource "aws_iam_role_policy_attachments_exclusive" "publisher" {
  role_name   = aws_iam_role.publisher.name
  policy_arns = []
}
