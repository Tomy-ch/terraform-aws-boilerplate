## commit message の検査
##
## 規約の正本は docs/adr/0022-commit-and-branch-naming.md。ここが持つのは起動だけである。
## 機械が課すのは type の enum と type / subject の空チェックだけで、文体と長さはレビューで見る。

.PHONY: commitlint ## 直近の commit message を検査
commitlint:
	@git log -1 --format=%B | $(NODE_TOOL) commitlint

.PHONY: commitlint-range ## Pull Request がベースの上へ積んだ範囲を検査
## 範囲の起点は origin/<base> の**現在の先端**とする。Pull Request のイベントが示すベースの断面を
## 使うと、ベースがその後動いた場合に**自分が作成していない古い commit**が範囲へ残り、持っていない
## 履歴で落ちる。
##
## 完全な履歴の取得が要る。浅い取得では範囲の両端が解決できない。
##
## 検査対象が0件のときは成功として返さず exit 2 とする。「ゲートが外れた」と「合格した」を
## 区別できなくしないため、また違反（1）と検査不全（2）を呼び出し側が分けられるようにするため。
COMMITLINT_BASE ?= $(shell git symbolic-ref --quiet --short refs/remotes/origin/HEAD 2>/dev/null || echo origin/main)
COMMITLINT_HEAD ?= HEAD

commitlint-range:
	@set -eu; \
	count="$$(git rev-list --count $(COMMITLINT_BASE)..$(COMMITLINT_HEAD) 2>/dev/null || echo 0)"; \
	if [ "$$count" -le 0 ]; then \
		echo "❌ 検査対象の commit が 0 件です。参照解決が壊れている可能性があります" >&2; \
		echo "   base=$(COMMITLINT_BASE) head=$(COMMITLINT_HEAD)" >&2; \
		exit 2; \
	fi; \
	echo "検査対象: $$count commit ($(COMMITLINT_BASE)..$(COMMITLINT_HEAD))"; \
	$(GO_TOOL) true >/dev/null 2>&1 || true; \
	$(NODE_TOOL) commitlint --from "$(COMMITLINT_BASE)" --to "$(COMMITLINT_HEAD)"
