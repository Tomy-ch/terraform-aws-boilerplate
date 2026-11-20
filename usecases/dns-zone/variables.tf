variable "project" {
  description = "AWS 資源の名前の先頭に置くプロジェクトの token。最も長い名前は alias `alias/<project>-sandbox-dns-query-logs`。"
  type        = string
  nullable    = false

  # ハイフンを許すと `<project>-<env>` の境目を名前から読めなくなる。
  validation {
    condition     = can(regex("^[a-z0-9]{1,42}$", var.project))
    error_message = "project は英小文字と数字だけで、42 文字以下にしてください。"
  }
}

variable "env" {
  description = "zone を置くアカウントの env の token。workload を持つ dev・stg・prd・sandbox に限る。prd の zone が apex_domain そのもので、ほかは `<env>.<apex_domain>` になる。"
  type        = string
  nullable    = false

  validation {
    condition     = contains(["dev", "stg", "prd", "sandbox"], var.env)
    error_message = "env は dev / stg / prd / sandbox のいずれかにしてください。"
  }
}

variable "apex_domain" {
  description = "登録したドメインの apex（例: example.com）。小文字で、末尾のドットを持たない。登録は人の手順（docs/setup/dns-zone.md）。"
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\\.)+[a-z]{2,63}$", var.apex_domain)) && length(var.apex_domain) <= 240
    error_message = "apex_domain は小文字の FQDN（例: example.com。末尾のドットと大文字を持たない）で、240 文字以下にしてください。"
  }
}

variable "delegations" {
  description = "prd の zone から dev・stg の zone へ委任する値。key は dev か stg、name_servers は子の zone の出力 name_servers（4つ）、ds_record は子の zone の出力 ds_record（DNSSEC の署名を親から信頼させるとき）。prd のほかは空の map を渡す。"
  type = map(object({
    name_servers = list(string)
    ds_record    = optional(string)
  }))
  nullable = false

  validation {
    condition     = length(setsubtract(keys(var.delegations), ["dev", "stg"])) == 0
    error_message = "delegations の key は dev か stg にしてください。"
  }

  validation {
    condition = alltrue([for d in values(var.delegations) :
      length(d.name_servers) == 4 && length(distinct(d.name_servers)) == 4 &&
      alltrue([for ns in d.name_servers : can(regex("^ns-[0-9]{1,4}\\.awsdns-[0-9]{2}\\.(com|net|org|co\\.uk)$", ns))])
    ])
    error_message = "delegations[*].name_servers は Route 53 の name server（ns-<n>.awsdns-<nn>.<com|net|org|co.uk>。末尾のドットを持たない）を重ねずに4つにしてください。"
  }

  # Route 53 の KSK は ECDSAP256SHA256（13）で、DS の digest は SHA-256（2）。
  validation {
    condition     = alltrue([for d in values(var.delegations) : d.ds_record == null || (can(regex("^[0-9]{1,5} 13 2 [0-9A-F]{64}$", d.ds_record)) && try(tonumber(split(" ", d.ds_record)[0]) <= 65535, false))])
    error_message = "delegations[*].ds_record は Route 53 の KSK の DS record（<0-65535 の key tag> 13 2 <64桁の大文字の16進>）か null にしてください。"
  }
}

variable "query_log_retention_days" {
  description = "公開 DNS のクエリログを残す日数。365 以上の、CloudWatch Logs が受ける値に限る。"
  type        = number
  nullable    = false

  validation {
    condition     = contains([365, 400, 545, 731, 1096, 1827, 2192, 2557, 2922, 3288, 3653], var.query_log_retention_days)
    error_message = "query_log_retention_days は 365, 400, 545, 731, 1096, 1827, 2192, 2557, 2922, 3288, 3653 のいずれかにしてください。"
  }
}

variable "query_log_deletion_protection" {
  description = "クエリログの Log Group の削除を拒むか。sandbox のように destroy する環境では false にする。"
  type        = bool
  nullable    = false
}

variable "notify_topic_arn" {
  description = "DNSSEC の alarm を通知する SNS の Topic の ARN。同じアカウントの us-east-1 の Topic にする（#421）。"
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^arn:aws[a-z-]*:sns:us-east-1:[0-9]{12}:[A-Za-z0-9_-]{1,256}$", var.notify_topic_arn))
    error_message = "notify_topic_arn は us-east-1 の SNS の Topic の ARN にしてください。FIFO の Topic は受けません。"
  }
}
