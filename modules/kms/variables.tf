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
  description = "鍵の暗号操作を許す相手。roles は経由するサービスごとの IAM role と、その利用の範囲（access が read-write なら復号と鍵の生成、read なら objects に名指した S3 の object の復号だけ）。via が sns の role は topics に名指した SNS の Topic へ publish するときだけ鍵を使える。via が ecr の role は access を grant とし、ECR が repository の暗号化に使う grant の作成と廃止だけを許す。services.logs は CloudWatch Logs の Log Group の名前。services.sns は SNS の Topic の名前と、その Topic へ publish する AWS のサービス（budgets・cloudwatch・costalerts・events）。services.cloudtrail は鍵で暗号化して配信する CloudTrail の trail の ARN、services.config は鍵で暗号化して配信する AWS Config のアカウントの ID。trail と Config のアカウントは鍵の外のアカウントでもよい。合わせて1件以上。"
  type = object({
    roles = optional(list(object({
      arn     = string
      via     = string
      access  = string
      objects = optional(list(string), [])
      topics  = optional(list(string), [])
    })), [])
    services = optional(object({
      logs = optional(list(string), [])
      sns = optional(list(object({
        topic      = string
        publishers = set(string)
      })), [])
      cloudtrail = optional(list(string), [])
      config     = optional(list(string), [])
    }), {})
  })
  nullable = false

  # 誰も暗号操作できない鍵は作る意味が無い。管理の委任は暗号操作を含まない。
  validation {
    condition     = length(var.users.roles) + length(var.users.services.logs) + length(var.users.services.sns) + length(var.users.services.cloudtrail) + length(var.users.services.config) > 0
    error_message = "users に roles・services.logs・services.sns・services.cloudtrail・services.config のいずれかを1件以上渡してください。"
  }

  validation {
    condition     = alltrue([for r in var.users.roles : can(regex("^arn:aws[a-z-]*:iam::[0-9]{12}:role/[A-Za-z0-9+=,.@_/-]+$", r.arn))])
    error_message = "users.roles[*].arn は IAM role の ARN にしてください。user・root・ワイルドカードは受けません。"
  }

  validation {
    condition     = alltrue([for r in var.users.roles : contains(["s3", "sns", "ecr"], r.via)])
    error_message = "users.roles[*].via は s3・sns・ecr のどれかにしてください。"
  }

  validation {
    condition     = alltrue([for r in var.users.roles : contains(r.via == "ecr" ? ["grant"] : ["read", "read-write"], r.access)])
    error_message = "users.roles[*].access は、via が ecr のとき grant、それ以外のとき read か read-write にしてください。"
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

  # SNS の文は publish に要る復号と鍵の生成を、topics の Topic の暗号化コンテキストに限って許す。
  validation {
    condition     = alltrue([for r in var.users.roles : r.via == "sns" ? r.access == "read-write" && length(r.objects) == 0 && length(r.topics) > 0 : length(r.topics) == 0])
    error_message = "users.roles[*] は、via が sns なら access を read-write・objects を空・topics を1件以上に、via が s3 なら topics を空にしてください。"
  }

  validation {
    condition     = alltrue(flatten([for r in var.users.roles : [for t in r.topics : can(regex("^[A-Za-z0-9_-]{1,256}$", t))]]))
    error_message = "users.roles[*].topics は SNS の Topic の名前（ARN ではない。英数字・_・- の 1-256 文字。FIFO の Topic は受けません）にしてください。"
  }

  validation {
    condition     = alltrue([for r in var.users.roles : length(distinct(r.topics)) == length(r.topics)])
    error_message = "users.roles[*].topics に同じ Topic を重ねないでください。"
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

  validation {
    condition     = alltrue([for t in var.users.services.sns : can(regex("^[A-Za-z0-9_-]{1,256}$", t.topic))])
    error_message = "users.services.sns[*].topic は SNS の Topic の名前（ARN ではない。英数字・_・- の 1-256 文字。FIFO の Topic は受けません）にしてください。"
  }

  validation {
    condition     = length(distinct([for t in var.users.services.sns : t.topic])) == length(var.users.services.sns)
    error_message = "users.services.sns に同じ Topic を重ねないでください。"
  }

  validation {
    condition     = alltrue([for t in var.users.services.sns : t.publishers != null && length(t.publishers) > 0 && length(setsubtract(t.publishers, ["budgets", "cloudwatch", "costalerts", "events"])) == 0])
    error_message = "users.services.sns[*].publishers は budgets・cloudwatch・costalerts・events から1つ以上にしてください。"
  }

  # Key Policy の StringEquals は * と ? をワイルドカードとして読まないが、要求元を名指す値なので閉じる。
  validation {
    condition     = alltrue([for a in var.users.services.cloudtrail : can(regex("^arn:aws[a-z-]*:cloudtrail:[a-z0-9-]+:[0-9]{12}:trail/[A-Za-z0-9][A-Za-z0-9._-]{2,127}$", a))])
    error_message = "users.services.cloudtrail は CloudTrail の trail の ARN（arn:<partition>:cloudtrail:<region>:<account>:trail/<name>）にしてください。"
  }

  validation {
    condition     = length(distinct(var.users.services.cloudtrail)) == length(var.users.services.cloudtrail)
    error_message = "users.services.cloudtrail に同じ trail を重ねないでください。"
  }

  validation {
    condition     = alltrue([for a in var.users.services.config : can(regex("^[0-9]{12}$", a))])
    error_message = "users.services.config は12桁のアカウントの ID にしてください。"
  }

  validation {
    condition     = length(distinct(var.users.services.config)) == length(var.users.services.config)
    error_message = "users.services.config に同じアカウントを重ねないでください。"
  }
}

variable "region" {
  description = "鍵を置くリージョン。null なら provider のリージョン。provider と別のリージョンは、us-east-1 にしか置けない鍵の利用者（Route 53 のクエリログの Log Group）のための us-east-1 だけを受ける。"
  type        = string
  default     = null

  validation {
    condition     = var.region == null || var.region == "us-east-1"
    error_message = "region は null（provider のリージョン）か us-east-1 にしてください。"
  }
}
