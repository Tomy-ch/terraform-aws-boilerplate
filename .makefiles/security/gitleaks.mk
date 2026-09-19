## 秘密の混入の検査
##
## 走査対象は commit と履歴であり、作業ツリーのファイルではない。Trivy の secret scanner は
## 無効化してあり、この層の権威は gitleaks が持つ。検出時は通さない。
##
## 混入を検出した場合の第一手は当該資格情報の**失効**である。履歴からの除去ではない。
## push 済みであれば、履歴を書き換えても漏洩の事実は取り消せない。

.PHONY: secret-scan ## push 予定の commit 範囲に秘密が無いことを検査
secret-scan:
	@$(GO_TOOL) gitleaks git --redact --no-banner --log-opts="$(COMMITLINT_FROM)..HEAD"

.PHONY: secret-scan-history ## 全履歴を走査（検出ルールの更新で過去が対象になるため定期実行する）
secret-scan-history:
	@$(GO_TOOL) gitleaks git --redact --no-banner
