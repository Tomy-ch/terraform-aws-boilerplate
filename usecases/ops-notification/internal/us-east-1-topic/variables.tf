variable "name" {
  description = "Topic と鍵の alias の名前（`<project>-<env>-ops-notification-us-east-1`）。"
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^[A-Za-z0-9_-]{1,256}$", var.name))
    error_message = "name は英数字・_・- だけの 1-256 文字にしてください。"
  }
}

variable "heartbeat_role_arn" {
  description = "Topic へ publish する心拍の role の ARN。鍵の Key Policy が SNS 経由の暗号操作を許す。この module と同じアカウントの role にする。"
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^arn:aws[a-z-]*:iam::[0-9]{12}:role/[A-Za-z0-9+=,.@_/-]+$", var.heartbeat_role_arn))
    error_message = "heartbeat_role_arn は role の ARN（arn:<partition>:iam::<account>:role/<name>）にしてください。"
  }
}
