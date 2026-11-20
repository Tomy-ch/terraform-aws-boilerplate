variable "name" {
  description = "名前の先頭（`<project>-<env>`）。alarm の名前と KSK の名前（ハイフンを除いたもの）に使う。"
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^[a-z0-9-]{1,100}$", var.name)) && length(replace(var.name, "-", "")) >= 3
    error_message = "name は英小文字・数字・- だけの 1-100 文字で、ハイフンを除いて 3 文字以上にしてください（KSK の名前の下限）。"
  }
}

variable "zone_name" {
  description = "Hosted Zone の名前（末尾のドットを持たない小文字の FQDN）。"
  type        = string
  nullable    = false
}

variable "query_log_group_arn" {
  description = "クエリログの Log Group の ARN（us-east-1。末尾の :* を持たない）。"
  type        = string
  nullable    = false
}

variable "dnssec_key_arn" {
  description = "KSK に使う KMS の鍵の ARN（us-east-1、ECC_NIST_P256、SIGN_VERIFY）。"
  type        = string
  nullable    = false
}

variable "notify_topic_arn" {
  description = "DNSSEC の alarm を通知する SNS の Topic の ARN（同じアカウントの us-east-1）。"
  type        = string
  nullable    = false
}

variable "delegations" {
  description = "この zone から委任する子の zone。key は子の名前の先頭のラベル、値は子の zone の name server と DS。"
  type = map(object({
    name_servers = list(string)
    ds_record    = optional(string)
  }))
  nullable = false
}
