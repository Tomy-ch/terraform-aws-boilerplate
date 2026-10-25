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
