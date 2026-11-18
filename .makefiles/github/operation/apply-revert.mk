## apply の失敗を戻す revert のブランチ（.github/workflows/README.md の「apply の失敗の revert」）
##
## 戻すかと、戻す範囲と、revert を置く commit（失敗した merge が運んだ線の commit）は scripts/apply-revert が決め、
## その範囲を revert した木が最後に成功した commit の木と一致することも確かめる（ci-revert-plan）。
## ここの push の入口は判定を持たず、決めた commit を同じ順に revert して push する。

# revert する commit を持つリポジトリ。テストが一時のリポジトリを渡す。
APPLY_REVERT_REPO ?= .
APPLY_REVERT_BOT := GIT_AUTHOR_NAME='github-actions[bot]' GIT_AUTHOR_EMAIL='41898282+github-actions[bot]@users.noreply.github.com' \
	GIT_COMMITTER_NAME='github-actions[bot]' GIT_COMMITTER_EMAIL='41898282+github-actions[bot]@users.noreply.github.com'
# 引数は RUN_SCRIPT_HOST の sh が環境変数から展開する。環境変数は revert-plan の job が渡す（README.md の「apply の失敗の revert」）。
APPLY_REVERT_PLAN_ARGS = plan -repo "$$GITHUB_REPOSITORY" -line "$$GITHUB_REF_NAME" -default "$$DEFAULT_BRANCH" -head "$$GITHUB_SHA" \
	-run "$$GITHUB_RUN_ID" -results "$$APPLY_REVERT_RESULTS" -workflow "$$APPLY_REVERT_WORKFLOW" \
	-branches .github/settings/branches.json -server "$$GITHUB_SERVER_URL" -output "$$GITHUB_OUTPUT"

# REVERT_HEAD・REVERT_COMMITS・REVERT_BRANCH を環境変数で受ける。push は force しない（README.md の「apply の失敗の revert」）。
# $(1)=revert の前に置く代入（作者） $(2)=push の前に置く代入（資格情報）
define APPLY_REVERT_PUSH
for v in REVERT_HEAD REVERT_COMMITS REVERT_BRANCH; do test -n "$${!v:-}" || { echo "❌ $$v がありません" >&2; exit 1; }; done; \
w=$$(mktemp -d); trap 'git -C "$(APPLY_REVERT_REPO)" worktree remove --force "$$w" >/dev/null 2>&1 || :' EXIT; \
git -C "$(APPLY_REVERT_REPO)" worktree add --quiet --detach "$$w" "$$REVERT_HEAD"; \
for c in $$REVERT_COMMITS; do $(1) git -C "$$w" revert -m 1 --no-edit "$$c"; done; \
$(2) git -C "$$w" push --quiet origin "HEAD:refs/heads/$$REVERT_BRANCH"; \
echo "✅ $$REVERT_BRANCH を push しました"
endef

.PHONY: ci-revert-plan ## [CI] apply の失敗を戻す revert のブランチを用意するかと、その範囲を決める
ci-revert-plan:
	@$(call RUN_SCRIPT_HOST,apply-revert,$(APPLY_REVERT_PLAN_ARGS))

# token は argv に置かない。GIT_CONFIG_* の環境変数で push の1回だけに渡す。
.PHONY: ci-revert-push ## [CI] ci-revert-plan が決めた範囲を revert し、GH_TOKEN で push する
ci-revert-push: SHELL := /bin/bash
ci-revert-push:
	@set -eu -o pipefail; test -n "$${GH_TOKEN:-}" || { echo "❌ GH_TOKEN がありません" >&2; exit 1; }; \
	auth=$$(printf 'x-access-token:%s' "$$GH_TOKEN" | base64 | tr -d '\n'); echo "::add-mask::$$auth"; \
	$(call APPLY_REVERT_PUSH,$(APPLY_REVERT_BOT),GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=http.https://github.com/.extraheader GIT_CONFIG_VALUE_0="AUTHORIZATION: basic $$auth")

.PHONY: apply-revert ## CI が push できなかった revert のブランチを、手元の資格情報で push する（通知のコマンドをそのまま実行する）
apply-revert: SHELL := /bin/bash
apply-revert:
	@set -eu -o pipefail; git -C "$(APPLY_REVERT_REPO)" fetch --quiet origin; \
	$(call APPLY_REVERT_PUSH,,)
