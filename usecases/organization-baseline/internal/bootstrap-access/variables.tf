variable "project" {
  description = "メンバーの state-backend と deployment-identity が名前の先頭に置くプロジェクトの token。読み取りの権限の Resource を限る。"
  type        = string
}

variable "role_name" {
  description = "各メンバーアカウントに置く立ち上げの読み取りの role の名前。StackSet の名前にもなる。"
  type        = string
}

variable "path" {
  description = "立ち上げの読み取りの role の IAM の path。`/` で始まり `/` で終わる。"
  type        = string

  validation {
    condition     = startswith(var.path, "/") && endswith(var.path, "/")
    error_message = "path は / で始まり / で終わる値にしてください。ARN は path と名前をつないで組みます。"
  }
}

variable "trusted_principal_arn" {
  description = "立ち上げの読み取りの role を引き受けてよい、管理アカウントの role の ARN（管理アカウントの identity root の plan の role）。"
  type        = string
}

variable "organization_root_id" {
  description = "立ち上げの読み取りの role を配る StackSet の展開先の、組織の Root の ID。"
  type        = string
}
