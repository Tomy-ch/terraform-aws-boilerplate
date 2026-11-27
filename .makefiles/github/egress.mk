## bullfrog の step の三つ組（本体・回復・再試行）と通信制限を SSOT から生成・検証するコマンド群
.PHONY: egress-apply ## .github/egress.toml を各 job の先頭の bullfrog の step の三つ組へ反映
.PHONY: egress-check ## bullfrog の step の三つ組が SSOT と生成の形どおりで、status の関数を持つ step の if: が bullfrog の成功で始まるか検証（書き換えなし・CI/hook用）

egress-apply:
	@$(call RUN_SCRIPT,egress,apply)

egress-check:
	@$(call RUN_SCRIPT,egress,check)
