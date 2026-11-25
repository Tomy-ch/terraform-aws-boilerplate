## 秘密の混入の検査（gitleaks）
##
## 走査対象は commit と履歴であり、作業ツリーのファイルではない。
##
## 起点は sh -c の位置引数で渡す。RUN_SCRIPT の $(2) は単一引用符の内側に入る。
.PHONY: secret-scan ## push 予定の commit 範囲に秘密が無いことを検査
secret-scan:
	@$(call RUN_SCRIPT,secret-scan,"$$@") secret-scan gitleaks-range -from "$(COMMITLINT_FROM)"

.PHONY: secret-scan-history ## 全履歴を走査（検出ルールの更新で過去が対象になるため定期実行する）
secret-scan-history:
	@$(call RUN_SCRIPT,secret-scan,gitleaks-history)

.PHONY: secret-suppression-lint ## 秘密の走査の抑止が、理由つきの fingerprint・path 単位であることを検査
secret-suppression-lint:
	@set -eu; mkdir -p tmp; trap 'rm -f tmp/secret-suppression-lint.tracked tmp/secret-suppression-lint.commits' EXIT; \
	git -c core.quotepath=false ls-files > tmp/secret-suppression-lint.tracked; \
	git rev-list --all > tmp/secret-suppression-lint.commits; \
	$(call RUN_SCRIPT,secret-suppression-lint,-tracked tmp/secret-suppression-lint.tracked -commits tmp/secret-suppression-lint.commits)
