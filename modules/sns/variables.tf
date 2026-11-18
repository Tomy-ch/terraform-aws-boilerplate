variable "name" {
  description = "Topic の名前。鍵の Key Policy（modules/kms の users.services.sns と users.roles[*].topics）へ同じ値を渡す。"
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^[A-Za-z0-9_-]{1,256}$", var.name))
    error_message = "name は英数字・_・- だけで、1-256 文字にしてください（FIFO の Topic は受けません）。"
  }
}

variable "kms_key_arn" {
  description = "Topic の暗号化に使う KMS の鍵の ARN。Topic と同じ partition・リージョン・アカウントの鍵にする。"
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^arn:aws[a-z-]*:kms:[a-z0-9-]+:[0-9]{12}:key/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$", var.kms_key_arn))
    error_message = "kms_key_arn は KMS の鍵の ARN（arn:<partition>:kms:<region>:<account>:key/<key-id>）にしてください。alias の ARN と多リージョンの鍵は受けません。"
  }
}

variable "publishers" {
  description = "Topic へ publish する同じアカウントの AWS のサービス（budgets・cloudwatch・costalerts・events）。鍵の Key Policy（modules/kms の users.services.sns）へ同じ値を渡す。IAM の role は Topic Policy ではなく、role の IAM policy と鍵の Key Policy で許す。"
  type        = set(string)
  nullable    = false

  validation {
    condition     = length(setsubtract(var.publishers, ["budgets", "cloudwatch", "costalerts", "events"])) == 0
    error_message = "publishers は budgets・cloudwatch・costalerts・events から選んでください。"
  }
}
