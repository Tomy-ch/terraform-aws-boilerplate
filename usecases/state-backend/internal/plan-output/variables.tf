variable "name" {
  description = "Bucket 名の前半。module が `-<account_id>-<region>-an` を付けて完全な名前にする。"
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9]([a-z0-9-]*[a-z0-9])?$", var.name))
    error_message = "name は英小文字・数字・ハイフンだけで、先頭と末尾は英小文字か数字にしてください。"
  }

  # 接尾辞の禁止は、module が付ける `-an` で終わるので当たらない。
  validation {
    condition     = !anytrue([for p in ["xn--", "sthree-", "amzn-s3-demo-"] : startswith(var.name, p)])
    error_message = "name を S3 が予約している接頭辞（xn--、sthree-、amzn-s3-demo-）で始めないでください。"
  }
}

variable "retention_days" {
  description = "plan の出力を残す日数。現行版はこの日数で失効し、上書きされた版と失効した版はさらにこの日数で消える。"
  type        = number
  nullable    = false

  validation {
    condition     = var.retention_days >= 1 && floor(var.retention_days) == var.retention_days
    error_message = "retention_days は1以上の整数にしてください。"
  }
}

variable "kms_key_arn" {
  description = "既定の暗号化に使う KMS の鍵の ARN。Bucket と同じ partition・リージョン・アカウントの鍵にする。"
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^arn:aws[a-z-]*:kms:[a-z0-9-]+:[0-9]{12}:key/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$", var.kms_key_arn))
    error_message = "kms_key_arn は KMS の鍵の ARN（arn:<partition>:kms:<region>:<account>:key/<key-id>）にしてください。alias の ARN と多リージョンの鍵は受けません。"
  }
}

variable "writers" {
  description = "object を書ける role と key の接頭辞の組の集合。key_prefix は / で終わる接頭辞で、空なら Bucket のすべての key。組の外の主体と接頭辞への s3:PutObject と、すべての主体の s3:DeleteObject・s3:DeleteObjectVersion を Bucket Policy が拒否する。"
  type = set(object({
    role_arn   = string
    key_prefix = string
  }))
  nullable = false

  validation {
    condition     = length(var.writers) >= 1
    error_message = "writers に1つ以上の組を渡してください。"
  }

  validation {
    condition     = alltrue([for w in var.writers : can(regex("^arn:aws[a-z-]*:iam::[0-9]{12}:role/[A-Za-z0-9+=,.@_/-]+$", w.role_arn))])
    error_message = "writers の role_arn は IAM role の ARN にしてください。user・assumed-role・root・ワイルドカードは受けません。"
  }

  validation {
    condition     = alltrue([for w in var.writers : can(regex("^([^/*?]+/)*$", w.key_prefix))])
    error_message = "writers の key_prefix は空か、/ で終わる接頭辞にしてください。/ で始めず、ワイルドカード（* と ?）を含めないでください。"
  }
}
