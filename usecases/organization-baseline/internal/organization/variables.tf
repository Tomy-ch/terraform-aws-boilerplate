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

variable "bootstrapping" {
  description = "立ち上げ中のメンバーアカウントの env。ここに無いアカウントには、OrganizationAccountAccessRole の使用を拒む SCP を付ける。"
  type        = set(string)
}

variable "delegated_admin_regions" {
  description = "GuardDuty・Inspector・Security Hub の委任管理者を指定するリージョンの集合（repository-wide/ADR-0209）。"
  type        = set(string)
  nullable    = false

  validation {
    condition     = length(var.delegated_admin_regions) > 0
    error_message = "delegated_admin_regions は空にできません。"
  }
}
