# Tools コンテナ

検査の道具を、利用者の環境に依らず同じ依存で走らせるための入れ物。

**これは配送物ではない。** このリポジトリが配送するのは Terraform の構成であって、コンテナではない。
版の出所は [`mise.toml`](../../mise.toml) だけで、このディレクトリは版を1つも持たない
（[ADR-0503](../../docs/adr/0503-tool-execution-form.md) 決定1・決定2）。

## ビルドターゲット

| ターゲット | ベースイメージ | 担当範囲 |
| --- | --- | --- |
| `go_tools_builder` | `golang:*-bookworm` | mise を導入し、`mise.toml` が宣言する道具をビルドする段 |
| `go_tools` | `golang:*-bookworm` | ビルド済みの道具だけを持つ提供段。mise 本体と install のキャッシュを残さない |
| `node_tools` | `node:*-alpine` | npm エコシステムの道具 |

段を分けるのは、提供段に version manager 本体と install のキャッシュを残さないためである
（ADR-0503 決定11）。ベースイメージは digest で固定する（同 決定10）—— tag だけの参照は、
付け替えられた時点で別のイメージを黙って引く。digest の SSOT は
[`docker/images-pin.toml`](../images-pin.toml) で、`make pin-images-check` がずれを見る。

**ベースイメージが提供するランタイムの版が `mise.toml` の宣言とずれていれば、ビルドがその場で落ちる**
（同 決定3）。3つの実行経路（ホスト / CI / コンテナ）の版が黙って割れるのを、ビルドの時点で止める。

## 入っている道具

版はいずれも `mise.toml` が持つ。ここに書き写さない —— 第二の出所になる。

| 段 | 道具 |
| --- | --- |
| `go_tools` | `terraform` を除く mise 管理の道具 —— `tflint` / `trivy` / `gitleaks` / `trufflehog` / `typos` / `editorconfig-checker` / `zizmor` / `conftest` / `actionlint` / `shellcheck` / `hadolint` / `golangci-lint` / `govulncheck` / `lefthook` |
| `node_tools` | `markdownlint-cli2` / `@commitlint/cli` |

**`terraform` はここに居ない。** 人と CI が最も頻繁に叩く単一の静的バイナリであり、資格情報の
受け渡しに境界を増やさないため、mise がホストへ入れたものを直接実行する（ADR-0503 決定5）。

**Checkov / KICS / Semgrep CE もここに居ない。** 上流がイメージとして配布する道具であり、そのイメージで
実行する。版は `docker-compose.yaml` の `image:` の tag が、digest は `docker/images-pin.toml` が持つ
（ADR-0503 決定4・10、ADR-0501 決定19）。

`node_tools` は mise を持たず、install は pnpm が行う —— mise は宣言された node が mise 経由で
入っていることを要求し、ベースイメージの node を認めないためである。**この段は `mise.toml` を
読まない。** 宣言を読む実装は [`scripts/lib/misetoml`](../../scripts/README.md) だけに置き、
この段が使う値はすべて写しとして置く。

| 写し | 書く入口 | 守る入口 |
| --- | --- | --- |
| [`node/package.json`](node/package.json) の道具の版 | `make versions-apply` | `make versions-check` |
| [`node/pnpm-workspace.yaml`](node/pnpm-workspace.yaml) の供給網の窓と、窓から外す直接の依存の一覧 | `make versions-apply` | `make versions-check` |
| [`node/pnpm-lock.yaml`](node/pnpm-lock.yaml)（推移的な依存の版と integrity） | `make node-tools-lock` | `make node-tools-lock-check`（宣言との整合）、`make node-tools-image-check`（integrity の照合） |
| Dockerfile の pnpm の版と配布物の sha256 | `make pin-pnpm-resolve` | `make pin-pnpm-check`（版と形）、`make node-tools-image-check`（照合） |

pnpm は単体のバイナリを GitHub Release から取り、sha256 を照合してから置く。道具は lockfile を
frozen で入れる（ADR-0503 決定17・18）。install 時の script の実行は
[`node/pnpm-workspace.yaml`](node/pnpm-workspace.yaml) が宣言し、宣言は空である ——
script を持つ依存が現れた時点で install が落ちる。

lockfile の生成と照合はホストの pnpm で行う。このイメージは lockfile から組まれるので、
lockfile がずれた時点でイメージ自体が組めなくなるためである。pnpm は `make host-tools-install`
が入れる。

## docker-compose サービス

| サービス | 段 | 用途 |
| --- | --- | --- |
| `go_tool_runner` | `go_tools` | `$(GO_TOOL)` が前置きとして呼ぶ |
| `node_tool_runner` | `node_tools` | `$(NODE_TOOL)` が前置きとして呼ぶ |

## 実行方法

**直接は叩かない。** `make` のターゲットが `RUNNER_MODE` に応じて前置きを切り替える
（ADR-0503 決定8・決定9）。

```sh
make md-lint                    # RUNNER_MODE=container（既定）—— コンテナで実行
make md-lint RUNNER_MODE=host   # ホストの mise が入れたもので実行
```

検査の定義は `.makefiles/` が1箇所で持ち、実行環境の差は前置きだけで表す。**同じ検査に対して
実行環境ごとの定義を作らない。**

## 道具を追加する場合

1. `mise.toml` の `[tools]` へ版を宣言する（そこが唯一の出所である）。
2. `Dockerfile` の当該段が、その道具を含む形になっているかを確かめる。
3. 供給網のクールダウンが掛かる（`make tool-cooldown-gate`）。公開直後の版は窓に入る。

GitHub API を未認証で叩くとこの install 群は 60 req/hour の制限に当たり 403 になる。トークンは
build 引数ではなく secret で渡す（ADR-0503 決定12）—— build 引数はイメージの履歴に残る。
