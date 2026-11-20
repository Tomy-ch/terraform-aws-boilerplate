variable "name" {
  description = "名前の先頭（`<project>-<env>`）。鍵の alias は `<name>-dns-query-logs`、resource policy の名前も同じになる。"
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^[a-z0-9-]{1,200}$", var.name))
    error_message = "name は英小文字・数字・- だけで、1-200 文字にしてください。"
  }
}

variable "zone_name" {
  description = "クエリログを送る Hosted Zone の名前。Log Group は `/aws/route53/<zone_name>` になる。"
  type        = string
  nullable    = false
}

variable "retention_days" {
  description = "クエリログを残す日数。modules/log-group の許す値。"
  type        = number
  nullable    = false
}

variable "deletion_protection" {
  description = "クエリログの Log Group の削除を拒むか。"
  type        = bool
  nullable    = false
}
