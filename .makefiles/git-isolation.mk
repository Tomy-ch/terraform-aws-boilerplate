## 一時のリポジトリで git を使うレシピを、外側のリポジトリから切り離す。
##
## git は hook に GIT_DIR や GIT_INDEX_FILE を渡す。それを継いだ `git -C <一時の場所>` は、一時のリポジトリ
## ではなく外側のリポジトリを書き換える。外す変数の一覧は git 自身が返すもの（git rev-parse --local-env-vars）
## に従う。Go のテストの側は scripts/lib/testenv の RunGitIsolated が同じことをする。

## レシピの先頭で使う。一覧を得られなければ exit 2 で止める。
define GIT_ISOLATE
git_vars=$$(git rev-parse --local-env-vars) && test -n "$$git_vars" || { \
	echo "❌ git rev-parse --local-env-vars から、外す環境変数の一覧を得られません" >&2; exit 2; }; \
unset $$git_vars
endef

## target $(1) を、使い捨ての外側のリポジトリへ GIT_DIR と GIT_INDEX_FILE を向けて走らせ、外側の .git の
## 中身が変わっていれば exit 1 にする。$(1) が GIT_ISOLATE を欠いたことを、hook の中でなくても落とす。
define GIT_OUTER_CHECK
mkdir -p tmp; outer=$$(mktemp -d tmp/git-outer.XXXXXX); trap 'rm -rf "$$outer"' EXIT; \
( $(GIT_ISOLATE); git -C "$$outer" -c core.hooksPath=/dev/null init -q ); \
snap() { find "$$outer/.git" -type f -exec cksum {} + | sort; }; before=$$(snap); \
GIT_DIR="$$PWD/$$outer/.git" GIT_INDEX_FILE="$$PWD/$$outer/.git/index" $(MAKE) -s --no-print-directory $(1); \
test "$$(snap)" = "$$before" || { \
	echo "❌ $(1) が外側のリポジトリを書き換えました。レシピの先頭に \$$(GIT_ISOLATE) が要ります" >&2; exit 1; }
endef
