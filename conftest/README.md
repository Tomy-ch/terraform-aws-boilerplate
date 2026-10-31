# conftest

Conftest が読む Rego と、その宣言の置き場所。検査ごとに1つの木を持ち、宣言はその木の中の `decl.json` に置く。
`make` は検査ごとに別の起動で、その木だけを `-p` に、その宣言のファイルだけを `--data` に渡す。
`-p` は `.rego` しか読まないので、宣言を木の中に置いても Rego の入力には混ざらない。

| 木 | 検査 | 入力 | 宣言 | 起動 |
| --- | --- | --- | --- | --- |
| [`hcl/infra_code/`](hcl/infra_code/README.md) | インフラ内コードの検出（[ADR-0103](../docs/adr/0103-infrastructure-code-gate.md)） | HCL のソース（[ADR-0501](../docs/adr/0501-development-tooling-composition.md) 決定5） | `hcl/infra_code/decl.json` | `make infra-code-gate` |
| [`plan/`](plan/README.md) | Policy Test（[ADR-0402](../docs/adr/0402-policy-test-scope-and-verification.md)） | `terraform show -json`（ADR-0501 決定8） | `plan/decl.json` | `make tf-policy` |

`hcl/lib/` は HCL のソースを Conftest の hcl2 パーサが返す形のまま読むための共通部分で、検査ではない。
HCL を読む検査は `-p` にこれを足して渡す。
