variable "name_prefix" {
  description = "予算の名前の前半（`<project>-<env>`）。module が `-monthly-cost` を付けて予算の名前にする。"
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^[a-z0-9]([a-z0-9-]*[a-z0-9])?$", var.name_prefix)) && length(var.name_prefix) <= 87
    error_message = "name_prefix は英小文字・数字・ハイフンの87文字以内で、先頭と末尾は英小文字か数字にしてください。"
  }
}

variable "notification_topic_arn" {
  description = "予算の通知を送る SNS の Topic の ARN。予算と同じ partition・アカウントの Topic にする。"
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^arn:aws[a-z-]*:sns:[a-z0-9-]+:[0-9]{12}:[A-Za-z0-9_-]{1,256}$", var.notification_topic_arn))
    error_message = "notification_topic_arn は SNS の Topic の ARN（arn:<partition>:sns:<region>:<account>:<name>）にしてください。FIFO の Topic は受けません。"
  }
}

variable "monthly_amount_usd" {
  description = "月の予算の額（USD）。小数は2桁まで。"
  type        = number
  nullable    = false

  validation {
    condition     = var.monthly_amount_usd > 0 && can(regex("^[0-9]+(\\.[0-9]{1,2})?$", tostring(var.monthly_amount_usd)))
    error_message = "monthly_amount_usd は0より大きく、小数2桁までの数にしてください。"
  }
}
