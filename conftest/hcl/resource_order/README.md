# resource の作成の順序

宣言した module の resource が、`depends_on` に宣言した resource を持つことを、HCL のソースで検査する
（[ADR-0501](../../../docs/adr/0501-development-tooling-composition.md) 決定5 がソースの構造の規約として Conftest に
割り当てる、resource の作成の順序）。順序は `terraform show -json` の出力に現れないので（同 決定5）、Policy 層
（`conftest/plan/`）では見ない。どの順序を守るかは、宣言の `adr` が名指すユースケースの ADR が決める。

起動は `make resource-order-check` で、CI では Pull Request の Static Analysis の層の job（`terraform-gates.yaml` の
`tf-lint`）が TFLint の後に走らせ、apply する木では `terraform-apply.yaml` の `apply-resource-order-check` が
走らせる。apply の job はその成功を待つ（[`.github/workflows/README.md`](../../../.github/workflows/README.md) の「apply の job」）。検査そのものの検査は、Rego のテスト（`resource-order-verify`）と、`scripts/hcl-input`・`scripts/hcl-tally` などの Go のテストである。

| 段 | make | 判定するもの |
| --- | --- | --- |
| Rego のテスト | `resource-order-verify` | 各 deny の ID が違反と非違反の両方のテストを持ち、テストがすべて走って通ったこと（`infra-code-gate rego-tests`） |
| 検査 | `resource-order-detect` | 走査の root（`usecases` `modules` `env`）の下を [`scripts/hcl-input`](../../../scripts/README.md) が読んだ JSON 1つを入力として掛ける。入力の `.tf` と `.tf.json` の集合と、構文として読んだファイルとトップレベルの block の数を、[`scripts/hcl-tally`](../../../scripts/README.md) の独立の数えと突き合わせ、評価した規則の数を `deny contains` の数と突き合わせる（`tf-local policy-result`） |

## 失敗の読み方

| ID | 意味 | 直し方 |
| --- | --- | --- |
| `RO001` | 宣言の `rules` が空か、項目が `module` / `resource` / `after` / `adr` の4つの空でない文字列を持たない | 宣言を直す |
| `RO002` | 走査の root に `.tf` も `.tf.json` も無い。または入力（hcl-input）が読んだファイルと `hcl-tally` の一覧が違う | 一覧の差のファイルを実体のファイルにするか、走査の root から外す。symlink は hcl-input が先に落とす |
| `RO003` | 宣言した module のディレクトリに `.tf` が無い | module を動かしたなら宣言の `module` を直す |
| `RO004` | 宣言した resource が module の中にちょうど1つ無い | address を変えたなら宣言を直す |
| `RO005` | resource の `depends_on` に宣言した resource が無い | `depends_on` を戻す。順序を変えるなら `adr` の決定を先に変える |
| `RO006` | 走査の root に JSON の構成（`.tf.json`）が在る。検査はそれを解釈しない（ADR-0501 決定5、ADR-0103 決定11） | HCL（`.tf`）で書く |
| `RO007` | 入力（hcl-input の JSON）の形が違う。または構文として読んだファイルの集合とトップレベルの block の数が独立の数と違う。崩れた入力の上では、それを読む規則を評価しない | `make resource-order-check` から起動する。block の数の差は、ヒアドキュメントやブロックコメントの行頭に block の頭の形が在る |

## 宣言

[`decl.json`](decl.json) が持つ。`module` は走査の root からのディレクトリ、`resource` と `after` は module の中の
`<型>.<名前>`（index key を持たない）、`adr` はその順序を決めた ADR である。

## 保証しないこと

- `depends_on` 以外の参照から Terraform が導く依存。宣言した依存が明示されていることだけを見る。`depends_on` の
  要素は、段がすべて属性の参照だけを address として読む
- 宣言に無い module と resource の順序
