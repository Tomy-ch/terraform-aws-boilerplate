variable "project" {
  description = "AWS 資源の名前の先頭に置くプロジェクトの token。Access Analyzer の analyzer は `<project>-<env>-external`、Config の recorder と delivery channel は `<project>-<env>` になる。"
  type        = string
  nullable    = false

  # ハイフンを許すと `<project>-<env>` の境目を名前から読めなくなる。
  validation {
    condition     = can(regex("^[a-z0-9]+$", var.project))
    error_message = "project は英小文字と数字だけにしてください。"
  }

  # 最も長い名前 `<project>-log-archive-external` を、analyzer の名前の上限の255文字に収める。
  validation {
    condition     = length(var.project) <= 235
    error_message = "project は235文字以内にしてください。"
  }
}

variable "env" {
  description = "account-baseline を置くアカウントの env の token。管理アカウントは `admin`。`sandbox` では AWS Config を置かず、`admin` では IAM の password policy を置かない。"
  type        = string
  nullable    = false

  validation {
    condition     = contains(["admin", "security", "log-archive", "sandbox", "dev", "stg", "prd"], var.env)
    error_message = "env は admin / security / log-archive / sandbox / dev / stg / prd のいずれかにしてください。"
  }
}

variable "config_delivery" {
  description = "AWS Config の配信先。ログ保管のアカウントの audit-evidence の `bucket_name` と、その鍵の ARN。sandbox では使わない。"
  type = object({
    bucket_name = string
    kms_key_arn = string
  })
  nullable = false

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$", var.config_delivery.bucket_name))
    error_message = "config_delivery.bucket_name は S3 の Bucket の名前にしてください。"
  }

  validation {
    condition     = can(regex("^arn:aws[a-z-]*:kms:[a-z0-9-]+:[0-9]{12}:key/[0-9a-f-]{36}$", var.config_delivery.kms_key_arn))
    error_message = "config_delivery.kms_key_arn は KMS の鍵の ARN（arn:<partition>:kms:<region>:<account>:key/<id>）にしてください。alias は受けません。"
  }
}

variable "config_history_retention_days" {
  description = "AWS Config の構成の履歴を保持する日数。sandbox では使わない。"
  type        = number
  nullable    = false

  validation {
    condition     = var.config_history_retention_days >= 30 && var.config_history_retention_days <= 2557 && floor(var.config_history_retention_days) == var.config_history_retention_days
    error_message = "config_history_retention_days は 30 以上 2557 以下の整数にしてください。"
  }
}

variable "security_contact" {
  description = "セキュリティの代替の連絡先（Security Hub の Account.1）。個人の情報を含むので sensitive にする。"
  type = object({
    name          = string
    title         = string
    email_address = string
    phone_number  = string
  })
  nullable  = false
  sensitive = true

  validation {
    condition     = alltrue([for v in values(var.security_contact) : length(trimspace(v)) > 0])
    error_message = "security_contact の4つの値は、どれも空にできません。"
  }

  validation {
    condition     = can(regex("^[^@[:space:]]+@[^@[:space:]]+\\.[^@[:space:]]+$", var.security_contact.email_address))
    error_message = "security_contact.email_address はメールアドレスにしてください。"
  }

  validation {
    condition     = can(regex("^[+][0-9 -]{4,49}$", var.security_contact.phone_number))
    error_message = "security_contact.phone_number は + で始まる国際形式の電話番号にしてください。"
  }
}
