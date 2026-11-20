variable "name" {
  description = "alias の名前の後半（`<project>-<env>-dns-dnssec`）。module が `alias/` を付ける。"
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^[a-z0-9-]{1,250}$", var.name))
    error_message = "name は英小文字・数字・- だけで、1-250 文字にしてください。"
  }
}

variable "zone_name" {
  description = "この鍵で署名する Hosted Zone の名前。鍵の説明にだけ使う。"
  type        = string
  nullable    = false
}
