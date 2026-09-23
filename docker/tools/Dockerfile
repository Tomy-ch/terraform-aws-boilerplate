# 道具を提供するイメージ。**版の出所はこのファイルではない**（ADR-0503 決定1・決定2）。
# 版は mise.toml が持ち、ここが持つのは実行形態だけである。イメージのビルドは mise.toml を
# 読んで install するので、版を書き写した第二の出所がここにできない。
#
# 段を分けるのは、提供段に mise 本体と install のキャッシュを残さないため（同 決定11）。
# ベースは digest で固定する（同 決定10）——tag だけの参照は、付け替えられた時点で別の
# イメージを黙って引く。
#
# ここに terraform は居ない。単一の静的バイナリであり、資格情報の受け渡しに境界を増やさない
# ため、ホストで実行する（同 決定5）。

# mise 本体の版。全ステージ共通。
ARG MISE_VERSION=v2026.9.9


# ==============================================================================
# go_tools_builder: mise.toml の宣言どおりに道具を入れ、バイナリだけを取り出す
# ==============================================================================
FROM golang:1.27.1-bookworm@sha256:648f440f42a0958804efb24df176f806f9d353b41f1c0627f666428e40310f6b AS go_tools_builder

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
# ずれたまま進むと、ホストとコンテナで別の Go がツールをビルドする。
#
# **宣言側の値は焼き込みであり、`make versions-apply` が書いて `versions-check` が守る。**
# ここで mise.toml から値を抜き出さないのは、そうするとこのファイルが宣言の読み手になるためで
# ある —— 読み手が増えるほど、一方だけが読める書式が生まれる余地が増える。読むのは
# scripts/lib/misetoml だけにして、ここへは結果を書き写す。
RUN set -eu; \
    declared_go="1.27.1"; \
    actual="$(go version | sed 's|^go version go\([^ ]*\).*|\1|')"; \
    [ "$declared_go" = "$actual" ] || { \
      echo "Go の版がずれています: 宣言=$declared_go / ベースイメージ=$actual" >&2; \
      echo "docker/tools/Dockerfile の FROM と mise.toml の go を揃えてください（make versions-apply）" >&2; exit 1; }

# 未認証の GitHub API（60 req/hour・IP 単位）ではこの install 群を賄えず 403 になる。
# トークンはイメージ履歴に焼き込む build 引数ではなく secret で渡す（ADR-0503 決定12）。
# 未指定でも未認証で成立するため、手元のビルドは従来どおり動く。
RUN --mount=type=secret,id=github_token \
    github_token="$(cat /run/secrets/github_token 2>/dev/null || true)"; \
    if [ -n "$github_token" ]; then export GITHUB_TOKEN="$github_token"; fi; \
    mise install actionlint \
    && mise install shellcheck \
    && mise install hadolint \
    && mise install golangci-lint \
    && mise install "aqua:terraform-linters/tflint" \
    && mise install "aqua:aquasecurity/trivy" \
    && mise install "aqua:gitleaks/gitleaks" \
    && mise install "aqua:zizmorcore/zizmor"

RUN mkdir /out \
    && cp "$(mise which actionlint)"     /out/ \
    && cp "$(mise which shellcheck)"     /out/ \
    && cp "$(mise which hadolint)"       /out/ \
    && cp "$(mise which golangci-lint)"  /out/ \
    && cp "$(mise which tflint)"         /out/ \
    && cp "$(mise which trivy)"          /out/ \
    && cp "$(mise which gitleaks)"       /out/ \
    && cp "$(mise which zizmor)"         /out/

# ==============================================================================
# go_tools: 運用機構（scripts/）を走らせる。mise 本体も install キャッシュもここには無い
# ==============================================================================
FROM golang:1.27.1-bookworm@sha256:648f440f42a0958804efb24df176f806f9d353b41f1c0627f666428e40310f6b AS go_tools

# git: 運用機構の多くが git を呼ぶ（base-branch は ls-remote、release は tag、commitlint の
#      範囲取得は rev-list）。テストもこれらを実際に起動して引数列を検査する。
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
# node_tools: npm エコシステムの道具。markdownlint-cli2 / commitlint
# ==============================================================================
# Node ランタイムは公式の alpine イメージを使う。mise の Node は alpine 上でソースからの
# ビルドを要求するため、ここはベースイメージに頼る。
#
# この段は mise を持たない。mise.toml は**版の宣言として読むだけ**で、install は npm が行う
# （ADR-0503 決定3）。mise を入れて npm backend を使う形は採れない —— mise は
# 宣言された node が「mise 経由で」入っていることを要求し、ベースイメージの node を認めない。
FROM node:24.21.0-alpine@sha256:be80f76cf40ec8e42b9bec49f60a55e0660f30af58d3e5a25530785b30ea67e2 AS node_tools

# パイプの左側が失敗しても右側が成功すれば RUN が通ってしまう既定を塞ぐ。
# 下の版の検査はコマンド置換の中でパイプを使うため、これが無いと**検査が実行されないまま緑になる**。
#
# `/bin/sh` ではなく実体のシェルを名指しするのは、`sh` が pipefail を持つ保証が無いためである
# （alpine の `sh` は busybox で、pipefail を持たない）。
SHELL ["/bin/ash", "-o", "pipefail", "-c"]

# git: commitlint が commit の範囲を読む。
RUN apk add --no-cache bash ca-certificates git make

WORKDIR /app

# 版の出所は mise.toml だけである（ADR-0503 決定1）。**ここに在るのはその写しであり、
# `make versions-apply` が書いて `versions-check` が守る。** この段は mise.toml を読まない ——
# 宣言を読む実装は scripts/lib/misetoml だけに置く。
#
# --ignore-scripts: install 時に依存のライフサイクルスクリプトを実行しない。ハッシュが一致した
# 配布物であっても script は install 時に走るため、実行させないことでその余地を無くす
# （ADR-0503 決定16 と同じ判断）。in-tree のパッケージを持つようになった時点で、lockfile と
# 「宣言に無い script を持ち込んだら失敗させる」形（同 決定17）へ移す。
RUN set -eu; \
    declared_node="24.21.0"; \
    actual_node="$(node --version | sed 's|^v||')"; \
    [ "$declared_node" = "$actual_node" ] || { \
      echo "Node の版がずれています: 宣言=$declared_node / ベースイメージ=$actual_node" >&2; \
      echo "docker/tools/Dockerfile の FROM と mise.toml の node を揃えてください（make versions-apply）" >&2; exit 1; }; \
    npm install -g --ignore-scripts \
      "markdownlint-cli2@0.23.2" \
      "@commitlint/cli@21.2.2"; \
    npm cache clean --force; \
    rm -rf /root/.npm

ENTRYPOINT []
CMD []
