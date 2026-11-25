## 検査をどこで走らせるか。**検査の定義そのものはここに書かない。**
## 実行環境の差はこの前置きだけで表す（ADR-0503 決定8）。
##
##   RUNNER_MODE=container （既定）  mise.toml から組んだイメージで実行する
##   RUNNER_MODE=host              用意済みの道具を直接実行する
##
## CI は host で呼ぶ（ADR-0503 決定7）。道具の版はどちらの経路でも mise.toml が決める。

RUNNER_MODE ?= container

## npm 側の道具をホストで起動する前置き（ADR-0503 決定7・17）。lockfile を frozen で入れてから、
## 入れた先の道具を起動する。排他・install・起動は scripts/node-tools の exec が持つ。
NODE_TOOLS_DIR := docker/tools/node
NODE_INSTALL_LOCK := tmp/node-tools-install.lock

ifeq ($(RUNNER_MODE),host)
GO_TOOL :=
NODE_TOOL = $(call RUN_SCRIPT_HOST_ARGS,node-tools) exec -dir "$(CURDIR)/$(NODE_TOOLS_DIR)" -lock "$(CURDIR)/$(NODE_INSTALL_LOCK)"
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

## 運用機構（scripts/）の起動。`go run` にせず、ビルドしてからリポジトリ直下で実行する（理由は .makefiles/README.md）。
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

## RUN_SCRIPT_HOST の、前置きとして使う形。引数はすべて後ろに続けて渡す（sh -c の本文に埋めない）。
RUN_SCRIPT_HOST_ARGS = sh -c 'set -eu; mkdir -p $(SCRIPT_BIN); \
	go -C scripts build -o "$$PWD/$(SCRIPT_BIN)/$(1)" ./$(1); \
	exec "./$(SCRIPT_BIN)/$(1)" "$$@"' $(1)

## literal_value は、変数 $(1) の値を返す。コマンドラインと環境から来た値は make に展開させず字面のまま返す
## （ref の名前は $ を含みうる）。Makefile の中の既定は展開する。
literal_value = $(if $(filter command environment,$(firstword $(origin $(1)))),$(value $(1)),$($(1)))
