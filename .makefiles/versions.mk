## 版のずれ検出。存在理由と検出条件は scripts/versions/main.go の package doc
## （ADR-0501 決定19）を見よ。

.PHONY: versions-apply ## mise.toml の版を Dockerfile / go.mod の写しへ反映
.PHONY: versions-check ## 写しが mise.toml 通りか検証（書き換えなし・CI/hook用）

versions-apply:
	@$(call RUN_SCRIPT,versions,apply)

versions-check:
	@$(call RUN_SCRIPT,versions,check)
