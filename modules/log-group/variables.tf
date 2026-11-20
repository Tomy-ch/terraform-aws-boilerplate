variable "name" {
  description = "Log Group の名前。鍵の Key Policy（modules/kms の users.services.logs）へ同じ値を渡す。"
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^[.\\-_/#A-Za-z0-9]{1,512}$", var.name))
    error_message = "name は英数字と . - _ / # だけで、1-512 文字にしてください。"
  }

  validation {
    condition     = !startswith(var.name, "aws/")
    error_message = "name を aws/ で始めないでください（AWS が予約しています）。"
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
