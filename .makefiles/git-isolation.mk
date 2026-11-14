## 一時リポジトリを建てるレシピと、外側のリポジトリの不変の検査
##
## git の hook の中では GIT_DIR / GIT_INDEX_FILE などが commit しているリポジトリを指しており、
## `git -C <一時の場所>` も、レシピが呼ぶ道具の内部の git も、そちらへ書く。一時リポジトリを建てる
## レシピは先頭で GIT_ISOLATE を通し、呼び出し側は GIT_OUTER_CHECK を通して呼ぶ。
## 実リポジトリを対象とするレシピは外さない —— `git commit -a` の一時 index を読むのが正しい。

## git 自身が定義する、リポジトリの位置を決める環境変数を外す。一覧を得られなければ exit 2。
GIT_ISOLATE = vars=$$(git rev-parse --local-env-vars) && test -n "$$vars" || { \
	echo "❌ git rev-parse --local-env-vars から外す変数の一覧を得られません" >&2; exit 2; }; \
	unset $$vars

## make go-test が GIT_DIR / GIT_INDEX_FILE に入れる、存在しえないパス。testenv.RunGitIsolated を
## 通さないテストのパッケージは、最初の git でこの名前を含む失敗を出す。
GIT_POISON := /dev/null/git-isolation-required

## target $(1) を、使い捨ての外側リポジトリへ GIT_DIR / GIT_INDEX_FILE を向けて走らせ、外側の .git の
## 中身が変わっていれば target の成否に依らず exit 1。変わらなければ target の終了コードを返す。
## サブシェルで走らせるので、呼び出し元の GIT_DIR は残る。
define GIT_OUTER_CHECK
( $(GIT_ISOLATE); \
	mkdir -p tmp; o=$$(mktemp -d tmp/git-outer.XXXXXX) || exit 2; trap 'rm -rf "$$o"' EXIT; \
	o=$$(cd "$$o" && pwd); printf 'outer\n' > "$$o/README"; \
	git -C "$$o" init -q && git -C "$$o" add README && \
	git -C "$$o" -c user.name=outer -c user.email=outer@example.invalid -c commit.gpgsign=false \
		-c core.hooksPath=/dev/null commit -q -m outer || exit 2; \
	snap() { (cd "$$o/.git" && find . -type f -exec cksum {} + | LC_ALL=C sort); }; \
	before=$$(snap); \
	rc=0; GIT_DIR="$$o/.git" GIT_INDEX_FILE="$$o/.git/index" $(MAKE) -s --no-print-directory $(1) || rc=$$?; \
	if [ "$$(snap)" != "$$before" ]; then \
		echo "❌ $(1) が外側のリポジトリ（GIT_DIR / GIT_INDEX_FILE の指す先）を書き換えました" >&2; exit 1; fi; \
	exit $$rc )
endef
