variable "admin_account_id" {
  description = "委任管理者にするメンバーアカウントの ID（security のアカウント）。管理アカウントは指定できない（Organizations の RegisterDelegatedAdministrator が拒む）。"
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^[0-9]{12}$", var.admin_account_id))
    error_message = "admin_account_id は12桁の数字にしてください。"
  }
}

variable "regions" {
  description = "GuardDuty と Inspector の委任管理者を指定するリージョンの集合（repository-wide/ADR-0209）。"
  type        = set(string)
  nullable    = false

  validation {
    condition     = length(var.regions) > 0
    error_message = "regions は空にできません。"
  }
}
