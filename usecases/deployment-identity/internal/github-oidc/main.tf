# kics-scan disable=e592a0c5-5bdb-414c-9066-5dba7cdea370,e38a8e0a-b88b-4902-b3fe-b0fcb17d5c10
# e592a0c5（IAM Access Analyzer）: アカウント単位の統制で account-baseline が持つ（docs/v1.0-plan/scope.md 第7節）。撤回: account-baseline を実装したとき。
# e38a8e0a（tag）: tag は共通の単一入力でだけ付ける（ADR-0204 決定5）。この module はその入力を持たず、tag の書き込みの権限も持たない（deployment-identity/ADR-0201）。撤回: 共通の tag 入力を配線したとき。

data "aws_caller_identity" "current" {}
data "aws_region" "current" {}
data "aws_partition" "current" {}

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
    plan            = { name = "${var.name_prefix}-ci-plan", environment = "${var.env}-plan" }
    apply           = { name = "${var.name_prefix}-ci-apply", environment = "${var.env}-apply" }
    apply_identity  = { name = "${var.name_prefix}-ci-apply-identity", environment = "${var.env}-apply-identity" }
    unlock_identity = { name = "${var.name_prefix}-ci-unlock-identity", environment = "${var.env}-unlock-identity" }
  }

  # apply-identity は他の role を管理する権限を持つので、resource を分けて先に書き込む（deployment-identity/ADR-0201 決定4）。
  ci_roles = { for k, r in local.roles : k => r if k != "apply_identity" }

  # role の作成を待たずに決まる ARN。apply-identity の policy が、まだ無い role を管理する権限を先に持てる。
  role_arns = { for k, r in local.roles : k => "arn:${data.aws_partition.current.partition}:iam::${data.aws_caller_identity.current.account_id}:role/${r.name}" }

  trust_policies = { for k, r in local.roles : k => jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect    = "Allow"
        Principal = { Federated = aws_iam_openid_connect_provider.github.arn }
        Action    = "sts:AssumeRoleWithWebIdentity"
        Condition = {
          StringEquals = {
            "${local.issuer}:aud"                 = "sts.amazonaws.com"
            "${local.issuer}:sub"                 = "${local.sub_prefix}:environment:${r.environment}"
            "${local.issuer}:repository_id"       = var.github.repository_id
            "${local.issuer}:repository_owner_id" = var.github.owner_id
          }
        }
      },
    ]
  }) }

  all_roles    = merge(aws_iam_role.this, { apply_identity = aws_iam_role.apply_identity })
  all_policies = merge(aws_iam_role_policy.this, { apply_identity = aws_iam_role_policy.apply_identity })
}

resource "aws_iam_role" "apply_identity" {
  name                 = local.roles.apply_identity.name
  max_session_duration = 3600
  assume_role_policy   = local.trust_policies.apply_identity
}

resource "aws_iam_role" "this" {
  for_each = local.ci_roles

  name                 = each.value.name
  max_session_duration = 3600
  assume_role_policy   = local.trust_policies[each.key]

  # 同じ apply で role を足すとき、apply-identity が CreateRole の権限を得てから作る。
  depends_on = [aws_iam_role_policy.apply_identity]
}

moved {
  from = aws_iam_role.this["apply_identity"]
  to   = aws_iam_role.apply_identity
}

resource "aws_iam_role_policies_exclusive" "this" {
  for_each = local.roles

  role_name    = local.all_roles[each.key].name
  policy_names = [local.all_policies[each.key].name]
}

resource "aws_iam_role_policy_attachments_exclusive" "this" {
  for_each = local.roles

  role_name   = local.all_roles[each.key].name
  policy_arns = []
}
