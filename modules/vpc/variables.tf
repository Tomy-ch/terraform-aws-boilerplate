variable "name" {
  description = "資源名の前半。Flow Logs の配送 role の名前を <name>-vpc-flow-logs にする。"
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^[a-z0-9]([a-z0-9-]*[a-z0-9])?$", var.name)) && length(var.name) <= 50
    error_message = "name は小文字の英数字と - だけで、先頭と末尾を英数字にし、50 文字以下にしてください。"
  }
}

variable "cidr" {
  description = "VPC の IPv4 の CIDR。RFC 1918 の範囲の /16 から /28 で、172.17.0.0/16 と重ならないもの。"
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^[0-9]{1,3}(\\.[0-9]{1,3}){3}/[0-9]{1,2}$", var.cidr)) && try(cidrsubnet(var.cidr, 0, 0) == var.cidr, false)
    error_message = "cidr は a.b.c.d/n の形の IPv4 の CIDR で、ホスト部を 0 にしてください。"
  }

  validation {
    condition     = try(tonumber(split("/", var.cidr)[1]) >= 16 && tonumber(split("/", var.cidr)[1]) <= 28, false)
    error_message = "cidr の大きさは /16 から /28 にしてください。"
  }

  validation {
    condition     = try(anytrue([for r in ["10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"] : cidrsubnet("${split("/", var.cidr)[0]}/${split("/", r)[1]}", 0, 0) == r]), false)
    error_message = "cidr は RFC 1918 の範囲（10.0.0.0/8、172.16.0.0/12、192.168.0.0/16）の中にしてください。"
  }

  validation {
    condition     = try(cidrsubnet("${split("/", var.cidr)[0]}/16", 0, 0) != "172.17.0.0/16", false)
    error_message = "cidr を 172.17.0.0/16 と重ねないでください（AWS のサービスが使います）。"
  }
}

variable "flow_logs_log_group_arn" {
  description = "Flow Logs の宛先の Log Group の ARN。modules/log-group の出力 log_group_arn を渡す。VPC と同じ partition・リージョン・アカウントのものにする。"
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^arn:aws[a-z-]*:logs:[a-z0-9-]+:[0-9]{12}:log-group:[.\\-_/#A-Za-z0-9]{1,512}$", var.flow_logs_log_group_arn))
    error_message = "flow_logs_log_group_arn は Log Group の ARN（arn:<partition>:logs:<region>:<account>:log-group:<name>）にし、末尾に :* を付けないでください。"
  }
}
