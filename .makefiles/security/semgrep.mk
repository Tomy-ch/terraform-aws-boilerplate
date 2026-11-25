## Semgrep CE による Terraform の検査（ADR-0501 決定6・19）
##
## ルールは semgrep-rules の commit で固定し、リポジトリに置かずに実行時に取得する（Semgrep Rules
## License v1.0 はルールの配布を許さない）。取得はホストで行う。
##
## 例外は該当行の直前の `# nosemgrep: <rule-id>` で持つ（ADR-0501 決定13）。rule-id は `terraform.` から始まる。
## 判定の実体は scripts/iac-scan（結果は tmp/semgrep/out）。docker を呼ぶのでホストで走る（.makefiles/README.md）。
SEMGREP_RULES_COMMIT := 16b9e6d45f10649f6a531ec4838ed9924372c3bc
# 上の commit の terraform/ の *.yaml / *.yml を名前順に並べた sha256 の一覧の sha256（形は scripts/iac-scan の
# stageRules）。commit を上げるときは同時に上げる。
SEMGREP_RULES_SHA256 := ebdaecff1b7b4c9f0419488afe3358ea99bb43baca37af889aef963b92157849
SEMGREP_RULES_TGZ := tmp/semgrep-rules-$(SEMGREP_RULES_COMMIT).tar.gz

$(SEMGREP_RULES_TGZ):
	@mkdir -p tmp
	@curl -fsSL --proto '=https' --proto-redir '=https' --max-filesize 8M "https://codeload.github.com/semgrep/semgrep-rules/tar.gz/$(SEMGREP_RULES_COMMIT)" -o "$@.part"
	@mv "$@.part" "$@"

.PHONY: semgrep ## Terraform の誤設定を Semgrep CE で検査（所見があれば落ちる）
semgrep: $(SEMGREP_RULES_TGZ)
	@$(call RUN_SCRIPT_HOST,iac-scan,semgrep -base "$(TF_BASE)" -rules $(SEMGREP_RULES_TGZ) -rules-sha256 $(SEMGREP_RULES_SHA256) $(TF_ROOTS))
