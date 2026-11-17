variable "name" {
  description = "Log Group と delivery destination の名前。鍵の Key Policy（modules/kms の users.services.logs）へ同じ値を渡す。"
  type        = string
  nullable    = false

  # delivery destination の名前の制約（PutDeliveryDestination）が Log Group の名前の制約より狭い。
  validation {
    condition     = can(regex("^[A-Za-z0-9_-]{1,60}$", var.name))
    error_message = "name は英数字と - _ だけで、1-60 文字にしてください。"
  }
}

variable "retention_days" {
  description = "ログを残す日数。365 以上の、CloudWatch Logs が受ける値に限る。"
  type        = number
  nullable    = false

  validation {
    condition     = contains([365, 400, 545, 731, 1096, 1827, 2192, 2557, 2922, 3288, 3653], var.retention_days)
    error_message = "retention_days は 365, 400, 545, 731, 1096, 1827, 2192, 2557, 2922, 3288, 3653 のいずれかにしてください。"
  }
}

variable "kms_key_arn" {
  description = "ログの暗号化に使う KMS の鍵の ARN。Log Group と同じ partition・リージョン・アカウントの鍵にする。"
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^arn:aws[a-z-]*:kms:[a-z0-9-]+:[0-9]{12}:key/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$", var.kms_key_arn))
    error_message = "kms_key_arn は KMS の鍵の ARN（arn:<partition>:kms:<region>:<account>:key/<key-id>）にしてください。alias の ARN と多リージョンの鍵は受けません。"
  }
}

variable "deletion_protection" {
  description = "Log Group の削除を拒むか。"
  type        = bool
  nullable    = false
}

variable "delivery_sources" {
  description = "この Log Group へ配送してよい delivery source の名前。送り側の module が置く delivery source と同じ値を渡す。"
  type        = list(string)
  nullable    = false

  validation {
    condition     = length(var.delivery_sources) > 0
    error_message = "delivery_sources に1件以上の名前を渡してください。"
  }

  validation {
    condition     = alltrue([for s in var.delivery_sources : can(regex("^[A-Za-z0-9_-]{1,60}$", s))])
    error_message = "delivery_sources の名前は英数字と - _ だけで、1-60 文字にしてください。"
  }

  validation {
    condition     = length(distinct(var.delivery_sources)) == length(var.delivery_sources)
    error_message = "delivery_sources に同じ名前を重ねないでください。"
  }
}
