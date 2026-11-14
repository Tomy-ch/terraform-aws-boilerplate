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
