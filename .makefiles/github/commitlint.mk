## commit message の検査
##
## 規約の正本は commitlint.config.js。ここが持つのは起動だけである。
## 機械が課すのは type の enum と type / subject の空チェックだけで、文体と長さはレビューで見る。

.PHONY: commitlint ## 直近の commit message を検査
commitlint:
	@git log -1 --format=%B | $(NODE_TOOL) commitlint

.PHONY: commitlint-msg ## 標準入力の commit message を検査（lefthook の commit-msg が呼ぶ）
commitlint-msg:
	@$(NODE_TOOL) commitlint

.PHONY: commitlint-range ## Pull Request がベースの上へ積んだ範囲を検査
## 範囲の起点は origin/<base> の**現在の先端**とする。Pull Request のイベントが示すベースの断面を
## 使うと、ベースがその後動いた場合に**自分が作成していない古い commit**が範囲へ残り、持っていない
## 履歴で落ちる。
##
## 完全な履歴の取得が要る。浅い取得では範囲の両端が解決できない。
##
## 起点は CI が渡す。渡されない手元では origin/HEAD を引き、それも無ければ空にする。
## **既定値を固定のブランチ名にしない** —— 存在しない ref を既定にすると、範囲が 0 件になった
## 理由が「積んだ commit が無い」なのか「ref が解決できない」なのか区別できなくなる。
COMMITLINT_FROM ?= $(if $(GITHUB_BASE_REF),origin/$(call literal_value,GITHUB_BASE_REF),$(shell git symbolic-ref --quiet --short refs/remotes/origin/HEAD 2>/dev/null))
COMMITLINT_TO ?= HEAD

## 範囲の件数の判定は scripts/commit-range が持つ。
## 引数は sh -c の位置引数で渡す（RUN_SCRIPT の $(2) は単一引用符の内側に入る）。範囲の両端は make の展開で
## レシピのシェルの文字列へ埋めず、env（RANGE_FROM・RANGE_TO）から読む。ref の名前は $ や " を含みうる。
commitlint-range: export RANGE_FROM = $(call literal_value,COMMITLINT_FROM)
commitlint-range: export RANGE_TO = $(call literal_value,COMMITLINT_TO)
commitlint-range:
	@$(call RUN_SCRIPT,commit-range,"$$@") commit-range check -from "$$RANGE_FROM" -to "$$RANGE_TO"
	@$(NODE_TOOL) commitlint --from "$$RANGE_FROM" --to "$$RANGE_TO"
