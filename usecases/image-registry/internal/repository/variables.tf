variable "name" {
  description = "repository の名前（`<project>-<env>/<論理名>`）。usecase が組む。"
  type        = string

  validation {
    condition     = length(var.name) <= 256 && can(regex("^[a-z0-9]+(-[a-z0-9]+)*/[a-z0-9]+(-[a-z0-9]+)*$", var.name))
    error_message = "name は <接頭辞>/<論理名> の形の、小文字・数字・ハイフンだけの名前にしてください。"
  }
}

variable "publisher_role_name" {
  description = "発火側が assume する push の role の名前。"
  type        = string

  validation {
    condition     = length(var.publisher_role_name) <= 64 && can(regex("^[A-Za-z0-9+=,.@_-]+$", var.publisher_role_name))
    error_message = "publisher_role_name は IAM の role の名前に使える文字だけの64文字以内にしてください。"
  }
}

variable "kms_key_arn" {
  description = "repository を暗号化する鍵の ARN。"
  type        = string

  validation {
    condition     = can(regex("^arn:aws[a-z-]*:kms:[a-z0-9-]+:[0-9]{12}:key/[0-9a-f-]+$", var.kms_key_arn))
    error_message = "kms_key_arn は KMS の鍵の ARN にしてください。alias は受けません。"
  }
}

variable "retained_images" {
  description = "タグ付きのイメージを残す件数。超えた分は push の古い順に消える。"
  type        = number

  validation {
    condition     = var.retained_images >= 2 && var.retained_images <= 10000 && floor(var.retained_images) == var.retained_images
    error_message = "retained_images は 2 以上 10000 以下の整数にしてください。1 では直前の版へ戻せません。"
  }
}

variable "publisher" {
  description = "この repository へ push してよい GitHub のリポジトリと environment。"
  type = object({
    owner         = string
    owner_id      = string
    repository    = string
    repository_id = string
    environment   = string
  })

  # 信頼条件の sub に `*` `?` `:` `/` `@` が入らないよう、GitHub が名前に許す文字に限る（ADR-0602 決定15・19）。
  validation {
    condition     = length(var.publisher.owner) <= 39 && can(regex("^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?$", var.publisher.owner))
    error_message = "publisher.owner は英数字とハイフンだけの39文字以内で、先頭と末尾を英数字にしてください。"
  }

  validation {
    condition     = length(var.publisher.repository) <= 100 && can(regex("^[A-Za-z0-9._-]+$", var.publisher.repository))
    error_message = "publisher.repository は英数字・ピリオド・アンダースコア・ハイフンだけの100文字以内にしてください。"
  }

  validation {
    condition     = can(regex("^[1-9][0-9]*$", var.publisher.owner_id)) && can(regex("^[1-9][0-9]*$", var.publisher.repository_id))
    error_message = "publisher.owner_id と publisher.repository_id は、0 で始まらない数字の文字列にしてください。"
  }

  validation {
    condition     = length(var.publisher.environment) <= 255 && can(regex("^[A-Za-z0-9._-]+$", var.publisher.environment))
    error_message = "publisher.environment は英数字・ピリオド・アンダースコア・ハイフンだけの255文字以内にしてください。"
  }
}

variable "github_oidc_provider_arn" {
  description = "このアカウントの GitHub Actions の OIDC provider の ARN。"
  type        = string

  validation {
    condition     = can(regex("^arn:aws[a-z-]*:iam::[0-9]{12}:oidc-provider/token\\.actions\\.githubusercontent\\.com$", var.github_oidc_provider_arn))
    error_message = "github_oidc_provider_arn は GitHub Actions の OIDC provider の ARN にしてください。"
  }
}
