## 綴りの検査
##
## 設定は .typos.toml。対象の除外と、誤検知として受け入れた語の理由はそこが持つ。
## canary・走査の範囲の突き合わせ・本走査の順と判定は scripts/typos-gate が持つ。

.PHONY: typos ## 綴りの誤りが無いことを検査
typos:
	@$(call RUN_SCRIPT,typos-gate,check -work tmp/typos-gate -config .typos.toml)
