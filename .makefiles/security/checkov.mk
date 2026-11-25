## Checkov による Terraform の検査（ADR-0501 決定6）
##
## 例外は resource の中の `#checkov:skip=<ID>:<理由>` で持つ（ADR-0501 決定13）。判定の実体は scripts/iac-scan
## （結果は tmp/checkov/out）。docker を呼ぶのでホストで走る（.makefiles/README.md）。

.PHONY: checkov ## Terraform の誤設定を Checkov で検査（所見があれば落ちる）
checkov:
	@$(call RUN_SCRIPT_HOST,iac-scan,checkov -base "$(TF_BASE)" $(TF_ROOTS))
