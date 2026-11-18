variable "project" {
  description = "AWS 資源の名前の先頭に置くプロジェクトの token。メンバーの state Bucket・CI の role・break-glass の role の名前を組む。"
  type        = string
}

variable "break_glass_role_name" {
  description = "各アカウントの break-glass の role の名前。SCP がこの role を守り、state の書き込みの拒否から外す。"
  type        = string
}

variable "break_glass_role_path" {
  description = "break-glass の role の IAM の path。`/` で始まり `/` で終わる。"
  type        = string
}
