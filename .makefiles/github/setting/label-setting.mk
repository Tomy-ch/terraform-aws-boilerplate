## GHのラベルを操作する
##
## 判定と GitHub への書き込みは scripts/repo-settings が持つ（宛先と資格情報は repo-settings.mk の REPO_SETTINGS_REMOTE）。
.PHONY: create-default-labels ## .github/settings/labels.json に基づいてラベルを作成
.PHONY: delete-all-labels ## すべてのラベルを削除（既存ラベル含む）

delete-all-labels:
	@$(call REPO_SETTINGS_REMOTE,delete-labels)

create-default-labels:
	@$(call REPO_SETTINGS_REMOTE,create-labels)
