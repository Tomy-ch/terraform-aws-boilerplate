## container image の digest 固定
##
## **ホストで実行する。** digest の解決に docker を呼ぶため、コンテナの中では成立しない
## （ツールランナーに docker は入っていない）。ADR-0503 決定4 の分類では、コンテナが
## 提供できない前提を持つ道具に当たる。

.PHONY: pin-images-resolve ## FROM / compose image の tag を digest へ解決し lockfile を更新
.PHONY: pin-images-apply ## lockfile を元に FROM / compose image を digest へ正規化
.PHONY: pin-images-check ## FROM / compose image が lockfile 通り固定済みか検証（書き換えなし・CI/hook用）

# supply-chain cooldown: N 日未満の新しすぎる digest は採用しない（0 で無効）
PIN_IMAGES_MIN_AGE_DAYS ?= 14

pin-images-resolve:
	@$(call RUN_SCRIPT_HOST,pin-images,resolve --min-age-days=$(PIN_IMAGES_MIN_AGE_DAYS))

pin-images-apply:
	@$(call RUN_SCRIPT_HOST,pin-images,apply)

pin-images-check:
	@$(call RUN_SCRIPT_HOST,pin-images,check)
