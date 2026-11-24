## KICS による Terraform の検査（ADR-0501 決定6）
##
## 例外はファイルの先頭の `# kics-scan disable=<query_id>` で持つ（ADR-0501 決定13）。閾値はここが渡し、判定の実体は
## scripts/iac-scan（結果は tmp/kics/out）。docker を呼ぶのでホストで走る（.makefiles/README.md）。
KICS_FAIL_ON := critical,high,medium,low,info

.PHONY: kics ## Terraform の誤設定を KICS で検査（所見があれば落ちる）
kics:
	@$(call RUN_SCRIPT_HOST,iac-scan,kics -base "$(TF_BASE)" -fail-on $(KICS_FAIL_ON) $(TF_ROOTS))
