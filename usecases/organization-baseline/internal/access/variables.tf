variable "project" {
  description = "AWS 資源の名前の先頭に置くプロジェクトの token。permission set の名前 `<project>-<キー>`、主体の parameter の名前、`organization_admin` の Deny が名指す管理アカウントの CI の role と policy の名前を組む。"
  type        = string
}

variable "partition" {
  description = "AWS managed policy と IAM の ARN を組む partition。"
  type        = string
}

variable "management_account_id" {
  description = "管理アカウントの ID。USER の割当の対象と、`organization_admin` の Deny の Resource を組む。"
  type        = string
}

variable "member_account_ids" {
  description = "メンバーアカウントの env から ID への map。GROUP の割当の対象を env から引く。"
  type        = map(string)
}

variable "management_assignments" {
  description = "管理アカウントへの USER の割当。`user` は parameter の `users` の label。"
  type        = list(object({ permission_set = string, user = string }))
  nullable    = false

  validation {
    condition     = length(var.management_assignments) > 0
    error_message = "management_assignments に割当を1つ以上置いてください。"
  }
}

variable "member_assignments" {
  description = "メンバーアカウントへの GROUP の割当。`account` はメンバーアカウントの env、`group` は parameter の `groups` の label。"
  type        = list(object({ account = string, permission_set = string, group = string }))
  nullable    = false
}

variable "bootstrap_bucket_arns" {
  description = "`organization_admin` に書き込みを拒ませる管理アカウントの Bucket（state・plan の出力・apply の出力）の ARN。"
  type        = list(string)
}

variable "state_key_arn" {
  description = "`organization_admin` に変更と暗号操作を拒ませる管理アカウントの state の鍵の ARN。"
  type        = string
}
