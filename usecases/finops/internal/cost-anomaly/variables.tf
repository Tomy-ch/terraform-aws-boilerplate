variable "name_prefix" {
  description = "monitor と subscription の名前の前半（`<project>-<env>`）。"
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^[a-z0-9]([a-z0-9-]*[a-z0-9])?$", var.name_prefix))
    error_message = "name_prefix は英小文字・数字・ハイフンだけで、先頭と末尾は英小文字か数字にしてください。"
  }
}

variable "scope" {
  description = "monitor が費用を分ける単位。`organization` は組織のメンバーアカウントごと（管理アカウントでしか作れない）、`account` はこのアカウントの AWS のサービスごと。"
  type        = string
  nullable    = false

  validation {
    condition     = contains(["organization", "account"], var.scope)
    error_message = "scope は organization か account にしてください。"
  }
}

variable "notification_topic_arn" {
  description = "異常の通知を送る SNS の Topic の ARN。subscription と同じ partition・アカウントの Topic にする。"
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^arn:aws[a-z-]*:sns:[a-z0-9-]+:[0-9]{12}:[A-Za-z0-9_-]{1,256}$", var.notification_topic_arn))
    error_message = "notification_topic_arn は SNS の Topic の ARN（arn:<partition>:sns:<region>:<account>:<name>）にしてください。FIFO の Topic は受けません。"
  }
}

variable "threshold_usd" {
  description = "通知する異常の影響額（USD）の下限。影響額がこの額以上で、かつ期待値に対して40%以上の異常を通知する。"
  type        = number
  nullable    = false

  validation {
    condition     = var.threshold_usd > 0 && var.threshold_usd <= 10000000000
    error_message = "threshold_usd は0より大きく、10000000000 以下にしてください。"
  }
}
