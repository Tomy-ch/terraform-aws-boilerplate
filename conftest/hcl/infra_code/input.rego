# インフラ内コードの検出の入力の検査（ADR-0103 決定11）。
#
# 入力は scripts/hcl-input が走査の root の下の通常のファイルすべてから作る JSON で、形と、独立の経路の数
# （find の一覧と行頭の数え）との一致を、ほかの規則より先に見る。崩れた入力の上では、それを読む規則を
# 評価しない（doc が undefined になる）。
package infra_code

import data.lib.hcl_input as hcl

supported_suffixes := [".tf", ".tftest.hcl"]

# Terraform が構成として読むが、検出が解釈しない入力。
unsupported_suffixes := [".tf.json", ".tftest.json", ".tfquery.hcl"]

# 走査の root に置いてよい、構成ファイルでないファイル。ここが除外の唯一の宣言である。
excluded_suffixes := [".md"]

excluded_names := {".terraform.lock.hcl"}

tf_blocks := {
	"terraform", "provider", "variable", "output", "locals", "module",
	"resource", "data", "moved", "import", "removed", "check", "ephemeral", "action",
}

tftest_blocks := {
	"run", "variables", "provider", "mock_provider",
	"override_resource", "override_data", "override_module", "test",
}

doc := input if hcl.input_ok

deny contains msg if {
	some e in hcl.input_errors
	msg := sprintf("IC005 [入力の形] %s → make infra-code-gate から起動する。hcl-input の出力と conftest/hcl/lib/hcl_input.rego の読む形を揃える", [e])
}

deny contains msg if {
	some f in doc.files
	not has_suffix(f.path, supported_suffixes)
	not has_suffix(f.path, unsupported_suffixes)
	not excluded(f.path)
	msg := sprintf("IC001 [対象外の入力] %s は Terraform の構成ファイルではありません → 走査の root から外すか、conftest/hcl/infra_code/input.rego の除外に足す", [f.path])
}

deny contains msg if {
	some f in doc.files
	has_suffix(f.path, unsupported_suffixes)
	msg := sprintf("IC002 [未対応の入力] %s は検出が解釈しない形式です → HCL（.tf / .tftest.hcl）で書く", [f.path])
}

deny contains msg if {
	some b in doc.blocks
	hcl.is_tf(b.file)
	not b.type in tf_blocks
	msg := sprintf("IC003 [未知のブロック] %s のブロック %s を解釈できません", [b.file, b.type])
}

deny contains msg if {
	some b in doc.blocks
	hcl.is_tftest(b.file)
	not b.type in tftest_blocks
	msg := sprintf("IC003 [未知のブロック] %s のブロック %s を解釈できません", [b.file, b.type])
}

excluded(path) if has_suffix(path, excluded_suffixes)

excluded(path) if base_name(path) in excluded_names

base_name(path) := parts[count(parts) - 1] if parts := split(path, "/")

has_suffix(s, suffixes) if {
	some suffix in suffixes
	endswith(s, suffix)
}
