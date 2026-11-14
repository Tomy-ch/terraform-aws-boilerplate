## govulncheck による検査
##
## text 形式は所見があると終了コード 3 を返す。報告のみとするため 3 だけを成功として扱い、それ以外の
## 非0（パッケージの読み込みの失敗、脆弱性 DB に届かない）は落とす。json 形式は所見があっても 0 を返す。
## 機械可読な出力を取るときは `make -s` で呼ぶ（理由は trivy.mk 冒頭）。
GOVULNCHECK_FORMAT ?= text

.PHONY: govulncheck ## 運用機構（scripts/）の Go の依存のうち、呼び出し経路が届く既知脆弱性を検査（報告のみ）
govulncheck:
	@$(GO_TOOL) sh -c 'rc=0; govulncheck -C scripts -format $(GOVULNCHECK_FORMAT) ./... || rc=$$?; [ "$$rc" -eq 0 ] || [ "$$rc" -eq 3 ]'
