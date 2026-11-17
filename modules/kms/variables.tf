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
  description = "鍵の暗号操作を許す相手。roles は経由するサービスごとの IAM role と、その利用の範囲（access が read-write なら復号と鍵の生成、read なら objects に名指した S3 の object の復号だけ）。services.logs は CloudWatch Logs の Log Group の名前。合わせて1件以上。"
  type = object({
    roles = optional(list(object({
      arn     = string
      via     = string
      access  = string
      objects = optional(list(string), [])
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
    condition     = alltrue([for r in var.users.roles : contains(["read", "read-write"], r.access)])
    error_message = "users.roles[*].access は read か read-write にしてください。"
  }

  # read の文は objects の暗号化コンテキストで限る。限る object の無い read は、read-write と同じ範囲の復号になる。
  validation {
    condition     = alltrue([for r in var.users.roles : r.access == "read" ? length(r.objects) > 0 : length(r.objects) == 0])
    error_message = "users.roles[*].objects は、access が read のとき1件以上、read-write のとき空にしてください。"
  }

  # Key Policy の StringEquals は * と ? をワイルドカードとして読まないが、IAM の Resource と突き合わせる値なので閉じる。
  validation {
    condition = alltrue(flatten([for r in var.users.roles : [
      for o in r.objects : can(regex("^arn:aws[a-z-]*:s3:::[a-z0-9][a-z0-9.-]*[a-z0-9]/[^*?]+$", o))
    ]]))
    error_message = "users.roles[*].objects は S3 の object の ARN（arn:<partition>:s3:::<bucket>/<key>）にしてください。* と ? は受けません。"
  }

  validation {
    condition     = alltrue([for r in var.users.roles : length(distinct(r.objects)) == length(r.objects)])
    error_message = "users.roles[*].objects に同じ object を重ねないでください。"
  }

  validation {
    condition     = length(distinct([for r in var.users.roles : "${r.via} ${r.arn}"])) == length(var.users.roles)
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
