## リリースタグの設定とリリースノートの設定コマンド
# 手順の実体は scripts/release。取り消しの効かない操作を含み、分岐を実地で確かめるには
# 本当にリリースするしかないため、手順と中止条件をシェルに置かない。
# git / gh はホストの認証情報を使うのでツールランナーは経由しない。

.PHONY: tag-patch ## リリースタグ(vX.Y.Z+1)を作成
.PHONY: tag-minor ## リリースタグ(vX.Y+1.0)を作成
.PHONY: tag-major ## リリースタグ(vX+1.0.0)を作成
.PHONY: tag-version ## 指定の版でリリースタグを作成 (VERSION=vX.Y.Z。タグが無い初回はこちら)

RELEASE_TOOL = $(call RUN_SCRIPT_HOST,release,$(1))

tag-patch:
	@$(call RELEASE_TOOL,tag -bump patch)

tag-minor:
	@$(call RELEASE_TOOL,tag -bump minor)

tag-major:
	@$(call RELEASE_TOOL,tag -bump major)

# 環境に残った VERSION で取り消しの効かないタグを打たないため、コマンドラインの VERSION だけを渡す。
tag-version: export RELEASE_TAG_VERSION = $(if $(filter command line,$(origin VERSION)),$(VERSION))
tag-version:
	@$(call RELEASE_TOOL,tag -version "$$RELEASE_TAG_VERSION")
