variable "repository_filter" {
  description = "ENHANCED の継続スキャンを当てる repository の名前の WILDCARD（`<project>-<env>/*`）。"
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9]+(-[a-z0-9]+)*/\\*$", var.repository_filter))
    error_message = "repository_filter は <接頭辞>/* の形にしてください。"
  }
}
