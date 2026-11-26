# TFLint の設定。同梱の terraform ruleset は既定（recommended）のまま使う。
#
# AWS ruleset の版は mise.toml が宣言し、mise が mise.lock の照合値で取る。`source` と `version` を書かない
# —— 書くと TFLint は `tflint --init` で取ることを求める。置き場を渡し、版が宣言と同じことを確かめるのは
# scripts/tf-gates。
plugin "aws" {
  enabled = true
}
