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
  description = "CI の role の ARN。plan は CI の plan、apply は workload root の apply、apply_identity は identity root の apply、unlock_identity は identity root の lock を外す role。state の鍵は、plan と apply_identity に復号と鍵の生成を、unlock_identity に identity root の lock file の復号だけを許す。state Bucket は、apply に workload root の state と lock file の書き込みを、apply_identity に identity root の state と lock file の書き込みを、unlock_identity に identity root の lock file の削除だけを許し、ほかの主体の object の書き込みを拒否する。どれも鍵と同じアカウントの role にする。"
  type = object({
    plan            = string
    apply           = string
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
    error_message = "ci_role_arns の plan・apply・apply_identity・unlock_identity に同じ role を渡さないでください。"
  }
}

variable "bootstrap_executor_user_name" {
  description = "新しいアカウントの bootstrap で、段4 の apply を実行する IAM user の名前（path を持たないもの）。値が在れば、この user は state Bucket の identity root の state と lock file に書き込める。段5 の最初の変更で null に戻す（docs/setup/bootstrap.md）。"
  type        = string
  default     = null

  validation {
    condition     = var.bootstrap_executor_user_name == null ? true : can(regex("^[A-Za-z0-9+=,.@_-]{1,64}$", var.bootstrap_executor_user_name))
    error_message = "bootstrap_executor_user_name は path を持たない IAM user の名前（英数字と +=,.@_- の64文字まで）にしてください。ARN は受けません。"
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
