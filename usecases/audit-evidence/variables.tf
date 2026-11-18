variable "project" {
  description = "AWS 資源の名前の先頭に置くプロジェクトの token。Bucket 名は `<project>-<env>-audit-<account_id>-<region>-an` になる。"
  type        = string

  # ハイフンを許すと `<project>-<env>` の境目を名前から読めなくなる。
  validation {
    condition     = can(regex("^[a-z0-9]+$", var.project))
    error_message = "project は英小文字と数字だけにしてください。"
  }
}

variable "env" {
  description = "置くアカウントの env の token。組織の証跡を保管するログ保管のアカウントは `log-archive`、検証は `sandbox`。log-archive は保持を COMPLIANCE で、sandbox は GOVERNANCE の1日で掛ける。"
  type        = string

  validation {
    condition     = contains(["log-archive", "sandbox"], var.env)
    error_message = "env は log-archive か sandbox にしてください。"
  }
}

variable "retention_days" {
  description = "証跡の各版を Object Lock でロックし、その後に失効させるまでの日数。log-archive では取り消せない（COMPLIANCE）。sandbox では使わず、1日で掛ける。"
  type        = number
  nullable    = false

  # 範囲の根拠は audit-evidence/ADR-0101 決定3。
  validation {
    condition     = var.retention_days >= 365 && var.retention_days <= 3653 && floor(var.retention_days) == var.retention_days
    error_message = "retention_days は 365 以上 3653 以下の整数にしてください。"
  }
}

variable "writers" {
  description = "証跡を書き込むサービス。cloudtrail_organization_trail は組織の trail（名前・所有する管理アカウントの ID・組織の ID・リージョン）、config_source_accounts は AWS Config が配信するアカウントの ID。Bucket Policy と鍵の Key Policy の文は module が組む。"
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

variable "access_log_retention_days" {
  description = "S3 のサーバーアクセスログの Log Group の保持日数。"
  type        = number
  nullable    = false

  validation {
    condition     = contains([365, 400, 545, 731, 1096, 1827, 2192, 2557, 2922, 3288, 3653], var.access_log_retention_days)
    error_message = "access_log_retention_days は 365, 400, 545, 731, 1096, 1827, 2192, 2557, 2922, 3288, 3653 のいずれかにしてください。"
  }
}

variable "notify_topic_arn" {
  description = "配信の途絶の alarm を通知する SNS の Topic の ARN。同じアカウントとリージョンの Topic（ops-notification）にする。"
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^arn:aws[a-z-]*:sns:[a-z0-9-]+:[0-9]{12}:[A-Za-z0-9_-]{1,256}$", var.notify_topic_arn))
    error_message = "notify_topic_arn は SNS の Topic の ARN にしてください。FIFO の Topic は受けません。"
  }
}
