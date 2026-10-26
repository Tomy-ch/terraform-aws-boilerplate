## 検査をどこで走らせるか。**検査の定義そのものはここに書かない。**
##
## 同一の検査に対してローカル用とCI用の2つの定義を作らない。定義は1箇所に置き、実行環境の差は
## この前置きだけで表す。2つ書くと、片方だけが更新された状態を両方実行するまで検出できない。
##
##   RUNNER_MODE=container （既定）  mise.toml から組んだイメージで実行する
##   RUNNER_MODE=host              用意済みの道具を直接実行する
##
## CI は host で呼ぶ（ADR-0503 決定7）。道具の版はどちらの経路でも mise.toml が決める。

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

## 上流がイメージとして配布する道具の起動（ADR-0503 決定4・7）。RUNNER_MODE に依らず、手元も CI もここを通る。
##
## ホストのパスを mount しない。入力を複写で渡し、結果を複写で取り出す（理由は ADR-0503 決定7）。
## 走査は外へ出ないので network も与えない。
## 版と digest は docker-compose.yaml の service が持つ。
##
##   $(1) service 名
##   $(2) コンテナの /work へ複写するホストのディレクトリ
##   $(3) コンテナの /tmp/out を取り出すホストのディレクトリ
##   $(4) docker create の追加の引数
##   $(5) 道具の引数
##
## 道具の終了コードを返す。結果を取り出せなければ、道具が 0 で終わっていても失敗にする。
IMAGE_RUN = sh -c 'set -eu; \
	img=$$(docker compose config --images $(1)); \
	id=$$(docker create --network none $(4) "$$img" $(5)); \
	trap "docker rm -f $$id >/dev/null 2>&1 || true" EXIT; \
	docker cp "$(2)/." "$$id:/work"; \
	rc=0; docker start -a "$$id" || rc=$$?; \
	mkdir -p "$(3)"; \
	docker cp "$$id:/tmp/out/." "$(3)" || { echo "❌ $(1): 結果を取り出せませんでした" >&2; [ "$$rc" -ne 0 ] || rc=1; }; \
	exit $$rc'

## IMAGE_RUN が取り出した SARIF $(1) を code scanning が読める形にする。パスは /work/src/ を外して
## リポジトリの相対パスへ戻し、source の中で抑止した result（suppressions を持つもの）は落とす ——
## code scanning は suppressions を見ずに open の alert として載せる。
define SARIF_NORMALIZE
if [ -f "$(1)" ]; then \
	jq '(.runs[].results) |= map(select((.suppressions // []) | length == 0)) \
		| (.. | objects | select(has("artifactLocation")) | .artifactLocation.uri) |= sub("^(\\.\\./)*/?work/src/"; "")' \
		"$(1)" > "$(1).tmp"; \
	mv "$(1).tmp" "$(1)"; \
fi
endef

## IMAGE_RUN へ渡す Terraform の走査の対象（TF_SCAN_DIRS）を $(1)/src へ写す。.terraform は写さない。
define TF_SCAN_STAGE
mkdir -p "$(1)/src"; scan=""; \
for d in $(TF_SCAN_DIRS); do if [ -d "$$d" ]; then scan="$$scan $$d"; fi; done; \
if [ -z "$$scan" ]; then echo "❌ $@: 走査の対象が0件です（$(TF_SCAN_DIRS)）" >&2; exit 1; fi; \
tar --exclude .terraform -cf "$(1)/stage.tar" $$scan; \
tar -xf "$(1)/stage.tar" -C "$(1)/src"; \
rm -f "$(1)/stage.tar"
endef

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
## 成立しない（ADR-0503 決定11）。ホストで実行する。
RUN_SCRIPT_HOST = sh -c 'set -eu; mkdir -p $(SCRIPT_BIN); \
	go -C scripts build -o "$$PWD/$(SCRIPT_BIN)/$(1)" ./$(1); \
	exec "./$(SCRIPT_BIN)/$(1)" $(2)'
