## リポジトリの保護設定（ruleset、environment、Private Vulnerability Reporting）の宣言・適用・突合
##
## 判定は scripts/repo-settings が持つ。apply-* は GitHub の実態を書き換えるので、管理者の資格情報を
## 持つ人が実行する（ADR-0603 決定30）。突合（drift）は読み取りの権限だけで動く（ADR-0603 決定6）。
.PHONY: repo-settings-apply ## branches.json と checks.json から ruleset の宣言（rulesets/ と、在れば branch-protection.json）の生成する区間と workflow の起動条件（apply の workflow と印 branch-set の行）を生成
.PHONY: repo-settings-check ## 保護設定の宣言が整合しているか検証（書き換えなし・CI/hook用）
.PHONY: repo-settings-drift ## 保護設定の宣言と GitHub の実態を突き合わせる（読み取りのみ）
.PHONY: apply-branch-protection ## ruleset の宣言（rulesets/ と、在れば branch-protection.json）を対象リポジトリの ruleset へ適用（宣言に無い ruleset は消さない）
.PHONY: apply-environments ## environments.json を対象リポジトリの environment へ適用
.PHONY: apply-vulnerability-reporting ## private-vulnerability-reporting.json を対象リポジトリの Private Vulnerability Reporting へ適用
.PHONY: deploy-source-check ## [CI] デプロイブランチへ入る Pull Request の head を検査する（ADR-0603 決定31）
.PHONY: create-deploy-branch ## DEPLOY_ENV のデプロイブランチを、デフォルトブランチの先頭から作る（管理者が bootstrap で1回）
.PHONY: deploy-branch-exists ## DEPLOY_ENV のデプロイブランチが在ることを確かめる（読み取りのみ。bootstrap の段4 の apply の前）

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

apply-vulnerability-reporting:
	@$(call REPO_SETTINGS_REMOTE,sync -only vulnerability-reporting)

# 引数は RUN_SCRIPT_HOST の sh が環境変数から展開する。
deploy-source-check:
	@$(call RUN_SCRIPT_HOST,repo-settings,source -base "$${BASE_REF:-}" -base-sha "$${BASE_SHA:-}" -head-sha "$${HEAD_SHA:-}" -default "$${DEFAULT_BRANCH:-}")

# DEPLOY_ENV は人が渡す値なので、deploy-source-check と同じく RUN_SCRIPT_HOST の sh が環境変数から展開する。
create-deploy-branch:
	@$(call REPO_SETTINGS_REMOTE,create-deploy-branch -env "$${DEPLOY_ENV:-}")

deploy-branch-exists:
	@$(call REPO_SETTINGS_REMOTE,deploy-branch-exists -env "$${DEPLOY_ENV:-}")
