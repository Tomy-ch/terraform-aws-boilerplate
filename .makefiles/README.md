# .makefiles

## 役割

`make` のターゲットの定義。リポジトリ直下の `Makefile` が、ここの `.mk` を `include` して組み上げる。

make が持つのは、運用機構（`scripts/`）と道具を呼び出す**順序**と、軽い条件分岐だけである。判定・計算・
繰り返しと処理のスクリプトは運用機構の Go が持ち、テストも Go が持つ
（[0702](../docs/adr/0702-repository-operations-substrate.md) 決定24・25）。
各ツールが何を検査するかは [`scripts/README.md`](../scripts/README.md) が持つ。ここはその起動だけを持つ。

**ターゲットの一覧はここが持たない。** 一覧は `make help` が出す —— この文書が一覧を写せば、
次にターゲットが増えた日に片方だけが古くなる。ここが持つのは、一覧から読めないことだけである。

## 命名規約

規約（検査は持たず、`.mk` を足す Pull Request のレビューが見る）:

- **レシピは道具を順に呼ぶことと、軽い条件分岐だけを持つ。** 処理のスクリプトとテストを `.mk` に
  書かない。処理を持った時点で、その機構を丸ごと運用機構の Go へ置き換える（0702 決定25）。
- **`.mk` を足したら、同じ変更で `Makefile` か、それを使う `.mk` に `include` を足す。** どこからも
  `include` されない `.mk` は何も定義しない。
- 一覧に出すターゲットは `.PHONY: <名前> ## <説明>` の1行で宣言する。この形でない行は `make help` に
  出ない（下の「補足」）。
- 宣言を各所へ書き込むターゲットと、そのずれを落とすターゲットは `-apply` と `-check` の対にする
  （0702 決定10・11）。

## 補足

### `make help` に出る条件

`help` は `.PHONY: <名前> ## <説明>` という形の行だけを集める。

- `## <説明>` を持たない `.PHONY` はターゲットとして機能するが、一覧には出ない（`help` 自身がそう）
- `.PHONY` の**直下に続く** `##` のブロックは `help` が読まない。あれは読み手のための散文である

だから「このターゲットは在るか」の答えは `.mk` を読むことではなく、`make help` を叩くことである。

### 検査の起動

検査の定義は1箇所に置き、実行環境の差は起動の前置きだけで表す（[0503](../docs/adr/0503-tool-execution-form.md) 決定8-9）。
前置きは `runner.mk` が組み立て、`RUNNER_MODE`（既定 `container`、CI は `host`）で切り替わる。

| 形 | 何を起動するか | 使う場面 |
| --- | --- | --- |
| `$(GO_TOOL)` | Go 側のツールランナー | `mise.toml` が pin した Go 製の既製品（golangci-lint、trivy、hadolint ほか） |
| `$(NODE_TOOL)` | npm 側のツールランナー。host では `docker/tools/node` の lockfile を frozen で入れてから、入れた先の道具を起動する（`NODE_HOST_RUN`） | markdownlint-cli2、commitlint |
| `$(call RUN_SCRIPT,<名前>,<引数>)` | `scripts/<名前>` をビルドして起動 | このリポジトリが書いた検査 |
| `$(call IMAGE_RUN,<service>,<入力>,<出力>,<create の引数>,<引数>)` | `docker-compose.yaml` の service が宣言する上流のイメージ。mount せず、入力を複写で渡して結果を複写で取り出す。`RUNNER_MODE` に依らない | 上流がイメージとして配布する道具（Checkov / KICS / Semgrep CE。ADR-0503 決定4・7） |
| `$(call RUN_SCRIPT_HOST,<名前>,<引数>)` | 同上、ただしコンテナを経由しない | **提供段のコンテナに無い前提を要するもの** —— version manager 自身を入力にする、`docker` を呼ぶ、`git` / `gh` がホストの資格情報を使う |
| 前置きなし（`terraform` を直接） | ホストの terraform | Terraform 側の検査（ADR-0503 決定4・5）。TFLint だけは `$(GO_TOOL)` を経由する |

最後の1つが要る理由は呼び出し側ごとに違い、いずれも「ツールランナーが提供できない前提」に帰着する
—— 提供段に version manager を残さない形でイメージを組んでいる（[0503](../docs/adr/0503-tool-execution-form.md) 決定11）、
ツールランナーに `docker` が入っていない、`git` / `gh` がホストの資格情報を使う。**どれに当たるかは
呼び出し元の `.mk` のコメントが持つ。**

**この表は検査の起動を尽くす。操作系はここに入らない** —— 領域ごとの節の `github/operation/` と
`terraform/operation/` を見よ。

`RUN_SCRIPT` がビルドしてから起動するのは、`scripts/` がモジュールルートで `go -C scripts run` が
作業ディレクトリをそこへ移す一方、各ツールはリポジトリ直下を基準にパスを解決するためである。

### ローカルのフックとの関係

`.lefthook.yaml` は `make <target>` を呼ぶだけで、検査を定義しない。権威は CI にあり、フックは
速いフィードバックのために在る（[0501](../docs/adr/0501-development-tooling-composition.md) 決定26）。
**フックでのみ成立する検査を作らない**（[0702](../docs/adr/0702-repository-operations-substrate.md) 決定19）。

## 領域ごとの節

### `docker/`

Dockerfile と compose の検査、image digest の固定。

### `github/`

workflow と action の検査、commit message、egress の SSOT、action と runner の固定、ベースブランチの解決。

### `github/operation/`

リリースブランチ・リリースタグ・リポジトリ初期化と、apply の失敗を戻す revert のブランチの操作。
取り消しの効かない操作をホストの資格情報で行うので、`RELEASE_TOOL` / `REPO_SETUP` は `RUN_SCRIPT_HOST` で
起動し、`RUNNER_MODE` を経由しない。理由は各 `.mk` のコメントが持つ。

### `github/setting/`

ブランチ保護・ラベル・リポジトリ設定の適用。

### `go/`

運用機構（`scripts/`）の Go の整形・Lint・テスト。

### `hygiene/`

綴りと `.editorconfig` の一致、editorconfig-checker の無効化の印の形。判定と canary は `scripts/typos-gate`・`scripts/editorconfig-gate`・`scripts/editorconfig-directive-lint` が持つ。

### `markdown/`

Markdown の体裁、ADR の構造、スキルの参照の実在。

### `security/`

secret scan、脆弱性・設定の走査、供給網のクールダウン。

### `terraform/`

Terraform 側の検査（fmt / validate / TFLint / `terraform test` / Policy Test、インフラ内コードの検出、
bootstrap の plan の期待値と identity root の一致）と、その検査自体の検査。Policy Test の conftest は
`$(GO_TOOL)` を経由する。

### `terraform/operation/`

Terraform を動かす入口。`local.mk` は人の手元（AWS へのログイン、env の root の init と plan）、`ci.mk` は
CI（OIDC で role を assume しての plan と apply）。

検査の起動の表の外にあり、ホストの terraform と AWS CLI を人か CI の role の資格情報で起動する。
`TF_ENV`（資格情報を外す）を経由しない。aws と terraform に渡す値（region、Bucket 名、外す環境変数、
plan の log の1行目）は `$(call RUN_SCRIPT_HOST,tf-local,...)` で `scripts/tf-local` が返し、aws の出力は
`-identity` の引数で tf-local に渡す。tf-local は aws も terraform も起動しない。

`bootstrap-check-plan` は、保存した plan を `terraform show -json` で読み、`scripts/bootstrap-check` へ標準入力で
渡して段2-4 の期待値と突き合わせる。plan のパスと標準入力がホストのものなので `RUN_SCRIPT_HOST` で起動し、
`terraform show` は `env -i` で起動して資格情報を渡さない。`PARTIAL` は make の引数でだけ渡せる。

### 直下

`runner.mk`（起動の前置き）、`versions.mk`（版の写しの反映と照合）、`host-tools.mk`（ホストへの道具の導入）、
`git-isolation.mk`（一時のリポジトリで git を使うレシピを外側のリポジトリから切り離す）。
