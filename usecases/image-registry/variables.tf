variable "project" {
  description = "名前の先頭の token。repository・role・鍵の alias の名前に入る。"
  type        = string

  validation {
    condition     = length(var.project) <= 20 && can(regex("^[a-z0-9]+$", var.project))
    error_message = "project は小文字と数字だけの20文字以内にしてください。"
  }
}

variable "env" {
  description = "このアカウントの環境の役割（dev / stg / prd / sandbox）。名前の2つ目の token になる。"
  type        = string

  # 管理アカウントにワークロードを置かない（docs/v1.0-plan/scope.md 第10.1節）。
  validation {
    condition     = contains(["dev", "stg", "prd", "sandbox"], var.env)
    error_message = "env は dev・stg・prd・sandbox のどれかにしてください。"
  }
}

variable "repositories" {
  description = "作る repository。キーは論理名で、repository は `<project>-<env>/<論理名>` になる。retained_images はタグ付きのイメージを残す件数、publisher はその repository へ push してよい GitHub のリポジトリ（所有者・名前・変わらない数値の ID）と environment。"
  type = map(object({
    retained_images = number
    publisher = object({
      owner         = string
      owner_id      = string
      repository    = string
      repository_id = string
      environment   = string
    })
  }))
  nullable = false

  validation {
    condition     = length(var.repositories) > 0
    error_message = "repositories に1件以上渡してください。"
  }

  validation {
    condition     = alltrue([for k, _ in var.repositories : length(k) <= 20 && can(regex("^[a-z0-9]+(-[a-z0-9]+)*$", k))])
    error_message = "repositories のキーは小文字・数字・ハイフンだけの20文字以内で、先頭と末尾を小文字か数字にしてください。"
  }
}

variable "github_oidc_provider_arn" {
  description = "このアカウントの GitHub Actions の OIDC provider の ARN（`deployment-identity` の `github_oidc_provider_arn`）。push の role の信頼条件の Principal になる。"
  type        = string

  validation {
    condition     = can(regex("^arn:aws[a-z-]*:iam::[0-9]{12}:oidc-provider/token\\.actions\\.githubusercontent\\.com$", var.github_oidc_provider_arn))
    error_message = "github_oidc_provider_arn は GitHub Actions の OIDC provider の ARN にしてください。"
  }
}

variable "registry_manager_role_arn" {
  description = "このユースケースを apply する role の ARN（workload root の apply の role）。repository の暗号化の鍵に、ECR 経由の grant の作成をこの role に限って許す。"
  type        = string

  validation {
    condition     = can(regex("^arn:aws[a-z-]*:iam::[0-9]{12}:role/[A-Za-z0-9+=,.@_/-]+$", var.registry_manager_role_arn))
    error_message = "registry_manager_role_arn は IAM role の ARN にしてください。"
  }
}
