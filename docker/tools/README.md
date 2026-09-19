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
| `go_tools` | `terraform` を除く mise 管理の道具 —— `tflint` / `trivy` / `gitleaks` / `zizmor` / `actionlint` / `shellcheck` / `hadolint` / `golangci-lint` / `lefthook` |
| `node_tools` | `markdownlint-cli2` / `@commitlint/cli` |

**`terraform` はここに居ない。** 人と CI が最も頻繁に叩く単一の静的バイナリであり、資格情報の
受け渡しに境界を増やさないため、mise がホストへ入れたものを直接実行する（ADR-0503 決定5）。

`node_tools` は mise を持たない。`mise.toml` は**版の宣言として読むだけ**で、install は npm が行う
（同 決定3）—— mise は宣言された node が mise 経由で入っていることを要求し、ベースイメージの node を
認めないためである。`--ignore-scripts` を付けるのは、ハッシュが一致した配布物であっても script は
install 時に走るためで、実行させないことでその余地を無くす（同 決定16）。

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
