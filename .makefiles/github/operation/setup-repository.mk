## リポジトリの初期化
.PHONY: setup-repo ## リポジトリの初期化

# git / gh の手順（タグの作り直し・ブランチ整備・デフォルトブランチ移動・リリースノート整理）は
# scripts/repo-setup（テスト付き）が持つ。取り消しの効かない操作ばかりで、分岐を実地で確かめようと
# するとリポジトリを本当に壊すしかないため、手順をシェルに置かない。
# ラベル・ルールセット・ワークフローの初期化は個別の make ターゲットなので、連鎖はここに残す。
# ホストの認証情報を使うためツールランナーは経由しない。
REPO_SETUP := go -C scripts run ./repo-setup

setup-repo:
	@if [ -n "$(DRY_RUN)" ]; then echo "❌ setup-repo は DRY_RUN 未対応です（ローカル/リモートを破壊的に変更します）。DRY_RUN を外して実行してください。"; exit 1; fi
	@$(REPO_SETUP) preflight

	@echo "🔧 ghコマンドのログインを開始します..."
	@$(MAKE) gh-login
	@echo "✅ ghコマンドのログインが完了しました。"

	@$(REPO_SETUP) bootstrap

	@echo "🔧 ワークフローの有効化を開始します..."
	@$(MAKE) enable-workflows
	@echo "✅ ワークフローの有効化を終了します。"

	@echo "🔧 ラベルの初期化を開始します..."
	@$(MAKE) delete-all-labels
	@$(MAKE) create-default-labels
	@echo "✅ ラベルの初期化を終了します。"

	@$(REPO_SETUP) prune-release-notes

	@echo "🔧 ルールセットの適用を開始します..."
	@$(MAKE) apply-branch-protection
	@echo "✅ ルールセットの適用を終了します。"

	@git remote remove upstream || true
	@echo "✅ Initialization complete. Default branch: production"
