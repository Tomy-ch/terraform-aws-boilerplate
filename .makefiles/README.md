# .makefiles

`make` のターゲットの定義。リポジトリ直下の `makefile` が、ここの `.mk` を `include` して組み上げる。

**ターゲットの一覧はここが持たない。** 一覧は `make help` が出す —— この文書が一覧を写せば、
次にターゲットが増えた日に片方だけが古くなる。ここが持つのは、一覧から読めないことだけである。

## どこに何が在るか

| ディレクトリ | 関心事 |
| --- | --- |
| `docker/` | Dockerfile と compose の検査、image digest の固定 |
| `github/` | workflow と action の検査、commit message、egress の SSOT、action の固定、ベースブランチの解決 |
| `github/operation/` | リリースブランチ・リリースタグ・リポジトリ初期化の操作 |
| `github/setting/` | ブランチ保護・ラベル・リポジトリ設定の適用 |
| `go/` | 運用機構（`scripts/`）の Go の整形・Lint・テスト |
| `markdown/` | Markdown の体裁、ADR の構造、スキルの参照の実在 |
| `security/` | secret scan、脆弱性・設定の走査、供給網のクールダウン |
| 直下 | `runner.mk`（起動の前置き）、`versions.mk`（版の写しの反映と照合）、`host-tools.mk`（ホストへの道具の導入） |

各ツールが何を検査するかは [`scripts/README.md`](../scripts/README.md) が持つ。ここはその起動だけを持つ。

## `make help` に出る条件

`help` は `.PHONY: <名前> ## <説明>` という形の行だけを集める。

- `## <説明>` を持たない `.PHONY` はターゲットとして機能するが、一覧には出ない（`help` 自身がそう）
- `.PHONY` の**直下に続く** `##` のブロックは `help` が読まない。あれは読み手のための散文である
- **`makefile` が `include` していない `.mk` は、何も定義しない。** `.mk` を新設したら `include` を
  同じ変更で足す。足し忘れは静かに起きる —— ファイルは在るのに `make` が知らない状態になる

だから「このターゲットは在るか」の答えは `.mk` を読むことではなく、`make help` を叩くことである。

## 検査の起動

検査の定義は1箇所に置き、実行環境の差は起動の前置きだけで表す（[0503](../docs/adr/0503-tool-execution-form.md) 決定8-9）。
前置きは `runner.mk` が組み立て、`RUNNER_MODE`（既定 `container`、CI は `host`）で切り替わる。

| 形 | 何を起動するか | 使う場面 |
| --- | --- | --- |
| `$(GO_TOOL)` | Go 側のツールランナー | `mise.toml` が pin した Go 製の既製品（golangci-lint、trivy、hadolint ほか） |
| `$(NODE_TOOL)` | npm 側のツールランナー | markdownlint-cli2、commitlint |
| `$(call RUN_SCRIPT,<名前>,<引数>)` | `scripts/<名前>` をビルドして起動 | このリポジトリが書いた検査 |
| `$(call RUN_SCRIPT_HOST,<名前>,<引数>)` | 同上、ただしコンテナを経由しない | **提供段のコンテナに無い前提を要するもの** —— version manager 自身を入力にする、`docker` を呼ぶ、`git` / `gh` がホストの資格情報を使う |

最後の1つが要る理由は呼び出し側ごとに違い、いずれも「ツールランナーが提供できない前提」に帰着する
—— 提供段に version manager を残さない形でイメージを組んでいる（[0503](../docs/adr/0503-tool-execution-form.md) 決定11）、
ツールランナーに `docker` が入っていない、`git` / `gh` がホストの資格情報を使う。**どれに当たるかは
呼び出し元の `.mk` のコメントが持つ。**

**この表は検査の起動を尽くす。操作系はここに入らない** —— `github/operation/` の
`RELEASE_TOOL` / `REPO_SETUP` は `go -C scripts run` を直接呼び、`RUNNER_MODE` を経由しない。
取り消しの効かない操作をホストの資格情報で行うためで、理由は各 `.mk` のコメントが持つ。

`RUN_SCRIPT` がビルドしてから起動するのは、`scripts/` がモジュールルートで `go -C scripts run` が
作業ディレクトリをそこへ移す一方、各ツールはリポジトリ直下を基準にパスを解決するためである。

## ローカルのフックとの関係

`.lefthook.yaml` は `make <target>` を呼ぶだけで、検査を定義しない。権威は CI にあり、フックは
速いフィードバックのために在る（[0501](../docs/adr/0501-development-tooling-composition.md) 決定26）。
**フックでのみ成立する検査を作らない**（[0702](../docs/adr/0702-repository-operations-substrate.md) 決定19）。
