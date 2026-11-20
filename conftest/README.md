# conftest

Conftest が読む Rego と、その宣言の置き場所。検査ごとに1つの木を持ち、宣言はその木の中の `decl.json` に置く。
`make` は検査ごとに別の起動で、その木だけを `-p` に、その宣言のファイルだけを `--data` に渡す。
`-p` は `.rego` しか読まないので、宣言を木の中に置いても Rego の入力には混ざらない。

| 木 | 検査 | 入力 | 宣言 | 起動 |
| --- | --- | --- | --- | --- |
| [`hcl/infra_code/`](hcl/infra_code/README.md) | インフラ内コードの検出（[ADR-0103](../docs/adr/0103-infrastructure-code-gate.md)） | `scripts/hcl-input` が HCL のソースから作る JSON（[ADR-0501](../docs/adr/0501-development-tooling-composition.md) 決定5） | `hcl/infra_code/decl.json` | `make infra-code-gate` |
| [`hcl/resource_order/`](hcl/resource_order/README.md) | resource の作成の順序（ADR-0501 決定5。順序は当該ユースケースの ADR が決める） | `scripts/hcl-input` が HCL のソースから作る JSON（ADR-0501 決定5） | `hcl/resource_order/decl.json` | `make resource-order-check` |
| [`hcl/layer/`](hcl/layer/README.md) | 層の規則（[ADR-0102](../docs/adr/0102-use-case-centric-scope.md) 決定14・17・18） | HCL のソースと、`scripts/hcl-input` が HCL のソースから作る JSON（`data.hcl_input`。検査(3) の参照。ADR-0501 決定5） | `hcl/layer/decl.json` | `make layer-check` |
| [`hcl/root/`](hcl/root/README.md) | root が呼ぶ usecase の構成と backend 設定の不変条件（[ADR-0604](../docs/adr/0604-state-split-by-apply-role.md) 決定2-4・7・8） | `scripts/hcl-input` が HCL のソースから作る JSON（ADR-0501 決定5） | `hcl/root/decl.json` | `make root-check` |
| [`plan/`](plan/README.md) | Policy Test（[ADR-0402](../docs/adr/0402-policy-test-scope-and-verification.md)） | `terraform show -json`（ADR-0501 決定8） | `plan/decl.json` と、`make tf-policy` が Provider の schema から取り出す非推奨の引数の一覧（ADR-0402 決定18） | `make tf-policy` |

`hcl/lib/` は `scripts/hcl-input` が作る JSON を読むための共通部分（`hcl_input.rego`）で、検査ではない。
HCL を読む検査は `-p` にこれを足して渡す。`hcl/lib/fixtures/` と各検査の木の `fixtures/` は Rego のテストの
fixture（txtar）で、`*-verify` が `hcl-input fixtures` で JSON にして `--data` で渡す。層の検査の block の構造が
Conftest の hcl2 パーサの形を読む間は、その読み方が `hcl/layer/hcl2.rego` に在る（ADR-0501 決定5 の移行の一文）。
