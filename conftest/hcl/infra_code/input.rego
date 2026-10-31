# インフラ内コードの検出の入力の検査（ADR-0103 決定11）。ファイル1つずつ評価する。
#
# 解釈できない入力を、検出が何も見ないまま緑で終える形を塞ぐ。ここの規則はどのファイルにも必ず評価され、
# infra-code-gate count はそれを手がかりに、Conftest が読んだファイルを数える。
package infra_code_input

import data.lib.hcl

supported_suffixes := [".tf", ".tftest.hcl"]

# Terraform が構成として読むが、検出が解釈しない入力。
unsupported_suffixes := [".tf.json", ".tftest.json", ".tfquery.hcl"]

tf_blocks := {
  "terraform", "provider", "variable", "output", "locals", "module",
  "resource", "data", "moved", "import", "removed", "check", "ephemeral", "action",
}

tftest_blocks := {
  "run", "variables", "provider", "mock_provider",
  "override_resource", "override_data", "override_module", "test",
}

name := data.conftest.file.name

# ファイル名が渡らなければ、下の規則はどれも評価されずに成功として数えられる。
deny contains "IC001 [対象外の入力] Conftest がファイル名を渡していません" if not data.conftest.file.name

deny contains msg if {
  not has_suffix(name, supported_suffixes)
  not has_suffix(name, unsupported_suffixes)
  msg := sprintf("IC001 [対象外の入力] %s は Terraform の構成ファイルではありません → 走査の root から外すか、infra-code.mk の除外に足す", [name])
}

deny contains msg if {
  has_suffix(name, unsupported_suffixes)
  msg := sprintf("IC002 [未対応の入力] %s は検出が解釈しない形式です → HCL（.tf / .tftest.hcl）で書く", [name])
}

deny contains msg if {
  hcl.is_tf(name)
  some key, _ in input
  not key in tf_blocks
  msg := sprintf("IC003 [未知のブロック] %s のブロック %s を解釈できません", [name, key])
}

deny contains msg if {
  hcl.is_tftest(name)
  some key, _ in input
  not key in tftest_blocks
  msg := sprintf("IC003 [未知のブロック] %s のブロック %s を解釈できません", [name, key])
}

has_suffix(s, suffixes) if {
  some suffix in suffixes
  endswith(s, suffix)
}
