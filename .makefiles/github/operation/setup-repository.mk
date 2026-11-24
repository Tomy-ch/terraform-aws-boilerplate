## リポジトリの初期化
.PHONY: setup-repo ## リポジトリの初期化

# git / gh の手順は scripts/repo-setup が持つ（なぜシェルに置かないかは scripts/README.md の repo-setup 行）。
# 個別の make ターゲットの連鎖は、ここが呼ぶ順を持つ。
# ホストの認証情報を使うため、ツールランナーは経由しない。
REPO_SETUP = $(call RUN_SCRIPT_HOST,repo-setup,$(1))

setup-repo:
	@$(call REPO_SETUP,preflight)

	@echo "🔧 ghコマンドのログインを開始します..."
	@$(MAKE) gh-login
	@echo "✅ ghコマンドのログインが完了しました。"

	@$(call REPO_SETUP,bootstrap)

	@echo "🔧 ワークフローの有効化を開始します..."
	@$(MAKE) enable-workflows
	@echo "✅ ワークフローの有効化を終了します。"

	@echo "🔧 ラベルの初期化を開始します..."
	@$(MAKE) delete-all-labels
	@$(MAKE) create-default-labels
	@echo "✅ ラベルの初期化を終了します。"

	@$(call REPO_SETUP,prune-release-notes)

	@echo "🔧 ルールセットと environment の適用を開始します..."
	@$(MAKE) apply-branch-protection
	@$(MAKE) apply-environments
	@echo "✅ ルールセットと environment の適用を終了します。"

	@git remote remove upstream || true

	@echo "🔧 Private Vulnerability Reporting の適用を開始します..."
	@$(MAKE) apply-vulnerability-reporting || { echo "❌ Private Vulnerability Reporting を適用できませんでした。ほかの初期化は済んでいます。直したら make apply-vulnerability-reporting で適用し直してください。"; exit 1; }
	@echo "✅ Private Vulnerability Reporting の適用を終了します。"

	@echo "✅ 初期化が完了しました"
