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

variable "state_key_alias_name" {
  description = "state の鍵の alias の名前（`alias/<name>`）。apply-identity の alias の作成と、plan の role の鍵の読み取りをこの alias に絞る。"
  type        = string
}

variable "plan_output_bucket_arn" {
  description = "plan の出力の Bucket の ARN。plan の role の書き込みと、Bucket の設定の Resource をこの Bucket に絞る。"
  type        = string
}
