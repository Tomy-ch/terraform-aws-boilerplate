variable "project" {
  description = "parameter の名前 `/<project>/organization-baseline/account-ids` と共有の名前 `<project>-account-ids` の先頭。"
  type        = string
}

variable "account_ids" {
  description = "メンバーアカウントの env から ID への map。"
  type        = map(string)
}

variable "organization_arn" {
  description = "共有の宛先の組織の ARN。"
  type        = string
}
