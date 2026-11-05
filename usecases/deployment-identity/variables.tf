variable "project" {
  description = "AWS 資源の名前の先頭に置くプロジェクトの token。role 名は `<project>-<env>-ci-<役割>` になる。"
  type        = string

  # ハイフンを許すと `<project>-<env>` の境目を名前から読めなくなる。
  validation {
    condition     = can(regex("^[a-z0-9]+$", var.project))
    error_message = "project は英小文字と数字だけにしてください。"
  }

  # 最も長い role 名 `<project>-sandbox-ci-apply-identity` を IAM の上限の64文字に収める。
  validation {
    condition     = length(var.project) <= 38
    error_message = "project は38文字以内にしてください。"
  }
}

variable "env" {
  description = "role を置くアカウントの env の token。管理アカウントは `admin`。GitHub の environment 名は `<env>-plan` / `<env>-apply` / `<env>-apply-identity` になる。"
  type        = string

  validation {
    condition     = contains(["dev", "stg", "prd", "sandbox", "admin"], var.env)
    error_message = "env は dev / stg / prd / sandbox / admin のいずれかにしてください。"
  }
}

variable "github" {
  description = "role を assume してよい GitHub のリポジトリ。所有者とリポジトリの名前と、変わらない数値の ID（`gh api repos/<owner>/<repo> --jq '.owner.id, .id'`）を受ける。"
  type = object({
    owner         = string
    owner_id      = string
    repository    = string
    repository_id = string
  })

  # 信頼条件の sub に `*` `?` `:` が入らないよう、GitHub が名前に許す文字に限る。
  validation {
    condition     = length(var.github.owner) <= 39 && can(regex("^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?$", var.github.owner))
    error_message = "github.owner は英数字とハイフンだけの39文字以内で、先頭と末尾を英数字にしてください。"
  }

  validation {
    condition     = length(var.github.repository) <= 100 && can(regex("^[A-Za-z0-9._-]+$", var.github.repository))
    error_message = "github.repository は英数字・ピリオド・アンダースコア・ハイフンだけの100文字以内にしてください。"
  }

  validation {
    condition     = can(regex("^[1-9][0-9]*$", var.github.owner_id)) && can(regex("^[1-9][0-9]*$", var.github.repository_id))
    error_message = "github.owner_id と github.repository_id は、0 で始まらない数字の文字列にしてください。"
  }
}

variable "state_bucket_arn" {
  description = "このアカウントの state Bucket の ARN（`state-backend` の `bucket_arn`）。CI の role の S3 の Action をこの Bucket と state の key に絞る。"
  type        = string

  validation {
    condition     = can(regex("^arn:aws[a-z-]*:s3:::[a-z0-9][a-z0-9.-]*[a-z0-9]$", var.state_bucket_arn))
    error_message = "state_bucket_arn は S3 Bucket の ARN（arn:<partition>:s3:::<bucket>）にしてください。"
  }
}

variable "plan_output_bucket_arn" {
  description = "このアカウントの plan の出力の Bucket の ARN（`state-backend` の `plan_output_bucket_arn`）。plan の role の書き込みと、Bucket の設定の Action をこの Bucket に絞る。"
  type        = string

  validation {
    condition     = can(regex("^arn:aws[a-z-]*:s3:::[a-z0-9][a-z0-9.-]*[a-z0-9]$", var.plan_output_bucket_arn))
    error_message = "plan_output_bucket_arn は S3 Bucket の ARN（arn:<partition>:s3:::<bucket>）にしてください。"
  }
}
