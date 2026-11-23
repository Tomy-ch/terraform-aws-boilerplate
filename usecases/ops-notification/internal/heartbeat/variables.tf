variable "name" {
  description = "role・schedule group・schedule の名前（`<project>-<env>-ops-heartbeat`）。"
  type        = string
  nullable    = false

  # role 名と schedule group 名の上限（64文字）と、両者が共通に許す文字。
  validation {
    condition     = can(regex("^[A-Za-z0-9_.-]{1,64}$", var.name))
    error_message = "name は英数字・_・.・- だけの 1-64 文字にしてください。"
  }
}

variable "env" {
  description = "心拍の本文に載せる env の token。"
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^[a-z0-9-]+$", var.env))
    error_message = "env は英小文字・数字・- だけにしてください。"
  }
}

variable "us_east_1_name" {
  description = "us-east-1 の schedule group と schedule の名前（`<project>-<env>-ops-us-east-1`）。"
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^[A-Za-z0-9_.-]{1,64}$", var.us_east_1_name))
    error_message = "us_east_1_name は英数字・_・.・- だけの 1-64 文字にしてください。"
  }
}

variable "topic_arn" {
  description = "心拍を publish する Topic の ARN。この module と同じ partition・リージョン・アカウントの Topic にする。"
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^arn:aws[a-z-]*:sns:[a-z0-9-]+:[0-9]{12}:[A-Za-z0-9_-]{1,256}$", var.topic_arn))
    error_message = "topic_arn は標準の SNS の Topic の ARN（arn:<partition>:sns:<region>:<account>:<name>）にしてください。FIFO の Topic は受けません。"
  }
}

variable "us_east_1_topic_arn" {
  description = "心拍を publish する us-east-1 の Topic の ARN。この module と同じ partition・アカウントの Topic にする。"
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^arn:aws[a-z-]*:sns:us-east-1:[0-9]{12}:[A-Za-z0-9_-]{1,256}$", var.us_east_1_topic_arn))
    error_message = "us_east_1_topic_arn は us-east-1 の標準の SNS の Topic の ARN（arn:<partition>:sns:us-east-1:<account>:<name>）にしてください。"
  }
}

variable "permissions_boundary_arn" {
  description = "role に付ける permissions boundary の customer managed policy の ARN。この module と同じアカウントの policy にする。"
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^arn:aws[a-z-]*:iam::[0-9]{12}:policy/[A-Za-z0-9+=,.@_/-]+$", var.permissions_boundary_arn))
    error_message = "permissions_boundary_arn は customer managed policy の ARN（arn:<partition>:iam::<account>:policy/<name>）にしてください。AWS managed policy は受けません。"
  }
}
