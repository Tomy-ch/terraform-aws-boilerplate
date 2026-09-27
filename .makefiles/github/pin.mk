## GitHub Actions の固定コマンド群 —— uses: の SHA と、runs-on: の runner label
.PHONY: pin-actions-resolve ## uses: の tag を SHA へ解決し lockfile を更新
.PHONY: pin-actions-apply ## lockfile を元に uses: を SHA へ固定
.PHONY: pin-actions-check ## uses: が lockfile 通り固定済みか検証（書き換えなし・CI/hook用）

# supply-chain quarantine: N 日未満の新しすぎるコミットは採用しない（0 で無効）
PIN_ACTIONS_MIN_AGE_DAYS ?= 14

pin-actions-resolve:
	@$(call RUN_SCRIPT,pin-actions,resolve --min-age-days=$(PIN_ACTIONS_MIN_AGE_DAYS))

pin-actions-apply:
	@$(call RUN_SCRIPT,pin-actions,apply)

pin-actions-check:
	@$(call RUN_SCRIPT,pin-actions,check)

.PHONY: pin-runners-apply ## .github/runners-pin.toml を各 workflow の runs-on へ反映
.PHONY: pin-runners-check ## runs-on が宣言通り固定済みか検証（書き換えなし・CI/hook用）

pin-runners-apply:
	@$(call RUN_SCRIPT,pin-runners,apply)

pin-runners-check:
	@$(call RUN_SCRIPT,pin-runners,check)
