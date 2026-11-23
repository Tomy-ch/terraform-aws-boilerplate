variable "name_prefix" {
  description = "資源の名前の先頭（`<project>-<env>`）。"
  type        = string
  nullable    = false
}

variable "regions" {
  description = "設定を置くリージョンの集合。このアカウントで有効なリージョンをすべて含まなければならない。"
  type        = set(string)
  nullable    = false

  validation {
    condition     = length(var.regions) > 0
    error_message = "regions は空にできません。"
  }
}
