## apply の失敗を戻す revert のブランチ（.github/workflows/README.md の「apply の失敗の revert」）
##
## 戻すかと、戻す範囲と、revert を置く commit（失敗した merge が運んだ線の commit）は scripts/apply-revert が決め、
## その範囲を revert した木が最後に成功した commit の木と一致することも確かめる（ci-revert-plan）。
## ここの push の入口は判定を持たず、決めた commit を同じ順に1回の git revert へ渡して push する。
## 書き込みの job は道具を導入しないので、push の入口は git と gh を決まった順に呼ぶだけにする（ADR-0702 決定25）。

APPLY_REVERT_BOT := GIT_AUTHOR_NAME='github-actions[bot]' GIT_AUTHOR_EMAIL='41898282+github-actions[bot]@users.noreply.github.com' \
	GIT_COMMITTER_NAME='github-actions[bot]' GIT_COMMITTER_EMAIL='41898282+github-actions[bot]@users.noreply.github.com'
# 失敗した回の worktree は残り、次の回の最初の2行が登録ごと外す。同じ checkout で2つを同時に走らせない。
APPLY_REVERT_WORKTREE := $(CURDIR)/tmp/apply-revert

# REVERT_HEAD・REVERT_COMMITS・REVERT_BRANCH を環境変数で受ける。push は force しない（README.md の「apply の失敗の revert」）。
# $(1)=revert の前に置く代入（作者） $(2)=push の git の -c（資格情報）
define APPLY_REVERT_PUSH
@test -n "$$REVERT_HEAD" || { echo "❌ REVERT_HEAD がありません" >&2; exit 1; }
@test -n "$$REVERT_COMMITS" || { echo "❌ REVERT_COMMITS がありません" >&2; exit 1; }
@test -n "$$REVERT_BRANCH" || { echo "❌ REVERT_BRANCH がありません" >&2; exit 1; }
@rm -rf "$(APPLY_REVERT_WORKTREE)"
@git worktree prune
@git worktree add --quiet --detach "$(APPLY_REVERT_WORKTREE)" "$$REVERT_HEAD"
@$(1) git -C "$(APPLY_REVERT_WORKTREE)" revert -m 1 --no-edit -- $$REVERT_COMMITS
@git -C "$(APPLY_REVERT_WORKTREE)" $(2) push --quiet origin "HEAD:refs/heads/$$REVERT_BRANCH"
@git worktree remove --force "$(APPLY_REVERT_WORKTREE)"
@echo "✅ $$REVERT_BRANCH を push しました"
endef

.PHONY: ci-revert-plan ## [CI] apply の失敗を戻す revert のブランチを用意するかと、その範囲を決める
ci-revert-plan:
	@$(call RUN_SCRIPT_HOST,apply-revert,plan -branches .github/settings/branches.json)

# token は gh の credential helper が GH_TOKEN から push の1回だけに渡し、argv にも git の設定にも置かない。
.PHONY: ci-revert-push ## [CI] ci-revert-plan が決めた範囲を revert し、GH_TOKEN で push する
ci-revert-push:
	@test -n "$$GH_TOKEN" || { echo "❌ GH_TOKEN がありません" >&2; exit 1; }
	$(call APPLY_REVERT_PUSH,$(APPLY_REVERT_BOT),-c credential.helper= -c 'credential.helper=!gh auth git-credential')

.PHONY: apply-revert ## CI が push できなかった revert のブランチを、手元の資格情報で push する（通知のコマンドをそのまま実行する）
apply-revert:
	@git fetch --quiet origin
	$(call APPLY_REVERT_PUSH,,)
