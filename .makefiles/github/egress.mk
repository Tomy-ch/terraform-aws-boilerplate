## bullfrog の通信制限（with:）を SSOT から生成・検証するコマンド群
.PHONY: egress-apply ## .github/egress.toml を各 job の bullfrog の with: へ反映
.PHONY: egress-check ## bullfrog の with: が SSOT 通りか検証（書き換えなし・CI/hook用）

egress-apply:
	@$(call RUN_SCRIPT,egress,apply)

egress-check:
	@$(call RUN_SCRIPT,egress,check)
