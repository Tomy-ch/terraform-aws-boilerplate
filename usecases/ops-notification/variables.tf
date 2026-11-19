variable "project" {
  description = "AWS 資源の名前の先頭に置くプロジェクトの token。Topic は `<project>-<env>-ops-notification` になる。"
  type        = string
  nullable    = false

  # ハイフンを許すと `<project>-<env>` の境目を名前から読めなくなる。
  validation {
    condition     = can(regex("^[a-z0-9]+$", var.project))
    error_message = "project は英小文字と数字だけにしてください。"
  }

  # 最も長い role 名 `<project>-log-archive-ops-heartbeat` を IAM の上限の64文字に収め、deployment-identity と同じ上限にする。
  validation {
    condition     = length(var.project) <= 37
    error_message = "project は37文字以内にしてください。"
  }
}

variable "env" {
  description = "Topic を置くアカウントの env の token。"
  type        = string
  nullable    = false

  validation {
    condition     = contains(["admin", "security", "log-archive", "sandbox", "dev", "stg", "prd"], var.env)
    error_message = "env は admin / security / log-archive / sandbox / dev / stg / prd のいずれかにしてください。"
  }
}

variable "chat" {
  description = "通知を届けるチャットの宛先。slack は、Amazon Q Developer in chat applications で認可済みの Slack の workspace の ID（team_id）と channel の ID（channel_id）。"
  type = object({
    slack = object({
      team_id    = string
      channel_id = string
    })
  })
  nullable = false

  validation {
    condition     = var.chat.slack != null
    error_message = "chat.slack を渡してください。"
  }

  # Amazon Q Developer in chat applications API Reference の CreateSlackChannelConfiguration の SlackTeamId・SlackChannelId。
  validation {
    condition     = var.chat.slack == null || (can(regex("^[0-9A-Z]{1,255}$", var.chat.slack.team_id)) && can(regex("^[A-Za-z0-9]{1,255}$", var.chat.slack.channel_id)))
    error_message = "chat.slack.team_id は英大文字と数字、chat.slack.channel_id は英数字の ID（名前ではない）にしてください。"
  }
}

variable "permissions_boundary_arn" {
  description = "このユースケースが作る IAM role のすべてに付ける permissions boundary の customer managed policy の ARN。同じアカウントの policy にする。"
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^arn:aws[a-z-]*:iam::[0-9]{12}:policy/[A-Za-z0-9+=,.@_/-]+$", var.permissions_boundary_arn))
    error_message = "permissions_boundary_arn は customer managed policy の ARN（arn:<partition>:iam::<account>:policy/<name>）にしてください。AWS managed policy は受けません。"
  }
}
