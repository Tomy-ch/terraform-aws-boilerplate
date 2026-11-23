variable "name_prefix" {
  description = "recorder と delivery channel の名前（`<project>-<env>`）。"
  type        = string
  nullable    = false
}

variable "regions" {
  description = "recorder を置くリージョンの集合。provider のリージョン（home）を含まなければならない。"
  type        = set(string)
  nullable    = false

  validation {
    condition     = length(var.regions) > 0
    error_message = "regions は空にできません。"
  }
}

variable "delivery" {
  description = "Config の配信先。ログ保管のアカウントの audit-evidence の Bucket の名前と鍵の ARN。"
  type = object({
    bucket_name = string
    kms_key_arn = string
  })
  nullable = false
}

variable "retention_days" {
  description = "Config の構成の履歴を保持する日数。"
  type        = number
  nullable    = false
}
