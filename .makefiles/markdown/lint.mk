## Markdown の体裁の検査
##
## 除外は .markdownlint-cli2.yaml の ignores: に集約する（エディタ拡張と同じ結果にするため）。

MD_GLOBS := "**/*.md"

.PHONY: md-lint ## Markdown の体裁を検査
md-lint:
	@$(NODE_TOOL) markdownlint-cli2 $(MD_GLOBS)

.PHONY: md-fix ## Markdown の体裁を自動修正
md-fix:
	@$(NODE_TOOL) markdownlint-cli2 --fix $(MD_GLOBS)

.PHONY: adr-lint ## ADR の構造を検査（ADR-0001 が定めた検証方法）
adr-lint:
	@$(call RUN_SCRIPT,adr-lint,)
