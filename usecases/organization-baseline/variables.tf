variable "project" {
  description = "AWS 資源の名前の先頭に置くプロジェクトの token。policy の名前 `<project>-<policy>`、break-glass の role の名前 `<project>-break-glass` を組む。メンバーの state Bucket と CI の role は `deployment-identity`・`state-backend` と同じ token で名指す。"
  type        = string

  # ハイフンを許すと `<project>-<env>` の境目を名前から読めなくなる。SCP は `<project>-*-ci-<役割>` で CI の role を名指す。
  validation {
    condition     = can(regex("^[a-z0-9]+$", var.project))
    error_message = "project は英小文字と数字だけにしてください。"
  }

  # 最も長いアカウント名 `<project>-log-archive` を Organizations の上限の50文字に収める。
  validation {
    condition     = length(var.project) <= 38
    error_message = "project は38文字以内にしてください。"
  }
}

variable "bootstrapping" {
  description = "立ち上げ中のメンバーアカウントの env（`security`・`log-archive`）。管理アカウントの identity root がメンバーの `state-backend`・`deployment-identity` を作り終えるまで入れておく。ここに無いアカウントでは `OrganizationAccountAccessRole` の使用を SCP が拒む。"
  type        = set(string)
  default     = []

  validation {
    condition     = alltrue([for e in var.bootstrapping : contains(["security", "log-archive"], e)])
    error_message = "bootstrapping はメンバーアカウントの env（security・log-archive）だけにしてください。"
  }
}

variable "bootstrap_read_principal_arn" {
  description = "メンバーアカウントの立ち上げの読み取りの role（`<project>-bootstrap-read`）を引き受けてよい、管理アカウントの role の ARN。管理アカウントの identity root の plan の role を渡す。"
  type        = string

  validation {
    condition     = can(regex("^arn:aws[a-z-]*:iam::[0-9]{12}:role/[A-Za-z0-9+=,.@_/-]+$", var.bootstrap_read_principal_arn)) && !strcontains(var.bootstrap_read_principal_arn, "*")
    error_message = "bootstrap_read_principal_arn は、wildcard を含まない IAM role の ARN にしてください。"
  }
}
