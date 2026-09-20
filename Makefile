.DEFAULT_GOAL := help

# 実行基盤と道具の版
include .makefiles/runner.mk
include .makefiles/versions.mk

# GitHub関連
include .makefiles/github/operation/release-branch.mk
include .makefiles/github/operation/release-tag.mk
include .makefiles/github/setting/github.mk
include .makefiles/github/setting/branch-ruleset.mk
include .makefiles/github/setting/label-setting.mk
include .makefiles/github/base-branch.mk
include .makefiles/github/lint.mk
include .makefiles/github/commitlint.mk
include .makefiles/github/pin.mk
include .makefiles/github/egress.mk
include .makefiles/github/workflows.mk
# Go言語関連
include .makefiles/go/fmt.mk
include .makefiles/go/golangci-lint.mk
include .makefiles/go/test.mk
# Markdown関連
include .makefiles/markdown/lint.mk
# セキュリティ関連
include .makefiles/security/trivy.mk
include .makefiles/security/gitleaks.mk
include .makefiles/security/go-cooldown.mk
include .makefiles/security/tool-cooldown.mk
include .makefiles/security/zizmor.mk
# Docker関連
include .makefiles/docker/lint.mk
include .makefiles/docker/pin.mk

# GitHub関連
include .makefiles/github/operation/setup-repository.mk

.PHONY: help
help:
	@grep -hE '^\.PHONY: [a-zA-Z0-9_-]+ ##' $(MAKEFILE_LIST) | sed 's/^\.PHONY: //' | awk -F' ## ' '{printf "  \033[36m%-24s\033[0m %s\n", $$1, $$2}' | sort
