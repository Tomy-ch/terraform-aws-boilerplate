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

  # Amazon Q Developer in chat applications の custom notification の形（ops-notification/ADR-0102）。
  message = jsonencode({
    version = "1.0"
    source  = "custom"
    content = {
      textType    = "client-markdown"
      title       = "ops-notification の心拍（警報ではありません）"
      description = "通知経路が働いていることを示す毎日の心拍です。対応は要りません。\nenv: ${var.env} / region: ${local.region} / 予定時刻: <aws.scheduler.scheduled-time>"
    }
  })
}

# 信頼の aws:SourceArn は schedule group の ARN だけを受ける。default の group は同じアカウントの別の schedule と共有になる。
resource "aws_scheduler_schedule_group" "this" {
  name = var.name
}

resource "aws_iam_role" "this" {
  name                 = var.name
  max_session_duration = 3600
  permissions_boundary = var.permissions_boundary_arn
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "scheduler.amazonaws.com" }
      Action    = "sts:AssumeRole"
      Condition = { StringEquals = {
        "aws:SourceAccount" = local.account
        "aws:SourceArn"     = aws_scheduler_schedule_group.this.arn
      } }
    }]
  })

  lifecycle {
    precondition {
      condition     = startswith(var.permissions_boundary_arn, "arn:${local.partition}:iam::${local.account}:policy/")
      error_message = "permissions_boundary_arn が、この module と同じアカウントと partition の policy ではありません。"
    }
  }
}

# 鍵の暗号操作は IAM で与えず、Topic の鍵の Key Policy が名指す（ADR-0301 決定12）。
resource "aws_iam_role_policy" "this" {
  name = "publish"
  role = aws_iam_role.this.name
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = "sns:Publish"
      Resource = var.topic_arn
    }]
  })

  lifecycle {
    precondition {
      condition     = startswith(var.topic_arn, "arn:${local.partition}:sns:${local.region}:${local.account}:")
      error_message = "topic_arn が、この module と同じ partition・リージョン・アカウントの Topic ではありません。"
    }
  }
}

resource "aws_iam_role_policies_exclusive" "this" {
  role_name    = aws_iam_role.this.name
  policy_names = [aws_iam_role_policy.this.name]
}

resource "aws_iam_role_policy_attachments_exclusive" "this" {
  role_name   = aws_iam_role.this.name
  policy_arns = []
}

resource "aws_scheduler_schedule" "this" {
  #checkov:skip=CKV_AWS_297:本文は機微な値を持たない固定の文で、CMK は守りを足さない（usecases/ops-notification/docs/adr/0102-daily-heartbeat.md 決定7）。撤回: 本文が機微な値を持つとき
  name                         = var.name
  group_name                   = aws_scheduler_schedule_group.this.name
  schedule_expression          = "cron(0 9 * * ? *)"
  schedule_expression_timezone = "Asia/Tokyo"
  state                        = "ENABLED"
  action_after_completion      = "NONE"

  flexible_time_window {
    mode = "OFF"
  }

  target {
    arn      = var.topic_arn
    role_arn = aws_iam_role.this.arn
    input    = local.message

    # retry の上限は 1 日未満に収める（ops-notification/ADR-0102 決定3）。
    retry_policy {
      maximum_event_age_in_seconds = 3600
      maximum_retry_attempts       = 185
    }
  }

  # 権限が付く前に起動して拒まれないよう、inline policy の後に作る。
  depends_on = [aws_iam_role_policy.this]
}
