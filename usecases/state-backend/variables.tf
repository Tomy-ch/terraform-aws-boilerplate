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

variable "plan_output_retention_days" {
  description = "CI の plan の出力を残す日数。現行版はこの日数で失効し、上書きされた版と失効した版はさらにこの日数で消える。"
  type        = number
  default     = 7

  validation {
    condition     = var.plan_output_retention_days >= 1 && floor(var.plan_output_retention_days) == var.plan_output_retention_days
    error_message = "plan_output_retention_days は1以上の整数にしてください。"
  }
}
