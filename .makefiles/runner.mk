## 検査をどこで走らせるか。**検査の定義そのものはここに書かない。**
## 実行環境の差はこの前置きだけで表す（ADR-0503 決定8）。
##
##   RUNNER_MODE=container （既定）  mise.toml から組んだイメージで実行する
##   RUNNER_MODE=host              用意済みの道具を直接実行する
##
## CI は host で呼ぶ（ADR-0503 決定7）。道具の版はどちらの経路でも mise.toml が決める。

RUNNER_MODE ?= container

## npm 側の道具をホストで起動する前置き（ADR-0503 決定7・17）。
##
## 起動のたびに $(1) の lockfile を frozen で入れ、入れた先の道具を絶対パスで exec する。lockfile が
## package.json からずれていれば、道具を起動する前に落ちる。PATH の同名の道具へは退かない。
## install は $(2) のディレクトリを排他の印にして直列にする。並行の初回 install は同じ node_modules へ
## shim を書き合って落ちる。install と exec を1つの sh に収めるのは、パイプの stdin を道具へ届けるため。
##
##   $(1) package.json と lockfile を持つディレクトリ（絶対パス）
##   $(2) 排他の印にするディレクトリ（絶対パス）
NODE_TOOLS_DIR := docker/tools/node
NODE_INSTALL_LOCK := tmp/node-tools-install.lock
NODE_HOST_RUN = sh -c 'set -eu; dir="$$1"; lock="$$2"; tool="$$3"; shift 3; \
	mkdir -p "$${lock%/*}"; n=0; \
	until mkdir "$$lock" 2>/dev/null; do \
		n=$$((n + 1)); \
		[ "$$n" -lt 600 ] || { echo "❌ $$lock を2分取れません。install が動いていなければ消してください" >&2; exit 1; }; \
		sleep 0.2; \
	done; \
	unlock() { rmdir "$$lock" 2>/dev/null || true; }; trap unlock EXIT; \
	pnpm -C "$$dir" install --frozen-lockfile --prod --reporter=silent </dev/null >&2; \
	unlock; trap - EXIT; \
	exec "$$dir/node_modules/.bin/$$tool" "$$@"' node-tool "$(1)" "$(2)"

ifeq ($(RUNNER_MODE),host)
GO_TOOL :=
NODE_TOOL := $(call NODE_HOST_RUN,$(CURDIR)/$(NODE_TOOLS_DIR),$(CURDIR)/$(NODE_INSTALL_LOCK))
else
# ホストの gh からトークンを借りる。未認証の GitHub API は 60 req/hour・IP 単位で、
# イメージの install 群を賄えず 403 になる。
# 生成物をホストの所有者で書き出すため uid/gid を渡す（root で書くと本人が消せない）。
COMPOSE_RUN = RUNNER_UID=$$(id -u) RUNNER_GID=$$(id -g) \
	RUNNER_GIT_COMMON_DIR="$$(git rev-parse --path-format=absolute --git-common-dir 2>/dev/null)" \
	GITHUB_TOKEN="$${GITHUB_TOKEN:-$$(gh auth token 2>/dev/null || true)}" \
	docker compose run --rm -T
GO_TOOL := $(COMPOSE_RUN) go_tool_runner
NODE_TOOL := $(COMPOSE_RUN) node_tool_runner
endif

## 運用機構（scripts/）の起動
##
## go.mod は scripts/ をモジュールルートとするため、`go -C scripts run` は cwd をそこへ移す。
## 一方で各ツールはリポジトリ直下を基準にパスを解決する（.github/egress.toml など）。
## そのままでは scripts/.github/... を探して落ちる。ビルドしてから直下で実行する。
SCRIPT_BIN := tmp/bin
RUN_SCRIPT = $(GO_TOOL) sh -c 'set -eu; mkdir -p $(SCRIPT_BIN); \
	go -C scripts build -o "$$PWD/$(SCRIPT_BIN)/$(1)" ./$(1); \
	exec "./$(SCRIPT_BIN)/$(1)" $(2)'

## 道具をビルドだけする（起動しない）。同じ置き場の別の道具が、ビルドしたものを起動する。
BUILD_SCRIPT_HOST = sh -c 'set -eu; mkdir -p $(SCRIPT_BIN); go -C scripts build -o "$$PWD/$(SCRIPT_BIN)/$(1)" ./$(1)'

## 提供段のコンテナに無い前提を要する道具の起動。RUN_SCRIPT と同じく直下で実行する。
##
## どの前提に当たるかは呼び出し元の .mk のコメントが持つ（.makefiles/README.md）。
RUN_SCRIPT_HOST = sh -c 'set -eu; mkdir -p $(SCRIPT_BIN); \
	go -C scripts build -o "$$PWD/$(SCRIPT_BIN)/$(1)" ./$(1); \
	exec "./$(SCRIPT_BIN)/$(1)" $(2)'
