## bullfrog の step の対（本体と再試行）と通信制限を SSOT から生成・検証するコマンド群
.PHONY: egress-apply ## .github/egress.toml を各 job の先頭の bullfrog の step の対へ反映
.PHONY: egress-check ## bullfrog の step の対が SSOT と生成の形どおりか検証（書き換えなし・CI/hook用）

egress-apply:
	@$(call RUN_SCRIPT,egress,apply)

egress-check:
	@$(call RUN_SCRIPT,egress,check)
