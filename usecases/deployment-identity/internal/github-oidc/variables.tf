variable "name_prefix" {
  description = "role の名前の前半。role 名は `<name_prefix>-ci-<役割>` になる。"
  type        = string
}

variable "env" {
  description = "GitHub の environment 名の前半。environment 名は `<env>-<役割>` になる。"
  type        = string
}

variable "github" {
  description = "role を assume してよい GitHub のリポジトリの名前と、変わらない数値の ID。"
  type = object({
    owner         = string
    owner_id      = string
    repository    = string
    repository_id = string
  })
}

variable "state_bucket_arn" {
  description = "state Bucket の ARN。S3 の Action の Resource をこの Bucket と、その state の key に絞る。"
  type        = string
}

variable "state_key_arn" {
  description = "state の鍵の ARN。plan と apply-identity の鍵の Action と、apply の role の鍵の Deny の Resource をこの鍵に絞る。"
  type        = string
}

variable "plan_output_bucket_arn" {
  description = "plan の出力の Bucket の ARN。plan の role の書き込みと、Bucket の設定の Resource をこの Bucket に絞る。"
  type        = string
}

variable "access_log_group_arn" {
  description = "state Bucket と plan の出力の Bucket の S3 サーバーアクセスログを受ける Log Group の ARN（`:*` を除いた形）。Logs の Action の Resource をこの Log Group に絞る。"
  type        = string
}

variable "access_log_destination_arn" {
  description = "S3 サーバーアクセスログの delivery destination の ARN。delivery destination の Action の Resource をこれに絞る。"
  type        = string
}

variable "access_log_source_arns" {
  description = "2つの Bucket の delivery source の ARN。delivery source の Action の Resource をこれに絞る。"
  type        = list(string)
}
