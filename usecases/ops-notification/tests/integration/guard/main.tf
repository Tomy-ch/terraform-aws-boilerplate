# 複製を作る前に project の形を検査する。検査の失敗は後の run を止める（assert の失敗は止めない）。
variable "project" {
  description = "複製の project。"
  type        = string
  nullable    = false

  # 常駐の構成と名前が衝突しないよう、実行の識別子の接頭辞 it を求める。
  validation {
    condition     = can(regex("^it[a-z0-9]{6,35}$", var.project))
    error_message = "project は it で始め、実行の識別子（英小文字・数字の6-35文字）を続けてください。"
  }
}
