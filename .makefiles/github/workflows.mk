## GitHub Actions ワークフローの状態操作
.PHONY: enable-workflows ## disabled_fork 状態のワークフローを一括で有効化します

# テンプレート由来のリポジトリでは、GitHub が全ワークフローを disabled_fork 状態で
# 作成する。この状態のワークフローは push にも schedule にも反応せず、しかも Actions タブを
# 開くまで気付きにくい。セキュリティ系のスキャナは「動いているつもりで一度も動いていない」が
# 最悪の失敗なので、リポジトリ初期化の一環でまとめて有効化する。
#
# 冪等：既に有効なものには触らないため、再実行しても何もしない。
enable-workflows:
	@$(call REPO_SETTINGS_REMOTE,enable-workflows)
