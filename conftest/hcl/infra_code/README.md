# インフラ内コードの検出

起動は `make infra-code-gate` で、CI の required context も同じ名前である。中身は3つの段に分かれる。

| 段 | make | 判定するもの |
| --- | --- | --- |
| Rego のテスト | `infra-code-verify` | 各 deny が違反する入力と違反しない入力の両方で検証されていること（ADR-0402 決定7-9 の規律） |
| 宣言の突き合わせ | `infra-code-decl-check` | ソースの所在の実体、参照する ADR、言語の required context、code owner の宣言、required の宣言 |
| 検出 | `infra-code-detect` | 入力の形と独立の数（find の一覧・行頭の block の数）、型の分類、コードを実行させる構成要素と許可リストの照合、path、規則が評価されたこと |

HCL のソースから判定できるものはこの木の Rego が持ち、ファイルシステムと Markdown と他の宣言を
読むものは [`scripts/infra-code-gate`](../../../scripts/README.md) が持つ。Rego はファイルシステムを見られず、
Conftest は Markdown を読まないためである（ADR-0103 決定11）。

入力は [`scripts/hcl-input`](../../../scripts/README.md) が走査の root の下の通常のファイルすべてから作る JSON 1つで
（ADR-0501 決定5）、名前空間 `infra_code` がそれを1度に評価する。許可リストの項目がどの構成要素にも当たらない
ことは、全ファイルを見ないと決まらない。入力を作る段は `.makefiles/terraform/hcl-input.mk` が持ち、hcl-input が
失敗すれば Conftest を起動しない（`❌ hcl-input:` で始まる行が名指すファイルを直す）。

## 失敗の読み方

deny のメッセージは固定の ID で始まり、何を直すかを含む。

| ID | 意味 | 直し方 |
| --- | --- | --- |
| `IC001` | 構成ファイルでも未対応の入力でもなく、除外の宣言（[`input.rego`](input.rego) の `.md` と `.terraform.lock.hcl`）にも当たらないファイルが走査の root に在る | 走査の root から外す。外せないなら `input.rego` の除外に足す |
| `IC002` | JSON の構成（`.tf.json` 等）か `.tfquery.hcl` | HCL で書く。検出はこれらを解釈しない |
| `IC003` | 解釈できないトップレベルのブロック | 検出がそのブロックを読めるようにしてから使う |
| `IC004` | 宣言に未知のキーがある。型の行の `executes_code` が `true` / `false` でない。許可リストの項目に `id` か `owner` が無い。宣言そのものが無い | キーの綴りを直す / 足りないキーを書く |
| `IC005` | 入力（hcl-input の JSON）の形が違う。または読んだファイルの集合が find の一覧と、ファイルごとのトップレベルの block の数が行頭の数えと違う。崩れた入力の上では、それを読む規則を評価しない | `make infra-code-gate` から起動する。行頭の数えとの差は、ヒアドキュメントやブロックコメントの行頭に block の頭の形が在る |
| `IC010` | 型の分類に無い型 | [`decl.json`](decl.json) の `types` に1行足す |
| `IC011` | 許可リストに無いインフラ内コード | `allowlist` に項目を足す。本リポジトリ所有なら ADR-0103 決定13 の ADR を書く |
| `IC012` | 決定9 が決定5 の側に置く型を、そうでない側に分類している | `executes_code` を `true` にする |
| `IC013` / `IC014` | provider の分類に無い provider / source がリテラルでない。`required_providers` の source は、provider ブロックが無くても対象になる | `providers` に1行足す / source をリテラルにする |
| `IC020` | 許可リストの項目がどのインフラ内コードにも当たらない | 項目を消す |
| `IC030`-`IC033` | 許可リストの項目の path の違反（決定15） | メッセージのとおり |
| `IC034` | 許可リストの項目が、コードをリストで渡す引数（`external` の `program` 等）を持つ | 許可リストには載せられない。どの要素がコードかを決められず、path を検査できないため |

`IC010` と `IC011` の直し方は宣言を足すことだが、宣言の各行は code owner の review の対象である
（ADR-0103 決定14）。

## 宣言

[`decl.json`](decl.json) が1箇所に持つ（ADR-0103 決定12）。そのパスは `.makefiles/terraform/infra-code.mk` の
`IC_DECL` だけが持ち、Conftest の `--data` と `infra-code-gate decl` の `-decl` へ同じファイルを渡す。
`--data` にディレクトリを渡すと、そこに在る他のファイルも深くマージされて検出の入力に混ざる。

## 宣言の書き方

`types` のキーは `<種別>.<型>` で、種別は `resource` / `data` / `ephemeral` / `action` である。
`executes_code` が `true` の型は、引数の有無に依らず、現れた構成要素をすべて検出する。
`path_args` は本リポジトリ内のファイルを指す引数、`body_args` はコードの本文を持つ引数で、
どちらも許可リストの項目の path の判定（決定15）に使う。

`providers` のキーは source address（`registry.terraform.io/<namespace>/<name>`）である。`command_blocks` には
コマンドを実行させるブロックの位置を `.` 区切りで書く。任意の深さの `exec` ブロックは、ここに書かなくても
検出する。

許可リストの項目の `id` は `<宣言した module のディレクトリ>#<種別>.<型>.<名前>` である。provider は
`<module のディレクトリ>#provider.<名前>[.<alias>]`、`.tftest.hcl` の provider は `<ファイル>#provider.<名前>`、
provisioner を持つ `removed` ブロックは `<module のディレクトリ>#removed.<from のアドレス>` と書く。
module が何度呼ばれても、項目は1つで済む。

| `owner` | 持つキー |
| --- | --- |
| `repository` | `source`（ソースの所在）、`language`、`adr`（`<scope>/<slug>`） |
| `external` | `repository`（`<host>/<owner>/<repo>`） |

本リポジトリ所有の項目を足すときは、`source` の行を [`.github/CODEOWNERS`](../../../.github/CODEOWNERS) にも足す。
`language` は `languages` に在り、その `contexts` は required の宣言に在る必要がある（ADR-0103 決定18）。

インラインのコード（CloudFront Functions の `code` 等）を持つ項目は、`source` に宣言したファイルそのものを書く。

## 値の読み方

`path_args` と `body_args` の値は、hcl-input が式の種類（`conftest/hcl/lib/hcl_input.rego`）で渡し、[`allowlist.rego`](allowlist.rego)
の `category` が4つに分ける。文字列のリテラル（インラインのコードか path）、式（テンプレート・関数の呼び出し・
参照・その他。`file()` 系の呼び出しなら path）、リスト（`IC034`）、それ以外（`IC030`）である。式の文字列は読まない。

## 保証しないこと

- override のファイルによる合成を再現しない。ブロックごとに検出するので、override のファイルが足した
  provisioner も検出する。合成した後の引数の値は見ない
- `path_args` と `body_args` の網羅。引数の追加を伴う provider の更新で、型の行を見直すのは review である
  （ADR-0103「保証しないこと」）
- 本リポジトリ所有のコードのソース、依存ライブラリ、ビルド手順がリポジトリ内に在ること（決定17）
- `user_data` の内容が起動処理に限られること（決定19）
- module の `source` が指す先。検出はリポジトリ内の HCL を読むので、リポジトリの外から取り込む module の中身は
  見ない。`source` をリポジトリ内の相対パスに限るのは層検査（ADR-0102 決定17）である
- 静的に確定する path として扱うのは、文字列のリテラルと、先頭だけが `path.module` の参照で残りが文字列の
  テンプレートと、それを第1引数に取る `file()` 系の関数（後の引数は問わない）だけである。それ以外
  （`data.archive_file` の出力を含む）は落とす
