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

**Terraform の実装は、bootstrap のユースケースの `state-backend` と `deployment-identity`、ユースケースの `finops` と `network-foundation`、公開の Hosted Zone の `dns-zone`、組織の証跡を保管する `audit-evidence`、通知の `ops-notification`、常駐層のユースケースの `image-registry`、アカウントとリージョンの安全側の設定の `account-baseline`、一般 module の `modules/s3/`・`modules/kms/`・`modules/log-group/`・`modules/vended-log-group/`・`modules/sns/`・`modules/vpc/`・`modules/default-regions/` だけである。** `state-backend` と `deployment-identity` はどちらも bootstrap の段1-4 の分だけを持ち、`modules/s3/`・`modules/kms/` は `state-backend` が呼び、`modules/kms/`・`modules/log-group/`・`modules/vpc/` は `network-foundation` が呼び、`modules/kms/`・`modules/vended-log-group/` は `audit-evidence` が、`modules/kms/`・`modules/sns/` は `ops-notification` が、`modules/kms/` は `image-registry` が呼ぶ。`modules/default-regions/` は `account-baseline` と `deployment-identity` が呼ぶ。`network-foundation`・`dns-zone`・`audit-evidence`・`ops-notification`・`image-registry`・`account-baseline` を呼ぶ env は無い。そのほかに存在するのは、決定（`docs/adr/`）と、それを守るための機構である。

| 区分 | 状態 |
| --- | --- |
| `docs/adr/` | repository-wide の Accepted ADR。一覧は `docs/adr/README.md` が持ち、`make adr-lint` が構造と相互参照を検査する |
| `.github/workflows/` | CI の定義。必須検査の一覧は `.github/settings/branch-protection.json` が持つ |
| `.makefiles/` | 検査の定義。`RUNNER_MODE` でホストとコンテナを切り替える |
| `scripts/` | リポジトリ運用機構（Go）。ゲートそのものをテストで固定する |
| `docker/` | 道具を提供するイメージ。版の出所は `mise.toml` だけ |
| `conftest/` | Conftest の Rego と宣言。`plan/` が Policy Test（[`conftest/plan/README.md`](conftest/plan/README.md)） |
| `modules/` | 一般 module の `s3/`・`kms/`・`log-group/`・`vended-log-group/`・`sns/`・`vpc/` だけ |
| `usecases/` | `state-backend/` と `deployment-identity/`（bootstrap の段1-4 の分）と、管理アカウントの workload root が呼ぶ `ops-notification/`・`finops/` と、`network-foundation/`・`dns-zone/`・`audit-evidence/`・`image-registry/`・`account-baseline/`（どの root からも呼ばれない）と、設計記録（README と `docs/adr/`）だけを持つ `security-tooling/` だけ |
| `env/` | 管理アカウントの identity root の `admin/identity/` と workload root の `admin/` だけ |
| `providers/` | provider の版の正本（`providers.toml` と、そこから作る `.terraform.lock.hcl`）。root の lock はその写し（[`providers/README.md`](providers/README.md)） |
| Terraform 側の検査 | fmt / validate / TFLint / `terraform test` / Policy Test（Conftest、入力は plan）と、Conftest によるインフラ内コードの検出・resource の作成の順序の検査・層の規則の検査・root の構成と backend 設定の不変条件の検査（入力は HCL のソースから作る JSON）は `make`・pre-commit・CI に配線済み |

## 使い方

道具は [`mise.toml`](mise.toml) が1箇所で宣言する。3つの実行経路すべてがこのファイルを読む
（[ADR-0503](docs/adr/0503-tool-execution-form.md) 決定1）。

```sh
mise install                 # ホストへ道具を導入する（mise.toml の全エントリ。npm: の道具は除く）
make host-tools-install      # うち、手元での実行がホストのものだけ（terraform / AWS CLI / pnpm）
make help                    # 検査とターゲットの一覧
make md-lint                 # 既定は RUNNER_MODE=container（docker compose 経由）
make md-lint RUNNER_MODE=host  # ホストで実行する（npm の道具は起動のたびに lockfile から入れる）
```

同じ検査に対して実行環境ごとの定義は作らない。定義は `.makefiles/` が1箇所で持ち、実行環境の差は
起動の前置きだけで表す（同 決定8）。

## 読む順序

1. [`docs/adr/README.md`](docs/adr/README.md) —— **repository-wide の受理された決定の一覧が存在する唯一の場所。**
   番号は前二桁が関心事の群、後二桁が群内のスロットである。
2. [`AGENTS.md`](AGENTS.md) —— このリポジトリでどう作業するか。AI コーディングエージェント向けだが、
   人が読んでも規約として通る。
3. [`scripts/README.md`](scripts/README.md) —— 運用機構が何を検査しているか。
4. [`.github/workflows/README.md`](.github/workflows/README.md) —— CI の起動条件と、GitHub Actions
   固有の制約。

## このリポジトリから派生する

```sh
make setup-repo    # タグ・ブランチ・ラベル・ルールセット・environment・Private Vulnerability Reporting を初期化する
```

**取り消しの効かない操作を含む**（タグの一括削除、デフォルトブランチの移動）。実行前に
[`scripts/repo-setup/`](scripts/repo-setup/) が何をするかを読むこと。

<!-- sample:begin -->
このリポジトリのサンプルは、派生リポジトリが使わなければ取り除く。外部の契約を要する走査
（SonarCloud、Dependency Review）もここに含まれる。

```sh
make setup-remove-samples DRY_RUN=1   # 取り除くものを表示するだけ
make setup-remove-samples             # 取り除く。commit はしない
```

SonarCloud を残す場合は、SonarCloud にプロジェクトを作り、`SONAR_TOKEN` を Actions の secret に
登録する。登録するまでは、走査は Pull Request に「NOT RUN」と書くだけで落ちない。何がサンプルで、
どう印を置くかは [`scripts/README.md`](scripts/README.md) の `sample-remove/` の行が持つ。
<!-- sample:end -->

## ライセンス

[MIT](LICENSE)
