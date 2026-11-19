variable "project" {
  description = "AWS 資源の名前の先頭に置くプロジェクトの token。VPC の Flow Logs の配送 role は `<project>-<env>-vpc-flow-logs` になる。"
  type        = string
  nullable    = false

  # ハイフンを許すと `<project>-<env>` の境目を名前から読めなくなる。42 は `<project>-sandbox` が modules/vpc の name の上限 50 に収まる長さ。
  validation {
    condition     = can(regex("^[a-z0-9]{1,42}$", var.project))
    error_message = "project は英小文字と数字だけで、42 文字以下にしてください。"
  }
}

variable "env" {
  description = "VPC を置くアカウントの env の token。workload を持つ dev・stg・prd・sandbox に限る。"
  type        = string
  nullable    = false

  validation {
    condition     = contains(["dev", "stg", "prd", "sandbox"], var.env)
    error_message = "env は dev / stg / prd / sandbox のいずれかにしてください。"
  }
}

variable "cidr" {
  description = "VPC の IPv4 の CIDR。RFC 1918 の範囲の /16 から /24 で、172.17.0.0/16 と重ならないもの。subnet はここから固定の割り付けで切り出す。"
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^[0-9]{1,3}(\\.[0-9]{1,3}){3}/[0-9]{1,2}$", var.cidr)) && try(cidrsubnet(var.cidr, 0, 0) == var.cidr, false)
    error_message = "cidr は a.b.c.d/n の形の IPv4 の CIDR で、ホスト部を 0 にしてください。"
  }

  validation {
    condition     = try(anytrue([for r in ["10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"] : cidrsubnet("${split("/", var.cidr)[0]}/${split("/", r)[1]}", 0, 0) == r]), false)
    error_message = "cidr は RFC 1918 の範囲（10.0.0.0/8、172.16.0.0/12、192.168.0.0/16）の中にしてください。"
  }

  validation {
    condition     = try(cidrsubnet("${split("/", var.cidr)[0]}/16", 0, 0) != "172.17.0.0/16", false)
    error_message = "cidr を 172.17.0.0/16 と重ねないでください（AWS のサービスが使います）。"
  }

  validation {
    condition     = try(tonumber(split("/", var.cidr)[1]) >= 16 && tonumber(split("/", var.cidr)[1]) <= 24, false)
    error_message = "cidr の大きさは /16 から /24 にしてください（subnet は VPC を 16 に分けるので、/24 で subnet が /28 になる）。"
  }
}

variable "availability_zone_ids" {
  description = "subnet を置く AZ の ID（例: apne1-az1）。2 個から 4 個。位置が subnet の CIDR を決めるので、AZ を足すときは末尾に足し、途中を消さない（途中を消すとそれより後ろの subnet が置換になる）。"
  type        = list(string)
  nullable    = false

  validation {
    condition     = length(var.availability_zone_ids) >= 2 && length(var.availability_zone_ids) <= 4
    error_message = "availability_zone_ids は 2 個から 4 個にしてください。"
  }

  validation {
    condition     = length(distinct(var.availability_zone_ids)) == length(var.availability_zone_ids)
    error_message = "availability_zone_ids に同じ AZ を重ねないでください。"
  }

  validation {
    condition     = alltrue([for id in var.availability_zone_ids : can(regex("^[a-z]{2,}[0-9]+-az[0-9]+$", id))])
    error_message = "availability_zone_ids は AZ の ID（例: apne1-az1）にしてください。AZ の名前（例: ap-northeast-1a）は受けません。"
  }
}

variable "flow_logs_retention_days" {
  description = "VPC Flow Logs を残す日数。365 以上の、CloudWatch Logs が受ける値に限る。"
  type        = number
  nullable    = false

  validation {
    condition     = contains([365, 400, 545, 731, 1096, 1827, 2192, 2557, 2922, 3288, 3653], var.flow_logs_retention_days)
    error_message = "flow_logs_retention_days は 365, 400, 545, 731, 1096, 1827, 2192, 2557, 2922, 3288, 3653 のいずれかにしてください。"
  }
}

variable "flow_logs_deletion_protection" {
  description = "VPC Flow Logs の Log Group の削除を拒むか。sandbox のように destroy する環境では false にする。"
  type        = bool
  nullable    = false
}
