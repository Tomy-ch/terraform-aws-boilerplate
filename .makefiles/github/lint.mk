## GitHub Actions の定義に対する検査
##
## 道具ごとに別々の問いに答える。束ねると、片方が消えたときに気付けない。
##
##   actionlint          走査は .github/workflows だけ
##   actions-shellcheck  composite action の run:。actionlint は action 定義を渡すと
##                       workflow として解釈して構文エラーで落ちるため、ここが埋める
##   zizmor              zizmor.mk が持つ（lint に束ねない理由もそこにある）

.PHONY: actions-lint ## GitHub Actions の定義を検査（actionlint + composite + シェル）
actions-lint: actionlint actions-shellcheck shell-lint required-check-lint pr-comment-secret-lint actions-cutoff-lint pr-comment-fence-lint actions-mise-pin-lint tf-job-lint oidc-job-lint

.PHONY: actionlint ## workflow の構文と run: のシェルを検査
actionlint:
	@$(GO_TOOL) actionlint

.PHONY: actions-shellcheck ## composite action の run: を検査
actions-shellcheck:
	@$(call RUN_SCRIPT,actions-shellcheck,)

.PHONY: shell-lint ## ファイルとして置かれたシェルスクリプトを検査
shell-lint:
	@$(call RUN_SCRIPT,shell-lint,)

.PHONY: required-check-lint ## 保護設定が required とした context の報告 job と起動条件を検査
required-check-lint:
	@$(call RUN_SCRIPT,required-check-lint,)

.PHONY: tf-job-lint ## credentials を持たずに terraform を走らせる job の権限・checkout・uses: を検査
tf-job-lint:
	@$(call RUN_SCRIPT,tf-job-lint,)

.PHONY: oidc-job-lint ## OIDC で role を assume する job の environment・役割・起動条件を検査
oidc-job-lint:
	@$(call RUN_SCRIPT,oidc-job-lint,)

.PHONY: pr-comment-secret-lint ## PR へコメントする job に GITHUB_TOKEN 以外の secret を渡していないか検査
pr-comment-secret-lint:
	@$(call RUN_SCRIPT,pr-comment-secret-lint,)

.PHONY: actions-cutoff-lint ## 打ち切られた job でも PR に結果が残ることを検査
actions-cutoff-lint:
	@$(call RUN_SCRIPT,actions-cutoff-lint,)

.PHONY: pr-comment-fence-lint ## PR コメント本文が Markdown を壊さないことを検査
pr-comment-fence-lint:
	@$(call RUN_SCRIPT,pr-comment-fence-lint,)

.PHONY: actions-mise-pin-lint ## mise 導入 action の版・digest・キャッシュキーの三点一致を検査
actions-mise-pin-lint:
	@$(call RUN_SCRIPT,actions-mise-pin-lint,)
