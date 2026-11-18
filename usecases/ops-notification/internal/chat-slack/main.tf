# kics-scan disable=e592a0c5-5bdb-414c-9066-5dba7cdea370,e38a8e0a-b88b-4902-b3fe-b0fcb17d5c10
# e592a0c5（IAM Access Analyzer）: アカウント単位の統制で account-baseline が持つ（docs/v1.0-plan/scope.md 第7節）。撤回: account-baseline を実装したとき。
# e38a8e0a（tag）: tag は共通の単一入力でだけ付ける（ADR-0204 決定5）。この module はその入力を持たない。撤回: 共通の tag 入力を配線したとき。

data "aws_caller_identity" "current" {}
data "aws_region" "current" {}
data "aws_partition" "current" {}

locals {
  account   = data.aws_caller_identity.current.account_id
  region    = data.aws_region.current.region
  partition = data.aws_partition.current.partition
}

# guardrail と channel role に同じ policy を置く（ops-notification/ADR-0201）。guardrail を省くと AdministratorAccess になる。
resource "aws_iam_policy" "read" {
  name   = "${var.name}-read"
  path   = "/"
  policy = local.read_policy
}

resource "aws_iam_role" "this" {
  name                 = var.name
  max_session_duration = 3600
  permissions_boundary = var.permissions_boundary_arn
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "chatbot.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })

  lifecycle {
    precondition {
      condition     = startswith(var.permissions_boundary_arn, "arn:${local.partition}:iam::${local.account}:policy/")
      error_message = "permissions_boundary_arn が、この module と同じアカウントと partition の policy ではありません。"
    }
  }
}

resource "aws_iam_role_policies_exclusive" "this" {
  role_name    = aws_iam_role.this.name
  policy_names = []
}

resource "aws_iam_role_policy_attachments_exclusive" "this" {
  role_name   = aws_iam_role.this.name
  policy_arns = [aws_iam_policy.read.arn]
}

# Chatbot の API は us-east-2・us-west-2・eu-west-1・ap-southeast-1 にだけ在り、Provider はほかの region を us-west-2 へ寄せる（ops-notification/ADR-0201）。
resource "aws_chatbot_slack_channel_configuration" "this" {
  region                      = "us-west-2"
  configuration_name          = var.name
  iam_role_arn                = aws_iam_role.this.arn
  slack_team_id               = var.slack.team_id
  slack_channel_id            = var.slack.channel_id
  sns_topic_arns              = [var.topic_arn]
  guardrail_policy_arns       = [aws_iam_policy.read.arn]
  logging_level               = "NONE"
  user_authorization_required = true

  depends_on = [aws_iam_role_policy_attachments_exclusive.this]

  lifecycle {
    precondition {
      condition     = startswith(var.topic_arn, "arn:${local.partition}:sns:") && split(":", var.topic_arn)[4] == local.account
      error_message = "topic_arn が、この module と同じアカウントと partition の Topic ではありません。"
    }
  }
}
