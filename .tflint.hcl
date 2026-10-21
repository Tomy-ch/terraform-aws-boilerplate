# TFLint の設定。同梱の terraform ruleset は既定（recommended）のまま使う。
#
# AWS ruleset は `tflint --init` が GitHub Release から取得する。ここで固定した版の
# 供給網の窓の検査は scripts/README.md の tool-cooldown 行が持つ。
plugin "aws" {
  enabled = true
  version = "0.49.0"
  source  = "github.com/terraform-linters/tflint-ruleset-aws"
}
