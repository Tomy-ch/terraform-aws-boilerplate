## リポジトリの保護設定（ruleset と environment）の宣言・適用・突合
##
## 判定は scripts/repo-settings が持つ。宣言は .github/settings/ の branches.json /
## environments.json / branch-protection.json。apply-* は GitHub の実態を書き換えるので、
## 管理者の資格情報を持つ人が実行する。突合（drift）は読み取りの権限だけで動く（ADR-0603 決定6）。
.PHONY: repo-settings-apply ## branches.json から branch-protection.json の include を生成
.PHONY: repo-settings-check ## 保護設定の宣言が整合しているか検証（書き換えなし・CI/hook用）
.PHONY: repo-settings-drift ## 保護設定の宣言と GitHub の実態を突き合わせる（読み取りのみ）
.PHONY: apply-branch-protection ## branch-protection.json を対象リポジトリの ruleset へ適用
.PHONY: apply-environments ## environments.json を対象リポジトリの environment へ適用

REPO_SETTINGS_REPO ?= $(or $(GITHUB_REPOSITORY),$(shell gh repo view --json nameWithOwner -q .nameWithOwner 2>/dev/null))

# GitHub へ問い合わせる target は、ホストの gh の認証情報を使うためツールランナーを経由しない
# （ツールランナーは GITHUB_TOKEN をコンテナへ渡さない）。CI は GITHUB_TOKEN を自分で置く。
REPO_SETTINGS_REMOTE = GITHUB_TOKEN="$${GITHUB_TOKEN:-$$(gh auth token 2>/dev/null || true)}" $(call RUN_SCRIPT_HOST,repo-settings,$(1) -repo=$(REPO_SETTINGS_REPO))

repo-settings-apply:
	@$(call RUN_SCRIPT,repo-settings,apply)

repo-settings-check:
	@$(call RUN_SCRIPT,repo-settings,check)

repo-settings-drift:
	@$(call REPO_SETTINGS_REMOTE,drift)

apply-branch-protection:
	@$(call REPO_SETTINGS_REMOTE,sync -only ruleset)

apply-environments:
	@$(call REPO_SETTINGS_REMOTE,sync -only environments)
