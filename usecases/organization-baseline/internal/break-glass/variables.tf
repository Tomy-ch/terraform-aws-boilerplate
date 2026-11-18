variable "role_name" {
  description = "各アカウントの break-glass の role の名前。"
  type        = string
}

variable "path" {
  description = "break-glass の IAM user・group・role の path。`/` で始まり `/` で終わる。"
  type        = string
}
