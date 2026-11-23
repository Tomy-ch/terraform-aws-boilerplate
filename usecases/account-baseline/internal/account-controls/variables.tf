variable "security_contact" {
  description = "Security Hub の Account.1 が求める、セキュリティの代替の連絡先。"
  type = object({
    name          = string
    title         = string
    email_address = string
    phone_number  = string
  })
  nullable  = false
  sensitive = true
}

variable "manage_password_policy" {
  description = "このアカウントの IAM の password policy を置くか。管理アカウントでは organization-baseline の break-glass が置くので false にする。"
  type        = bool
  nullable    = false
}
