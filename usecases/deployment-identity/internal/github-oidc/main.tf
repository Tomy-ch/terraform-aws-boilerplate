# thumbprint_list は書かない（deployment-identity/ADR-0101 決定4）。
resource "aws_iam_openid_connect_provider" "github" {
  url            = "https://token.actions.githubusercontent.com"
  client_id_list = ["sts.amazonaws.com"]
}

locals {
  issuer = "token.actions.githubusercontent.com"

  # immutable 形式の sub（deployment-identity/ADR-0101 決定2・7）。
  sub_prefix = "repo:${var.github.owner}@${var.github.owner_id}/${var.github.repository}@${var.github.repository_id}"

  roles = {
    plan           = { name = "${var.name_prefix}-ci-plan", environment = "${var.env}-plan" }
    apply          = { name = "${var.name_prefix}-ci-apply", environment = "${var.env}-apply" }
    apply_identity = { name = "${var.name_prefix}-ci-apply-identity", environment = "${var.env}-apply-identity" }
  }
}

resource "aws_iam_role" "this" {
  for_each = local.roles

  name                 = each.value.name
  max_session_duration = 3600

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect    = "Allow"
        Principal = { Federated = aws_iam_openid_connect_provider.github.arn }
        Action    = "sts:AssumeRoleWithWebIdentity"
        Condition = {
          StringEquals = {
            "${local.issuer}:aud"                 = "sts.amazonaws.com"
            "${local.issuer}:sub"                 = "${local.sub_prefix}:environment:${each.value.environment}"
            "${local.issuer}:repository_id"       = var.github.repository_id
            "${local.issuer}:repository_owner_id" = var.github.owner_id
          }
        }
      },
    ]
  })
}

resource "aws_iam_role_policies_exclusive" "this" {
  for_each = local.roles

  role_name    = aws_iam_role.this[each.key].name
  policy_names = [aws_iam_role_policy.this[each.key].name]
}

resource "aws_iam_role_policy_attachments_exclusive" "this" {
  for_each = local.roles

  role_name   = aws_iam_role.this[each.key].name
  policy_arns = []
}
