## commit message の検査
##
## 規約の正本は commitlint.config.js。ここが持つのは起動だけである。
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
## 起点は CI が渡す。渡されない手元では origin/HEAD を引き、それも無ければ空にする。
## **既定値を固定のブランチ名にしない** —— 存在しない ref を既定にすると、範囲が 0 件になった
## 理由が「積んだ commit が無い」なのか「ref が解決できない」なのか区別できなくなる。
## 区別は下の番兵が exit 2 で行うが、そもそも誤った既定で踏ませない。
COMMITLINT_FROM ?= $(if $(GITHUB_BASE_REF),origin/$(GITHUB_BASE_REF),$(shell git symbolic-ref --quiet --short refs/remotes/origin/HEAD 2>/dev/null))
COMMITLINT_TO ?= HEAD

commitlint-range:
	@set -eu; \
	test -n "$(COMMITLINT_FROM)" || { \
		echo "❌ 比較対象が解決できません。GITHUB_BASE_REF を渡すか COMMITLINT_FROM を指定してください" >&2; \
		exit 2; \
	}; \
	count="$$(git rev-list --count $(COMMITLINT_FROM)..$(COMMITLINT_TO) 2>/dev/null || echo 0)"; \
	if [ "$$count" -le 0 ]; then \
		echo "❌ 検査対象の commit が 0 件です。参照解決が壊れている可能性があります" >&2; \
		echo "   base=$(COMMITLINT_FROM) head=$(COMMITLINT_TO)" >&2; \
		exit 2; \
	fi; \
	echo "検査対象: $$count commit ($(COMMITLINT_FROM)..$(COMMITLINT_TO))"; \
	$(GO_TOOL) true >/dev/null 2>&1 || true; \
	$(NODE_TOOL) commitlint --from "$(COMMITLINT_FROM)" --to "$(COMMITLINT_TO)"
