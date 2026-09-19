# Makefile
.DEFAULT_GOAL := help

# 依存されるファイル
# Docker関連
include .makefiles/runner.mk
# DB関連
# Go言語関連
# Node関連
# 負荷配分（重いターゲットが参照するため、それらより前に読む）

# 依存されないファイル
# DB関連
# Application関連
# GitHub関連
include .makefiles/github/operation/release-branch.mk
include .makefiles/github/operation/release-tag.mk
include .makefiles/github/setting/github.mk
include .makefiles/github/setting/branch-ruleset.mk
include .makefiles/github/setting/label-setting.mk
include .makefiles/github/base-branch.mk
include .makefiles/github/branches.mk
include .makefiles/github/lint.mk
include .makefiles/github/commitlint.mk
include .makefiles/github/pin.mk
include .makefiles/github/egress.mk
include .makefiles/github/workflows.mk
# Go言語関連
include .makefiles/go/fmt.mk
include .makefiles/go/golangci-lint.mk
include .makefiles/go/test.mk
# ドキュメント関連
# OpenAPI関連
# SQL関連
# Markdown関連
include .makefiles/markdown/lint.mk
# Node関連
# AI 開発フィードバック（Closed Loop）関連
# エージェント向けの静音実行
# Python関連
# Graphify関連
# セキュリティ関連
include .makefiles/security/trivy.mk
include .makefiles/security/gitleaks.mk
include .makefiles/security/go-cooldown.mk
include .makefiles/security/tool-cooldown.mk
include .makefiles/security/zizmor.mk
# Docker関連
include .makefiles/docker/lint.mk
include .makefiles/docker/pin.mk

# 一括実行系ファイル
# GitHub関連
include .makefiles/github/operation/setup-repository.mk
# DB関連
# 生成関連

.PHONY: help
help:
	@grep -hE '^\.PHONY: [a-zA-Z0-9_-]+ ##' $(MAKEFILE_LIST) | sed 's/^\.PHONY: //' | awk -F' ## ' '{printf "  \033[36m%-24s\033[0m %s\n", $$1, $$2}' | sort
