# terraform-aws-boilerplate

**限定された AWS アーキテクチャとユースケースを、安全かつ再現可能な形で Terraform へエンコードした
参照実装。** 汎用の Terraform module collection ではなく、**自由度より保証可能性を優先する**
（[ADR-0101](docs/adr/0101-architecture-principles.md)）。

何でも書ける口を持たないことが、このリポジトリの性質である。設定は型付きで有限であり
（[ADR-0203](docs/adr/0203-typed-configuration-policy.md)）、任意の値を通す脱出口を持たず
（[ADR-0204](docs/adr/0204-no-generic-escape-hatch.md)）、外部 Terraform module へ依存しない
（[ADR-0205](docs/adr/0205-module-dependency-policy.md)）。いずれも検査で守られており、
規約は暗黙知にせず ADR として明文化している。

## 現在の状態

**Terraform の実装はまだ無い。** 存在するのは、決定（`docs/adr/`）と、それを守るための機構である。

| 区分 | 状態 |
| --- | --- |
| `docs/adr/` | 22 件の Accepted ADR。`make adr-lint` が構造と相互参照を検査する |
| `.github/workflows/` | 19 workflow。必須検査 12 件が pull request を止める |
| `.makefiles/` | 検査の定義。`RUNNER_MODE` でホストとコンテナを切り替える |
| `scripts/` | リポジトリ運用機構（Go）。ゲートそのものをテストで固定する |
| `docker/` | 道具を提供するイメージ。版の出所は `mise.toml` だけ |
| `modules/` / `examples/` | **未着手** —— ユースケースの実装が無い |
| Terraform 側の検査 | **未配線** —— `.tflint.hcl`、Conftest、`terraform test` はまだ無い |

## 使い方

道具は [`mise.toml`](mise.toml) が1箇所で宣言する。3つの実行経路すべてがこのファイルを読む
（[ADR-0503](docs/adr/0503-tool-execution-form.md) 決定1）。

```sh
mise install                 # ホストへ道具を導入する
make help                    # 検査とターゲットの一覧
make md-lint                 # 既定は RUNNER_MODE=container（docker compose 経由）
make md-lint RUNNER_MODE=host  # ホストの mise が入れたもので実行する
```

同じ検査に対して実行環境ごとの定義は作らない。定義は `.makefiles/` が1箇所で持ち、実行環境の差は
起動の前置きだけで表す（同 決定8）。

## 読む順序

1. [`docs/adr/README.md`](docs/adr/README.md) —— **受理されたすべての決定の一覧が存在する唯一の場所。**
   番号は前二桁が関心事の群、後二桁が群内のスロットである。
2. [`AGENTS.md`](AGENTS.md) —— このリポジトリでどう作業するか。AI コーディングエージェント向けだが、
   人が読んでも規約として通る。
3. [`scripts/README.md`](scripts/README.md) —— 運用機構が何を検査しているか。
4. [`.github/workflows/README.md`](.github/workflows/README.md) —— CI の起動条件と、GitHub Actions
   固有の制約。

## このリポジトリから派生する

```sh
make setup-repo    # タグ・ブランチ・ラベル・ルールセットを初期化する
```

**取り消しの効かない操作を含む**（タグの一括削除、デフォルトブランチの移動）。実行前に
[`scripts/repo-setup/`](scripts/repo-setup/) が何をするかを読むこと。

## ライセンス

[MIT](LICENSE)
