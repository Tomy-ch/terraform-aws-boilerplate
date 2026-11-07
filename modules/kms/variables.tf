variable "name" {
  description = "alias の名前の後半。module が `alias/` を付ける。"
  type        = string

  validation {
    condition     = can(regex("^[a-zA-Z0-9/_-]{1,250}$", var.name))
    error_message = "name は英数字・/・_・- だけで、1 文字以上 250 文字以下にしてください（alias/<name> が 256 文字まで）。"
  }

  validation {
    condition     = !startswith(var.name, "aws/")
    error_message = "name を aws/ で始めないでください。AWS 管理鍵の alias に予約されています。"
  }
}

variable "users" {
  description = "鍵の暗号操作を許す相手。roles は経由するサービスごとの IAM role、services.logs は CloudWatch Logs の Log Group の名前。合わせて1件以上。"
  type = object({
    roles = optional(list(object({
      arn = string
      via = string
    })), [])
    services = optional(object({
      logs = optional(list(string), [])
    }), {})
  })
  nullable = false

  # 誰も暗号操作できない鍵は作る意味が無い。管理の委任は暗号操作を含まない。
  validation {
    condition     = length(var.users.roles) + length(var.users.services.logs) > 0
    error_message = "users に roles か services.logs を1件以上渡してください。"
  }

  validation {
    condition     = alltrue([for r in var.users.roles : can(regex("^arn:aws[a-z-]*:iam::[0-9]{12}:role/[A-Za-z0-9+=,.@_/-]+$", r.arn))])
    error_message = "users.roles[*].arn は IAM role の ARN にしてください。user・root・ワイルドカードは受けません。"
  }

  validation {
    condition     = alltrue([for r in var.users.roles : contains(["s3"], r.via)])
    error_message = "users.roles[*].via は s3 にしてください。"
  }

  validation {
    condition     = length(distinct(var.users.roles)) == length(var.users.roles)
    error_message = "users.roles に同じ role と via の組を重ねないでください。"
  }

  # Key Policy の ArnEquals は * と ? をワイルドカードとして読むので、名前の文字をここで閉じる。
  validation {
    condition     = alltrue([for n in var.users.services.logs : can(regex("^[.\\-_/#A-Za-z0-9]{1,512}$", n))])
    error_message = "users.services.logs は Log Group の名前（ARN ではない）にしてください。"
  }

  validation {
    condition     = length(distinct(var.users.services.logs)) == length(var.users.services.logs)
    error_message = "users.services.logs に同じ Log Group を重ねないでください。"
  }
}
