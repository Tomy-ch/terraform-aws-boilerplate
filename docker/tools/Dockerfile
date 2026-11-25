# 道具を提供するイメージ。**版の出所はこのファイルではない。** 版の出所・段構成・terraform を
# ここに含めない理由は docker/tools/README.md が持つ（ADR-0503 決定1・2・5・10・11）。
# **terraform をここへ足さない。**

ARG MISE_VERSION=v2026.9.9


# ==============================================================================
# go_tools_builder: mise.toml の宣言どおりに道具を入れ、バイナリだけを取り出す
# ==============================================================================
FROM golang:1.27.4-bookworm@sha256:4fa7c968ba93f1635670522dbb01a2c78150443e1385746924566079f36570ea AS go_tools_builder

# パイプの左側が失敗しても右側が成功すれば RUN が通ってしまう既定を塞ぐ。
# 下の版の検査はコマンド置換の中でパイプを使うため、これが無いと**検査が実行されないまま緑になる**。
#
# `/bin/sh` ではなく実体のシェルを名指しするのは、`sh` が pipefail を持つ保証が無いためである
# （Debian の `sh` は dash で、pipefail を持たない）。
SHELL ["/bin/bash", "-o", "pipefail", "-c"]

RUN apt-get update && apt-get install -y --no-install-recommends bash ca-certificates curl git && rm -rf /var/lib/apt/lists/*

ARG MISE_VERSION
ENV MISE_DATA_DIR=/root/.local/share/mise \
    MISE_CONFIG_DIR=/root/.config/mise \
    PATH="/root/.local/bin:/root/.local/share/mise/shims:$PATH" \
    # ホスト側の MISE_DISABLE_TOOLS をコンテナへ持ち込ませない。導入する道具は [tools] の
    # 宣言だけで決まる。
    MISE_DISABLE_TOOLS=""
# パイプのままだとリトライ時に部分受信分が sh へ流れ込むため、ファイルへ落としてから実行する。
RUN curl -fsSL --retry 3 --retry-delay 2 --retry-all-errors https://mise.run -o /tmp/mise-install.sh \
    && MISE_VERSION=${MISE_VERSION} sh /tmp/mise-install.sh \
    && rm /tmp/mise-install.sh \
    && /root/.local/bin/mise --version

WORKDIR /app
COPY mise.toml ./
RUN mise trust mise.toml

# ベースイメージの Go が mise.toml の宣言と一致することを強制する（ADR-0503 決定3）。
#
# 値をここで mise.toml から直接読まない理由は mise.toml 冒頭が持つ。焼き込みは
# `make versions-apply` が書き `versions-check` が守る。
RUN set -eu; \
    declared_go="1.27.4"; \
    actual="$(go version | sed 's|^go version go\([^ ]*\).*|\1|')"; \
    [ "$declared_go" = "$actual" ] || { \
      echo "Go の版がずれています: 宣言=$declared_go / ベースイメージ=$actual" >&2; \
      echo "docker/tools/Dockerfile の FROM と mise.toml の go を揃えてください（make versions-apply）" >&2; exit 1; }

# トークンの渡し方と未認証時の制限は docker/tools/README.md が持つ（ADR-0503 決定12）。
# go: backend は mise.toml の go を mise 管理の Go として要求するが、この段の Go はベースイメージが
# 提供する（版の一致は上の RUN が守る）。
RUN --mount=type=secret,id=github_token \
    github_token="$(cat /run/secrets/github_token 2>/dev/null || true)"; \
    if [ -n "$github_token" ]; then export GITHUB_TOKEN="$github_token"; fi; \
    mise install actionlint \
    && mise install shellcheck \
    && mise install hadolint \
    && mise install golangci-lint \
    && MISE_DISABLE_TOOLS=go mise install "go:golang.org/x/vuln/cmd/govulncheck" \
    && mise install "aqua:terraform-linters/tflint" \
    && mise install "aqua:aquasecurity/trivy" \
    && mise install "aqua:gitleaks/gitleaks" \
    && mise install "aqua:trufflesecurity/trufflehog" \
    && mise install "aqua:crate-ci/typos" \
    && mise install "aqua:editorconfig-checker/editorconfig-checker" \
    && mise install "aqua:zizmorcore/zizmor" \
    && mise install "aqua:open-policy-agent/conftest"

RUN mkdir /out \
    && cp "$(mise which actionlint)"     /out/ \
    && cp "$(mise which shellcheck)"     /out/ \
    && cp "$(mise which hadolint)"       /out/ \
    && cp "$(mise which golangci-lint)"  /out/ \
    && cp "$(mise which govulncheck)"    /out/ \
    && cp "$(mise which tflint)"         /out/ \
    && cp "$(mise which trivy)"          /out/ \
    && cp "$(mise which gitleaks)"       /out/ \
    && cp "$(mise which trufflehog)"     /out/ \
    && cp "$(mise which typos)"          /out/ \
    && cp "$(mise which editorconfig-checker)" /out/ \
    && cp "$(mise which zizmor)"         /out/ \
    && cp "$(mise which conftest)"       /out/

# ==============================================================================
# go_tools: 運用機構（scripts/）を走らせる。mise 本体も install キャッシュもここには無い
# ==============================================================================
FROM golang:1.27.4-bookworm@sha256:4fa7c968ba93f1635670522dbb01a2c78150443e1385746924566079f36570ea AS go_tools

# git: 運用機構とそのテストが git を実際に起動する（ADR-0503 決定6）。
# make: レシピをコンテナ側で実行する経路のため。
RUN apt-get update && apt-get install -y --no-install-recommends bash ca-certificates git make && rm -rf /var/lib/apt/lists/*

COPY --from=go_tools_builder /out/ /usr/local/bin/

WORKDIR /app

# ホストの GOPATH を汚さず、かつ非 root で書ける場所に置く。
ENV GOCACHE=/tmp/go-build \
    GOMODCACHE=/tmp/go-mod

# root で動かさない。bind mount した生成物が root 所有になると、道具が直した行を直した本人が
# 消せない状態がリポジトリに残る。compose も user を渡すが、イメージ単体でも既定を非 root にする。
# 数値で書くのは、ホスト側が名前を解決できるとは限らないため（hadolint DL3066）。
# 65534 は nobody:nogroup。この段に mise も home も残していないため、書き込む先を持たない。
USER 65534:65534

ENTRYPOINT []
CMD []

# ==============================================================================
# node_tools_builder: pnpm で道具を lockfile どおりに入れる
# ==============================================================================
# Node ランタイムは公式の alpine イメージを使う。mise の Node は alpine 上でソースからの
# ビルドを要求するため、ここはベースイメージに頼る。
#
# この段が mise を使わない理由は docker/tools/README.md が持つ。
FROM node:26.13.1-alpine@sha256:6bd9ab43c0ec0190ca7b2d193906cb8b7edf6e7f4afa43ddeccd6452710bffb5 AS node_tools_builder

# pipefail 付きのシェルへ切り替える理由は go_tools_builder 段の SHELL 直上が持つ。
# alpine の `sh` は busybox で、pipefail を持たない。
SHELL ["/bin/ash", "-o", "pipefail", "-c"]

RUN apk add --no-cache ca-certificates curl

# 版と照合値は make pin-pnpm-resolve が mise.toml の宣言から書く写しで、make pin-pnpm-check が守る。
ARG TARGETARCH
RUN set -eu; \
    pnpm_version="12.19.0"; \
    pnpm_sha256_amd64="b41d7b2d8f47187fdf9b3df170f984ffb02ef14bd27d353ccff704cef0d1af6e"; \
    pnpm_sha256_arm64="cf6a59cbbd06800575d8274cd5f84eb9de74794ec495630b554bfef341ecd0d8"; \
    case "${TARGETARCH:-}" in \
      amd64) asset=x64; sum="$pnpm_sha256_amd64" ;; \
      arm64) asset=arm64; sum="$pnpm_sha256_arm64" ;; \
      *) echo "pnpm の配布物が無いアーキテクチャです: '${TARGETARCH:-}'（BuildKit でビルドする）" >&2; exit 1 ;; \
    esac; \
    curl -fsSL --retry 3 --retry-delay 2 --retry-all-errors \
      "https://github.com/pnpm/pnpm/releases/download/v${pnpm_version}/pnpm-linux-${asset}-musl.tar.gz" -o /tmp/pnpm.tar.gz; \
    echo "${sum}  /tmp/pnpm.tar.gz" | sha256sum -c -; \
    mkdir -p /opt/pnpm; \
    tar -xzf /tmp/pnpm.tar.gz -C /opt/pnpm; \
    rm /tmp/pnpm.tar.gz; \
    ln -s /opt/pnpm/pnpm /usr/local/bin/pnpm; \
    [ "$(pnpm --version)" = "$pnpm_version" ]

# frozen で入れる根拠は ADR-0503 決定17・18。package.json の版は make versions-apply が mise.toml から書く写しである。
WORKDIR /opt/node-tools
COPY docker/tools/node/package.json docker/tools/node/pnpm-lock.yaml docker/tools/node/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile --prod --store-dir /tmp/pnpm-store

# ==============================================================================
# node_tools: npm エコシステムの道具。markdownlint-cli2 / commitlint
# ==============================================================================
# pnpm と curl はこの段へ持ち込まない（ADR-0503 決定11）。
FROM node:26.13.1-alpine@sha256:6bd9ab43c0ec0190ca7b2d193906cb8b7edf6e7f4afa43ddeccd6452710bffb5 AS node_tools

SHELL ["/bin/ash", "-o", "pipefail", "-c"]

# git: commitlint が commit の範囲を読む。
RUN apk add --no-cache bash ca-certificates git make

# Node の版をベースイメージと照らす（ADR-0503 決定3）。
RUN set -eu; \
    declared_node="26.13.1"; \
    actual_node="$(node --version | sed 's|^v||')"; \
    [ "$declared_node" = "$actual_node" ] || { \
      echo "Node の版がずれています: 宣言=$declared_node / ベースイメージ=$actual_node" >&2; \
      echo "docker/tools/Dockerfile の FROM と mise.toml の node を揃えてください（make versions-apply）" >&2; exit 1; }

COPY --from=node_tools_builder /opt/node-tools /opt/node-tools
ENV PATH="/opt/node-tools/node_modules/.bin:$PATH"

WORKDIR /app

# root で動かさない理由は go_tools 段の USER 直上が持つ。
USER 65534:65534

ENTRYPOINT []
CMD []
