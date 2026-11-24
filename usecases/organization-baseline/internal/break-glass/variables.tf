variable "role_name" {
  description = "各アカウントの break-glass の role の名前。"
  type        = string
}

variable "path" {
  description = "break-glass の IAM user・group・role の path。`/` で始まり `/` で終わる。"
  type        = string

  validation {
    condition     = startswith(var.path, "/") && endswith(var.path, "/")
    error_message = "path は / で始まり / で終わる値にしてください。ARN は path と名前をつないで組みます。"
  }
}

variable "organization_root_id" {
  description = "メンバーの break-glass の role を配る StackSet の展開先の、組織の Root の ID。"
  type        = string
}
