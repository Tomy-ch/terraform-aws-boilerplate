## ホストで実行する道具の導入。対象と根拠は HOST_TOOLS の直上が持つ。
##
## `mise install` は mise.toml の全エントリを入れる。ここが入れるのはその部分集合である。
##
## **道具名をここへ書くことは、mise.toml に対する2つ目の写しである。** 版は写さないが、名前は
## 写す。`mise install <宣言に無い名前>` は `@latest` へ落ちるため、写しがずれた日に pin を
## 外れた版が黙って入る。そうならないよう、宣言を引いてから明示的な版で install する。

.PHONY: host-tools-install ## ホストで実行する道具を mise.toml の版で導入

# terraform は ADR-0503 決定4 の表の「ホスト」行である。**aws-cli は同表に行が無い** —— 要るのは
# ADR-0602 決定4 の bootstrap 段1 で、コンテナへ入れない判断はこの宣言が持つ（ADR ではない）。
# 検査ツール（tflint 等）は決定4 の表の第2行に当たり、既定の経路がコンテナなので含めない。
HOST_TOOLS := aqua:hashicorp/terraform aqua:aws/aws-cli

host-tools-install:
	@command -v mise >/dev/null 2>&1 \
		|| { echo "❌ mise が PATH にありません。README「使い方」を見てください。"; exit 1; }
	@command -v jq >/dev/null 2>&1 || { echo "❌ jq が PATH にありません。"; exit 1; }
	@declared="$$(mise ls --current --json)" || exit 1; \
	for tool in $(HOST_TOOLS); do \
		version="$$(printf '%s' "$$declared" | jq -r --arg t "$$tool" '.[$$t][0].requested_version // empty')"; \
		origin="$$(printf '%s' "$$declared" | jq -r --arg t "$$tool" '.[$$t][0].source.path // empty')"; \
		if [ -z "$$version" ]; then \
			echo "❌ $$tool の宣言が mise.toml にありません。HOST_TOOLS か mise.toml のどちらかが古くなっています。"; \
			exit 1; \
		fi; \
		if [ "$$origin" != "$(CURDIR)/mise.toml" ]; then \
			echo "❌ $$tool の版が $$origin から来ています。このリポジトリの mise.toml ではありません。"; \
			exit 1; \
		fi; \
		echo "🔧 $$tool@$$version"; \
		mise install "$$tool@$$version" || exit 1; \
	done
	@installed="$$(mise ls --current --json)" || exit 1; \
	for tool in $(HOST_TOOLS); do \
		printf '%s' "$$installed" | jq -e --arg t "$$tool" '.[$$t][0].installed == true' >/dev/null \
			|| { echo "❌ $$tool が導入されていません。"; exit 1; }; \
		echo "✅ $$tool $$(printf '%s' "$$installed" | jq -r --arg t "$$tool" '.[$$t][0].version')"; \
	done
