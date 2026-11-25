## 秘密の混入の検査（trufflehog）
##
## gitleaks と同じ範囲を走査する（ADR-0302 決定2・6）。
##
## 既定の出力は候補の値を平文で出す。CI は TRUFFLEHOG_FORMAT=json で受け、値を運ばない要約だけを
## ログとコメントへ出す（.github/workflows/secret-scan.yaml）。
TRUFFLEHOG_FORMAT ?= text

## 値は sh -c の位置引数で渡す。
.PHONY: trufflehog-scan ## push 予定の commit 範囲を trufflehog で走査
trufflehog-scan: export RANGE_FROM = $(call literal_value,COMMITLINT_FROM)
trufflehog-scan: export SCAN_FORMAT = $(call literal_value,TRUFFLEHOG_FORMAT)
trufflehog-scan:
	@$(call RUN_SCRIPT,secret-scan,"$$@") secret-scan trufflehog-range -from "$$RANGE_FROM" -format "$$SCAN_FORMAT"

.PHONY: trufflehog-history ## 全履歴を trufflehog で走査
trufflehog-history: export SCAN_FORMAT = $(call literal_value,TRUFFLEHOG_FORMAT)
trufflehog-history:
	@$(call RUN_SCRIPT,secret-scan,"$$@") secret-scan trufflehog-history -format "$$SCAN_FORMAT"

.PHONY: trufflehog-worktree ## commit 前の作業ツリーを trufflehog で走査
trufflehog-worktree:
	@$(call RUN_SCRIPT,secret-scan,"$$@") secret-scan trufflehog-worktree -format "$(TRUFFLEHOG_FORMAT)"
