variable "project" {
  description = "AWS 資源の名前の先頭に置くプロジェクトの token。Bucket 名は `<project>-<env>-tfstate-<account_id>-<region>-an` になる。"
  type        = string

  # ハイフンを許すと `<project>-<env>` の境目を名前から読めなくなる。
  validation {
    condition     = can(regex("^[a-z0-9]+$", var.project))
    error_message = "project は英小文字と数字だけにしてください。"
  }
}

variable "env" {
  description = "state Bucket を置くアカウントの env の token。管理アカウントは `admin`。"
  type        = string

  validation {
    condition     = contains(["dev", "stg", "prd", "sandbox", "admin"], var.env)
    error_message = "env は dev / stg / prd / sandbox / admin のいずれかにしてください。"
  }
}

variable "ci_role_arns" {
  description = "state の鍵で暗号操作を許す CI の role の ARN。plan は CI の plan、apply_identity は identity root の apply、unlock_identity は identity root の lock を外す role。plan と apply_identity には復号と鍵の生成を、unlock_identity には identity root の lock file の復号だけを許す。どれも鍵と同じアカウントの role にする。"
  type = object({
    plan            = string
    apply_identity  = string
    unlock_identity = string
  })
  nullable = false

  validation {
    condition     = alltrue([for arn in values(var.ci_role_arns) : can(regex("^arn:aws[a-z-]*:iam::[0-9]{12}:role/[A-Za-z0-9+=,.@_/-]+$", arn))])
    error_message = "ci_role_arns の値は IAM role の ARN にしてください。user・root・ワイルドカードは受けません。"
  }

  # 同じ role を2つの役割に渡すと、ある役割が別の役割の権限で走っても気づけない。
  validation {
    condition     = length(distinct(values(var.ci_role_arns))) == length(values(var.ci_role_arns))
    error_message = "ci_role_arns の plan・apply_identity・unlock_identity に同じ role を渡さないでください。"
  }
}

variable "state_noncurrent_version_retention_days" {
  description = "上書きされた state の版を残す日数。この日数を過ぎた版は消え、その版へは戻せない。現行版は失効させない（state-backend/ADR-0103）。"
  type        = number
  nullable    = false

  validation {
    condition     = var.state_noncurrent_version_retention_days >= 1 && floor(var.state_noncurrent_version_retention_days) == var.state_noncurrent_version_retention_days
    error_message = "state_noncurrent_version_retention_days は1以上の整数にしてください。"
  }
}

variable "plan_output_retention_days" {
  description = "CI の plan の出力を残す日数。現行版はこの日数で失効し、上書きされた版と失効した版はさらにこの日数で消える。"
  type        = number
  default     = 7
  nullable    = false

  validation {
    condition     = var.plan_output_retention_days >= 1 && floor(var.plan_output_retention_days) == var.plan_output_retention_days
    error_message = "plan_output_retention_days は1以上の整数にしてください。"
  }
}
