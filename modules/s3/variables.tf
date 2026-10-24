variable "name" {
  description = "Bucket 名の前半。module が `-<account_id>-<region>-an` を付けて完全な名前にする。"
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9]([a-z0-9-]*[a-z0-9])?$", var.name))
    error_message = "name は英小文字・数字・ハイフンだけで、先頭と末尾は英小文字か数字にしてください。"
  }

  # S3 が予約している接頭辞。接尾辞の禁止は、module が付ける `-an` で終わるので当たらない。
  validation {
    condition     = !anytrue([for p in ["xn--", "sthree-", "amzn-s3-demo-"] : startswith(var.name, p)])
    error_message = "name を S3 が予約している接頭辞（xn--、sthree-、amzn-s3-demo-）で始めないでください。"
  }
}

variable "t4_unused" {
  type        = string
  description = "T4"
  default     = ""
}
