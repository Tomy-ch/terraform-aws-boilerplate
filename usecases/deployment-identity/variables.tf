variable "project" {
  description = "AWS 資源の名前の先頭に置くプロジェクトの token。role 名は `<project>-<env>-ci-<役割>` になる。"
  type        = string

  # ハイフンを許すと `<project>-<env>` の境目を名前から読めなくなる。
  validation {
    condition     = can(regex("^[a-z0-9]+$", var.project))
    error_message = "project は英小文字と数字だけにしてください。"
  }

  # 最も長い role 名 `<project>-sandbox-ci-unlock-identity` を IAM の上限の64文字に収める。
  validation {
    condition     = length(var.project) <= 37
    error_message = "project は37文字以内にしてください。"
  }
}

variable "env" {
  description = "role を置くアカウントの env の token。管理アカウントは `admin`。GitHub の environment 名は `<env>-plan` / `<env>-apply` / `<env>-apply-identity` / `<env>-unlock-identity` になる。"
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

variable "state_key_arn" {
  description = "このアカウントの state の鍵の ARN（`state-backend` の `state_key_arn`）。plan と apply-identity の role の鍵の Action と、apply の role の鍵の Deny をこの鍵に絞る。"
  type        = string

  # 多リージョンの鍵（mrk-）を受けないのは、modules/kms が多リージョンの鍵を作らないからである。
  validation {
    condition     = can(regex("^arn:aws[a-z-]*:kms:[a-z0-9-]+:[0-9]{12}:key/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$", var.state_key_arn))
    error_message = "state_key_arn は KMS の鍵の ARN（arn:<partition>:kms:<region>:<account>:key/<key-id>）にしてください。alias の ARN と多リージョンの鍵は受けません。"
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

variable "access_log_group_arn" {
  description = "state Bucket と plan の出力の Bucket の S3 サーバーアクセスログを受ける Log Group の ARN（`state-backend` の `access_log_group_arn`。`:*` を除いた形）。apply-identity の Log Group の Action と、apply の role の Deny をこの Log Group に絞る。"
  type        = string

  # 名前の形は delivery destination と同じ（modules/vended-log-group の name）。
  validation {
    condition     = can(regex("^arn:aws[a-z-]*:logs:[a-z0-9-]+:[0-9]{12}:log-group:[A-Za-z0-9_-]{1,60}$", var.access_log_group_arn))
    error_message = "access_log_group_arn は CloudWatch Logs の Log Group の ARN（arn:<partition>:logs:<region>:<account>:log-group:<name>。name は英数字と - _ の60文字以内、末尾の :* を付けない）にしてください。"
  }
}

variable "access_log_destination_arn" {
  description = "S3 サーバーアクセスログの delivery destination の ARN（`state-backend` の `access_log_destination_arn`）。delivery destination の Action と、apply の role の Deny をこれに絞る。"
  type        = string

  validation {
    condition     = can(regex("^arn:aws[a-z-]*:logs:[a-z0-9-]+:[0-9]{12}:delivery-destination:[A-Za-z0-9_-]{1,60}$", var.access_log_destination_arn))
    error_message = "access_log_destination_arn は CloudWatch Logs の delivery destination の ARN（arn:<partition>:logs:<region>:<account>:delivery-destination:<name>）にしてください。"
  }
}

variable "access_log_source_arns" {
  description = "state Bucket と plan の出力の Bucket の delivery source の ARN（`state-backend` の `access_log_source_arns`）。delivery source の Action と、apply の role の Deny をこれに絞る。"
  type        = list(string)
  nullable    = false

  validation {
    condition     = length(var.access_log_source_arns) > 0 && alltrue([for a in var.access_log_source_arns : can(regex("^arn:aws[a-z-]*:logs:[a-z0-9-]+:[0-9]{12}:delivery-source:[A-Za-z0-9_-]{1,60}$", a))])
    error_message = "access_log_source_arns は1件以上の、CloudWatch Logs の delivery source の ARN（arn:<partition>:logs:<region>:<account>:delivery-source:<name>）にしてください。"
  }

  validation {
    condition     = length(distinct(var.access_log_source_arns)) == length(var.access_log_source_arns)
    error_message = "access_log_source_arns に同じ ARN を重ねないでください。"
  }
}
