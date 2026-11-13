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

variable "noncurrent_version_retention_days" {
  description = "上書きされた版を残す日数。この日数を過ぎた版は lifecycle が消す。現行版は失効させない。"
  type        = number
  nullable    = false

  validation {
    condition     = var.noncurrent_version_retention_days >= 1 && floor(var.noncurrent_version_retention_days) == var.noncurrent_version_retention_days
    error_message = "noncurrent_version_retention_days は1以上の整数にしてください。"
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

variable "object_writers" {
  description = "object の書き込みを許す role。map の key は Bucket Policy の Sid の一部になる。role_arn は role の ARN、keys はその role が書ける object の key、access は write（PutObject と DeleteObject）か delete（DeleteObject だけ）。ほかの主体の書き込み、keys の外への書き込み、access の外の操作、DeleteObjectVersion は Bucket Policy が拒否する。"
  type = map(object({
    role_arn = string
    keys     = set(string)
    access   = string
  }))
  nullable = false

  validation {
    condition     = length(var.object_writers) >= 1
    error_message = "object_writers に1つ以上の role を渡してください。"
  }

  # key は Sid（英数字だけ）に入る。bootstrapExecutor は bootstrap_executor の文が使う。
  validation {
    condition     = alltrue([for k in keys(var.object_writers) : can(regex("^[a-z][a-zA-Z0-9]*$", k)) && k != "bootstrapExecutor"])
    error_message = "object_writers の key は英小文字で始まる英数字にしてください。bootstrapExecutor は使えません。"
  }

  validation {
    condition     = alltrue([for w in values(var.object_writers) : can(regex("^arn:aws[a-z-]*:iam::[0-9]{12}:role/[A-Za-z0-9+=,.@_/-]+$", w.role_arn))])
    error_message = "object_writers の role_arn は IAM role の ARN にしてください。user・assumed-role・root・ワイルドカードは受けません。"
  }

  validation {
    condition     = length(distinct([for w in values(var.object_writers) : w.role_arn])) == length(var.object_writers)
    error_message = "object_writers の2つの項目に同じ role を渡さないでください。"
  }

  validation {
    condition     = alltrue([for w in values(var.object_writers) : length(w.keys) >= 1 && alltrue([for key in w.keys : can(regex("^[^/*?][^*?]*$", key))])])
    error_message = "object_writers の keys には1つ以上の key を渡してください。key は / で始めず、ワイルドカード（* と ?）を含めないでください。"
  }

  validation {
    condition     = alltrue([for w in values(var.object_writers) : contains(["write", "delete"], w.access)])
    error_message = "object_writers の access は write か delete にしてください。"
  }
}

variable "bootstrap_executor" {
  description = "bootstrap の間だけ object の書き込みを許す IAM user。user_name は path を持たない IAM user の名前、keys はその user が書ける object の key。値が在れば、この user は keys に PutObject と DeleteObject ができ、暗号化の方式の header の拒否の例外にも入る。null なら例外を持たない。"
  type = object({
    user_name = string
    keys      = set(string)
  })
  default = null

  validation {
    condition     = var.bootstrap_executor == null ? true : can(regex("^[A-Za-z0-9+=,.@_-]{1,64}$", var.bootstrap_executor.user_name))
    error_message = "bootstrap_executor の user_name は path を持たない IAM user の名前（英数字と +=,.@_- の64文字まで）にしてください。"
  }

  validation {
    condition     = var.bootstrap_executor == null ? true : length(var.bootstrap_executor.keys) >= 1 && alltrue([for key in var.bootstrap_executor.keys : can(regex("^[^/*?][^*?]*$", key))])
    error_message = "bootstrap_executor の keys には1つ以上の key を渡してください。key は / で始めず、ワイルドカード（* と ?）を含めないでください。"
  }
}
