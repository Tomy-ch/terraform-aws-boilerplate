## 検査をどこで走らせるか。**検査の定義そのものはここに書かない。**
##
## 同一の検査に対してローカル用とCI用の2つの定義を作らない。定義は1箇所に置き、実行環境の差は
## この前置きだけで表す。2つ書くと、片方だけが更新された状態を両方実行するまで検出できない。
##
##   RUNNER_MODE=container （既定）  mise.toml から組んだイメージで実行する
##   RUNNER_MODE=host              用意済みの道具を直接実行する
##
## CI は host で呼ぶ。実行環境が使い捨てであり、既に隔離されているため、そこをもう一段
## コンテナで包む理由が無い。道具の版はどちらの経路でも mise.toml が決める。

RUNNER_MODE ?= container

ifeq ($(RUNNER_MODE),host)
GO_TOOL :=
NODE_TOOL :=
else
# ホストの gh からトークンを借りる。未認証の GitHub API は 60 req/hour・IP 単位で、
# イメージの install 群を賄えず 403 になる。
# 生成物をホストの所有者で書き出すため uid/gid を渡す（root で書くと本人が消せない）。
COMPOSE_RUN = RUNNER_UID=$$(id -u) RUNNER_GID=$$(id -g) \
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

## version manager 自身を入力とする道具の起動。
##
## 検査の入力が version manager であるため、提供段に version manager を持たないツールランナーでは
## 成立しない（ADR-0503 決定11）。ホストで実行する。CI も実行環境へ直接用意するので同じ経路になる。
RUN_SCRIPT_HOST = sh -c 'set -eu; mkdir -p $(SCRIPT_BIN); \
	go -C scripts build -o "$$PWD/$(SCRIPT_BIN)/$(1)" ./$(1); \
	exec "./$(SCRIPT_BIN)/$(1)" $(2)'
