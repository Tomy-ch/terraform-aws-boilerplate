## GitHub Actions の定義に対する検査
##
## 4つの道具が別々の問いに答える。束ねると、片方が消えたときに気付けない。
##
##   actionlint              workflow の構文・式・run: のシェル。走査は .github/workflows だけ
##   actions-shellcheck      composite action の run:。actionlint は action 定義を渡すと
##                           workflow として解釈して構文エラーで落ちるため、ここが埋める
##   shell-lint              ファイルとして置かれた *.sh
##   required-check-lint     保護設定が required とした context の報告 job の実在と、起動条件
##   zizmor                  template injection。${{ }} はシェルがパースする**前**に置換されるため、
##                           補間された式は文字列ではなくコードになる。shellcheck 系には構造的に見えない

.PHONY: actions-lint ## GitHub Actions の定義を検査（actionlint + composite + シェル）
actions-lint: actionlint actions-shellcheck shell-lint required-check-lint pr-comment-secret-lint actions-cutoff-lint pr-comment-fence-lint actions-mise-pin-lint

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
