variable "name" {
  description = "channel role・Chatbot の構成の名前（`<project>-<env>-ops-chat`）。読み取りの policy は `<name>-read`。"
  type        = string
  nullable    = false

  # role 名の上限（64文字）と、Chatbot の構成の名前が許す文字（API の ConfigurationName）。
  validation {
    condition     = can(regex("^[A-Za-z0-9_-]{1,64}$", var.name))
    error_message = "name は英数字・_・- だけの 1-64 文字にしてください。"
  }
}

variable "slack" {
  description = "通知を届ける Slack の workspace（Amazon Q Developer in chat applications で認可済み）の ID と channel の ID。"
  type = object({
    team_id    = string
    channel_id = string
  })
  nullable = false

  # Amazon Q Developer in chat applications API Reference の CreateSlackChannelConfiguration の SlackTeamId・SlackChannelId。
  validation {
    condition     = can(regex("^[0-9A-Z]{1,255}$", var.slack.team_id)) && can(regex("^[A-Za-z0-9]{1,255}$", var.slack.channel_id))
    error_message = "slack.team_id は英大文字と数字、slack.channel_id は英数字の ID（名前ではない）にしてください。"
  }
}

variable "topic_arn" {
  description = "購読する Topic の ARN。この module と同じ partition・アカウントの Topic にする。"
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^arn:aws[a-z-]*:sns:[a-z0-9-]+:[0-9]{12}:[A-Za-z0-9_-]{1,256}$", var.topic_arn))
    error_message = "topic_arn は標準の SNS の Topic の ARN（arn:<partition>:sns:<region>:<account>:<name>）にしてください。FIFO の Topic は受けません。"
  }
}

variable "us_east_1_topic_arn" {
  description = "購読する us-east-1 の Topic の ARN。この module と同じ partition・アカウントの Topic にする。"
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^arn:aws[a-z-]*:sns:us-east-1:[0-9]{12}:[A-Za-z0-9_-]{1,256}$", var.us_east_1_topic_arn))
    error_message = "us_east_1_topic_arn は us-east-1 の標準の SNS の Topic の ARN（arn:<partition>:sns:us-east-1:<account>:<name>）にしてください。"
  }
}

variable "permissions_boundary_arn" {
  description = "channel role に付ける permissions boundary の customer managed policy の ARN。この module と同じアカウントの policy にする。"
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^arn:aws[a-z-]*:iam::[0-9]{12}:policy/[A-Za-z0-9+=,.@_/-]+$", var.permissions_boundary_arn))
    error_message = "permissions_boundary_arn は customer managed policy の ARN（arn:<partition>:iam::<account>:policy/<name>）にしてください。AWS managed policy は受けません。"
  }
}
