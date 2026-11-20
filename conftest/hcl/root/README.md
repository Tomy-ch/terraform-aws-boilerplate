# root の構成と backend 設定の不変条件

[ADR-0604](../../../docs/adr/0604-state-split-by-apply-role.md) 決定2-4 の root が呼ぶ usecase の構成と、同 決定7・8 の
backend 設定の不変条件を、HCL のソースで検査する（同 ADR の検証方法の Static Analysis。
[ADR-0501](../../../docs/adr/0501-development-tooling-composition.md) 決定5 がソースの構造の規約として Conftest に割り当てる）。
何を禁じるかは ADR が持ち、ここには再掲しない。

起動は `make root-check` で、CI では Pull Request の Static Analysis の層の job（`terraform-gates.yaml` の `tf-lint`）が
走らせ、apply する木では `terraform-apply.yaml` の `apply-root-check` が走らせる。apply の job はその成功を待つ
（[`.github/workflows/README.md`](../../../.github/workflows/README.md) の「apply の job」）。検査そのものの検査は
`make root-check-test` である。

| 段 | make | 判定するもの |
| --- | --- | --- |
| Rego のテスト | `root-verify` | 各 deny の ID が違反と非違反の両方のテストを持ち、テストがすべて走って通ったこと（`infra-code-gate rego-tests`） |
| 検査 | `root-detect` | 走査の root（`usecases` `env`）の下を [`scripts/hcl-input`](../../../scripts/README.md) が読んだ JSON 1つを入力として掛ける（段は `.makefiles/terraform/hcl-input.mk`）。入力の `.tf` と `.tf.json` の集合を `find` の一覧と、構文として読んだファイルとトップレベルの block の数、ファイルごとの `module` block の数を `grep` が行頭で数えた数と突き合わせ、評価した規則の数を `root.rego` の `deny contains` の数と突き合わせる（`tf-local policy-result`） |

`tf-local policy-result` は `root.rego` の行頭の `deny contains` を数える。規則は字下げせずにこのファイルへ置く。

## 失敗の読み方

| ID | 意味 | 直し方 |
| --- | --- | --- |
| `RT001` | 宣言（`decl.json`）の形が違う。`management_env` が env の名前でない、`usecases` がオブジェクトでない、値が `bootstrap` と `control` のどちらでもない、`bootstrap` の usecase が無い、未知の鍵を持つ | 宣言を直す |
| `RT002` | 構成ファイルか root が0件。入力（hcl-input）が読んだファイルと `find` の一覧が違う。ファイルの `module` block の数が独立の数え方と違う | 差のファイルを実体のファイルにするか、行頭に `module "` が来ないように書く。symlink は hcl-input が先に落とす |
| `RT003` | JSON の構成（`.tf.json`）か、root の中の override のファイル（`override.tf`、`*_override.tf`）が在る。検査はどちらも解釈しない | HCL（`.tf`）の元のファイルに書く |
| `RT004` | 宣言の usecase の `usecases/<名前>/` に `.tf` が無い。`management_env` の identity root に `.tf` が無い | 宣言を直す |
| `RT005` | 入力（hcl-input の JSON）の形が違う。または構文として読んだファイルの集合とトップレベルの block の数が独立の数と違う（ヒアドキュメントやブロックコメントの行頭の block の頭の形）。崩れた入力の上では、それを読む規則を評価しない | `make root-check` から起動する。行頭に block の頭の形が来ないように書く |
| `RT101` | identity root（`env/<env>/identity/`）が、宣言した usecase 以外を呼ぶ（決定3） | workload root（`env/<env>/`）から呼ぶ |
| `RT102` | 管理アカウント以外の identity root が、`control` の usecase を呼ぶ（決定3） | 管理アカウントの identity root から呼ぶ |
| `RT103` | workload root が、宣言した usecase を呼ぶ（決定4） | identity root から呼ぶ |
| `RT104` | root の module の `source` が、走査の root の中を指す相対パスのリテラルでない。呼ぶ先を判定できない | リポジトリ内の相対パスにする（層の検査の `LY201`・`LY202` も同じ入力で落ちる） |
| `RT201` | root の backend が1つでない、`backend.tf` に無い、`s3` でない、`cloud` block が在る | root の `backend.tf` に `backend "s3"` を1つ置く |
| `RT202` | backend の `key` が決定7 の値（identity root は `identity/terraform.tfstate`、workload root は `workload/terraform.tfstate`）のリテラルでない | 決定7 の値を書く |
| `RT203` | `use_lockfile` が真偽値のリテラル `true` でない（無い、`false`、文字列、式）（決定8） | `use_lockfile = true` を書く |
| `RT204` | `encrypt` が真偽値のリテラル `true` でない（決定7） | `encrypt = true` を書く |
| `RT205` | `dynamodb_table` を書く（値を問わない） | 消す。lock は lock file で取る |
| `RT206` | `kms_key_id` を書く（値を問わない。決定7） | 消す。鍵は init の `-backend-config` で渡す |

## root の決め方

root はディレクトリの位置で決める。`env/<env>/` を workload root、`env/<env>/identity/` を identity root とし、その下の
`.tf` を読む。名前が `.` で始まるディレクトリと `tests` という名前の env も、位置が合えば root として読む。

`env/` の下でそれ以外の位置（`.terraform` の下を除く）に `.tf` を置くこと（決定5 の配置）と、各 root に plan・apply・
lock の解除・lock を残す job があることは、`make oidc-job-lint` が見る（ADR-0501 決定5）。

`module` の `source` は、ディレクトリと結んで `.` と `..` を字面で解いた後のパスで判定する（`lib.hcl_input.module_target`。
層の検査と同じ解き方）。解いた先が `usecases/<名前>` で、`<名前>` が宣言に在れば、その種類の usecase を呼ぶとみなす。
env の root が `usecases/<名前>` 以外（usecase の下のディレクトリ、別の root、ローカルの module、実在しない先）を呼ぶことは、
層の検査の `LY203`・`LY204` が落とす（ADR-0102 決定17）。`RT101`-`RT103` はその検査を通った入力を前提にする。
apply の job と Pull Request の必須検査が両方の検査を走らせることは、`make oidc-job-lint` が見る。

## 宣言

[`decl.json`](decl.json) の `root_decl` が持つ。

- `management_env`: 管理アカウントの env の名前。統制の usecase を呼べる identity root は `env/<management_env>/identity/`
  だけである
- `usecases`: usecase のディレクトリ名から、それが持つ構成物の種類（[ADR-0602](../../../docs/adr/0602-bootstrap-and-ci-authentication.md)
  決定1 の `bootstrap` か `control`）へ。identity root が呼べるのは、ここに在る usecase だけである

ADR-0602 決定1 は構成物の種類を定義し、どの usecase がどれに当たるかはこの宣言が持つ。bootstrap か統制の構成物を
持つ usecase を足す変更は、同じ変更でこの宣言に足す。宣言に在って実在しない usecase は `RT004` で落ちる。

## HCL の読み方

読むのは block の種類（`module`、`terraform` の `backend` と `cloud`）と、`module` の `source` と `backend` の属性の
リテラルの値だけである。式の中の参照は読まない。リテラルでない値（補間を含むテンプレート、変数、関数の呼び出し）は、
中身を読まずにリテラルでないものとして扱い、`source` なら `RT104`、`key`・`use_lockfile`・`encrypt` なら `RT202`-`RT204`
で落とす。hcl-input の JSON の形に触れるのは `root.rego` の「入力の読み方」の節と `lib.hcl_input` だけである。

## 保証しないこと

- 宣言の正しさ。bootstrap か統制の構成物を持つのに宣言に無い usecase は、workload root から呼んでも `RT103` に当たらず、
  identity root から呼ぶと `RT101` で落ちる。各 usecase の README の責務境界と宣言の一致は機械で見ない
- init の実際の引数。`-backend-config` による `key` などの上書きは、CI と手元の `make` の init が `bucket` と `kms_key_id`
  だけを渡すこと（`scripts/tf-local` のテスト）で閉じており、人が手で打つ `terraform init` は見ない
- `kms_key_id` の値が空でないこと。CI の init が state の鍵と照合した値を渡すことは `scripts/tf-local` の `stateKey` と
  そのテストが見る。鍵が無い間の bootstrap の手元の init（[`docs/setup/bootstrap.md`](../../../docs/setup/bootstrap.md)
  段2-4 の `-missing sse-s3`）は鍵を渡さない
- 行頭の `module "` とトップレベルの block の頭を数える独立の経路は、ヒアドキュメントやブロックコメントの中の行も
  数える。そのときは `RT005` か `RT002` で落ちる（通す向きには壊れない）
- 走査の root の外の `.tf`。root の外に構成ファイルを置くことは `make infra-code-gate` が落とす
