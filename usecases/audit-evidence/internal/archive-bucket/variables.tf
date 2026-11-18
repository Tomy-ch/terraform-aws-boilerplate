variable "name" {
  description = "Bucket 名の前半。module が `-<account_id>-<region>-an` を付けて完全な名前にする。"
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9]([a-z0-9-]*[a-z0-9])?$", var.name))
    error_message = "name は英小文字・数字・ハイフンだけで、先頭と末尾は英小文字か数字にしてください。"
  }

  validation {
    condition     = !anytrue([for p in ["xn--", "sthree-", "amzn-s3-demo-"] : startswith(var.name, p)])
    error_message = "name を S3 が予約している接頭辞（xn--、sthree-、amzn-s3-demo-）で始めないでください。"
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

variable "retention" {
  description = "Object Lock の既定の保持。mode は COMPLIANCE か GOVERNANCE、days は各版をロックする日数。同じ日数で lifecycle が版を失効させる。"
  type = object({
    mode = string
    days = number
  })
  nullable = false

  validation {
    condition     = contains(["COMPLIANCE", "GOVERNANCE"], var.retention.mode)
    error_message = "retention.mode は COMPLIANCE か GOVERNANCE にしてください。"
  }

  # S3 の保持の上限は 100 年（Amazon S3 User Guide「Object Lock considerations」）。
  validation {
    condition     = var.retention.days >= 1 && var.retention.days <= 36500 && floor(var.retention.days) == var.retention.days
    error_message = "retention.days は1以上 36500 以下の整数にしてください。"
  }
}

variable "force_destroy" {
  description = "destroy のとき、保持中の版を含む object を消してから Bucket を消すか。GOVERNANCE の版を消すには、destroy する主体が s3:BypassGovernanceRetention を要る。"
  type        = bool
  nullable    = false
}

variable "writers" {
  description = "Bucket Policy で書き込みを許すサービス。cloudtrail_organization_trail は組織の trail（名前・所有するアカウント・組織の ID・リージョン）、config_source_accounts は AWS Config が配信するアカウント。"
  type = object({
    cloudtrail_organization_trail = object({
      trail_name       = string
      owner_account_id = string
      organization_id  = string
      region           = string
    })
    config_source_accounts = optional(set(string), [])
  })
  nullable = false

  validation {
    condition     = can(regex("^[A-Za-z0-9][A-Za-z0-9._-]{2,127}$", var.writers.cloudtrail_organization_trail.trail_name)) && !can(regex("[._-][._-]", var.writers.cloudtrail_organization_trail.trail_name))
    error_message = "writers.cloudtrail_organization_trail.trail_name は CloudTrail の trail の名前（英数字で始まる 3-128 文字の英数字・.・_・-。記号を連ねない）にしてください。"
  }

  validation {
    condition     = can(regex("^[0-9]{12}$", var.writers.cloudtrail_organization_trail.owner_account_id))
    error_message = "writers.cloudtrail_organization_trail.owner_account_id は12桁のアカウントの ID にしてください。"
  }

  validation {
    condition     = can(regex("^o-[a-z0-9]{10,32}$", var.writers.cloudtrail_organization_trail.organization_id))
    error_message = "writers.cloudtrail_organization_trail.organization_id は組織の ID（o-<10-32 文字の英小文字と数字>）にしてください。"
  }

  validation {
    condition     = can(regex("^[a-z]{2}(-[a-z]+)+-[0-9]$", var.writers.cloudtrail_organization_trail.region))
    error_message = "writers.cloudtrail_organization_trail.region はリージョンの名前（例: ap-northeast-1）にしてください。"
  }

  validation {
    condition     = alltrue([for a in var.writers.config_source_accounts : can(regex("^[0-9]{12}$", a))])
    error_message = "writers.config_source_accounts は12桁のアカウントの ID にしてください。"
  }
}

variable "access_log_source_name" {
  description = "S3 のサーバーアクセスログの delivery source の名前。受け口の Log Group の resource policy が同じ名前を名指す。"
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^[A-Za-z0-9_-]{1,60}$", var.access_log_source_name))
    error_message = "access_log_source_name は英数字・_・- だけで 1-60 文字にしてください。"
  }
}

variable "access_log_destination_arn" {
  description = "サーバーアクセスログの配送の宛先（delivery destination）の ARN。"
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^arn:aws[a-z-]*:logs:[a-z0-9-]+:[0-9]{12}:delivery-destination:[A-Za-z0-9_-]{1,60}$", var.access_log_destination_arn))
    error_message = "access_log_destination_arn は CloudWatch Logs の delivery destination の ARN にしてください。"
  }
}

variable "notify_topic_arn" {
  description = "配信の途絶の alarm を通知する SNS の Topic の ARN。Bucket と同じアカウントとリージョンの Topic にする。"
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^arn:aws[a-z-]*:sns:[a-z0-9-]+:[0-9]{12}:[A-Za-z0-9_-]{1,256}$", var.notify_topic_arn))
    error_message = "notify_topic_arn は SNS の Topic の ARN にしてください。FIFO の Topic は受けません。"
  }
}
