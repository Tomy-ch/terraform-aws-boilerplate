variable "project" {
  description = "AWS 資源の名前の先頭に置くプロジェクトの token。policy の名前 `<project>-<policy>`、break-glass の role の名前 `<project>-break-glass`、permission set の名前 `<project>-<キー>` を組む。メンバーの state Bucket と CI の role は `deployment-identity`・`state-backend` と同じ token で名指す。"
  type        = string

  # ハイフンを許すと `<project>-<env>` の境目を名前から読めなくなる。SCP は `<project>-*-ci-<役割>` で CI の role を名指す。
  validation {
    condition     = can(regex("^[a-z0-9]+$", var.project))
    error_message = "project は英小文字と数字だけにしてください。"
  }

  # 最も長い permission set の名前 `<project>-organization-admin` を IAM Identity Center の上限の32文字に収める。
  # 名前は全 resource に在り後から変えられないので、identity_center が null の間も掛ける。
  validation {
    condition     = length(var.project) <= 13
    error_message = "project は13文字以内にしてください。"
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

variable "identity_center" {
  description = "IAM Identity Center の割当。Identity Center を有効にする前（bootstrap の段1-5）は null にし、null のとき permission set を作らない。`user`・`group` は人が付けた label で、ID は管理アカウントの parameter `/<project>/organization-baseline/identity-center-principals` から読む。管理アカウントへは USER、メンバーアカウントへは GROUP で割り当てる。"
  type = object({
    management_assignments = list(object({ permission_set = string, user = string }))
    member_assignments     = optional(list(object({ account = string, permission_set = string, group = string })), [])
  })
  default = null

  validation {
    condition = var.identity_center == null || alltrue([
      for a in concat(coalesce(try(var.identity_center.management_assignments, null), []), coalesce(try(var.identity_center.member_assignments, null), [])) :
      contains(["read_only", "terraform_plan", "billing", "support", "organization_admin"], a.permission_set)
    ])
    error_message = "permission_set は read_only・terraform_plan・billing・support・organization_admin のいずれかにしてください。"
  }

  validation {
    condition     = var.identity_center == null || alltrue([for a in coalesce(try(var.identity_center.member_assignments, null), []) : a.permission_set != "organization_admin"])
    error_message = "organization_admin は管理アカウントの外へ割り当てられません。"
  }

  validation {
    condition     = var.identity_center == null || try(var.identity_center.management_assignments, null) != null
    error_message = "management_assignments を null にしないでください。"
  }

  validation {
    condition     = var.identity_center == null || try(length(var.identity_center.management_assignments) > 0, true)
    error_message = "management_assignments を空にしないでください。"
  }

  # 最後の管理者の割当を消す plan を落とす。null と空は上の2つが落とす。
  validation {
    condition = var.identity_center == null || try(
      length(var.identity_center.management_assignments) == 0 || anytrue([for a in var.identity_center.management_assignments : a.permission_set == "organization_admin"]),
      true
    )
    error_message = "management_assignments に organization_admin の割当を1つ以上置いてください。"
  }

  # label は resource の address に載る。人の名前とメールを使わない。
  validation {
    condition = var.identity_center == null || alltrue(concat(
      [for a in coalesce(try(var.identity_center.management_assignments, null), []) : can(regex("^[a-z][a-z0-9-]{0,31}$", a.user))],
      [for a in coalesce(try(var.identity_center.member_assignments, null), []) : can(regex("^[a-z][a-z0-9-]{0,31}$", a.group))],
    ))
    error_message = "user と group の label は、英小文字で始まり英小文字・数字・ハイフンだけの32文字以内にしてください。"
  }

  validation {
    condition = var.identity_center == null || (
      length(distinct([for a in coalesce(try(var.identity_center.management_assignments, null), []) : "${a.permission_set}/${a.user}"])) == length(coalesce(try(var.identity_center.management_assignments, null), []))
      && length(distinct([for a in coalesce(try(var.identity_center.member_assignments, null), []) : "${a.account}/${a.permission_set}/${a.group}"])) == length(coalesce(try(var.identity_center.member_assignments, null), []))
    )
    error_message = "同じ割当を2回書かないでください。"
  }
}

variable "state_bucket_arn" {
  description = "管理アカウントの state Bucket の ARN（`state-backend` の `bucket_arn`）。`organization_admin` の permission set に、この Bucket と object の書き込みを拒ませる。"
  type        = string

  validation {
    condition     = can(regex("^arn:aws[a-z-]*:s3:::[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$", var.state_bucket_arn))
    error_message = "state_bucket_arn は S3 Bucket の ARN（arn:<partition>:s3:::<bucket>。Bucket 名は63文字以内）にしてください。"
  }
}

variable "plan_output_bucket_arn" {
  description = "管理アカウントの plan の出力の Bucket の ARN（`state-backend` の `plan_output_bucket_arn`）。`organization_admin` の permission set に、この Bucket と object の書き込みを拒ませる。"
  type        = string

  validation {
    condition     = can(regex("^arn:aws[a-z-]*:s3:::[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$", var.plan_output_bucket_arn))
    error_message = "plan_output_bucket_arn は S3 Bucket の ARN（arn:<partition>:s3:::<bucket>。Bucket 名は63文字以内）にしてください。"
  }
}

variable "apply_output_bucket_arn" {
  description = "管理アカウントの apply の出力の Bucket の ARN（`state-backend` の `apply_output_bucket_arn`）。`organization_admin` の permission set に、この Bucket と object の書き込みを拒ませる。"
  type        = string

  validation {
    condition     = can(regex("^arn:aws[a-z-]*:s3:::[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$", var.apply_output_bucket_arn))
    error_message = "apply_output_bucket_arn は S3 Bucket の ARN（arn:<partition>:s3:::<bucket>。Bucket 名は63文字以内）にしてください。"
  }
}

variable "state_key_arn" {
  description = "管理アカウントの state の鍵の ARN（`state-backend` の `state_key_arn`）。`organization_admin` の permission set に、この鍵の変更と暗号操作を拒ませる。"
  type        = string

  validation {
    condition     = can(regex("^arn:aws[a-z-]*:kms:[a-z0-9-]+:[0-9]{12}:key/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$", var.state_key_arn))
    error_message = "state_key_arn は KMS の鍵の ARN（arn:<partition>:kms:<region>:<account>:key/<key-id>）にしてください。alias の ARN は受けません。"
  }
}
