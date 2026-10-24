## サンプルの除去（scripts/sample-remove）
##
## 何をサンプルとし、どう印を置くかは scripts/README.md の sample-remove 行が持つ。
## このファイルは除去の対象であり、除去の後には残らない。

.PHONY: sample-remove-check ## サンプルの印と manifest を検査（書き換えなし・CI 用）
sample-remove-check:
	@$(call RUN_SCRIPT,sample-remove,check)

.PHONY: setup-remove-samples ## サンプル（SonarCloud・dependency-review を含む）を取り除く。DRY_RUN=1 で計画だけを表示
setup-remove-samples:
	@$(call RUN_SCRIPT,sample-remove,apply$(if $(DRY_RUN), --dry-run,))
