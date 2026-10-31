# Policy Test

[ADR-0402](../../docs/adr/0402-policy-test-scope-and-verification.md) の Policy 層である。本リポジトリの ADR を読まなければ書けない invariant だけを持つ（同 決定1-3）。`make tf-policy` が実行し、`make tf-policy-canary` がその検査自体を検査する（[`.makefiles/terraform/policy.mk`](../../.makefiles/terraform/policy.mk)）。

## 入力

各 `usecases/<use-case>/reference/tests/policy.tftest.hcl` を mock provider で plan し、`terraform test -json -verbose` が出す `test_plan` から `provider_schemas` を除いたものである（同 決定11）。すべての reference の root がこのファイルを持たなければ、`make tf-policy` は落ちる。

policy の文書が参照する ARN は、`override_resource`（`override_during = plan`）で instance ごとに与える。与えないと文書が plan で unknown になり、`[PLAN-INPUT-04]` で落ちる。

`env/` の root は入力にしない。資格情報なしでは plan できないためで、実 provider の plan は CI の plan の経路（#185）が `terraform show -json` で与える。それまで `env/admin/identity` の値は Policy Test の外にある。

入力は `tmp/tf-policy/` に書かれる。mock の値だけを含み、artifact や Pull Request のコメントへは出さない。

## 宣言

[`../data/policy-plan.json`](../data/policy-plan.json) が1箇所に持つ（同 決定15）。

| キー | 中身 |
| --- | --- |
| `plan_format_version` | 読める入力の形の版。違えば `[PLAN-INPUT-02]` で落ちる |
| `documents` | policy の文書として読む resource の型と属性。表に無い型の文書は読まない。mock provider は表に無い computed な属性にも乱数を入れるので、属性の名前だけでは拾わない |
| `deployment_identity` | 信頼条件を見る role と OIDC provider の address。index key を除いた形で書く |

## Policy

| package | ID | 見るもの |
| --- | --- | --- |
| `plan.input` | `PLAN-INPUT-*` | 入力が0件、形の版が違う、`resource_changes` が空、文書が unknown、文書が JSON として読めない、文書が IAM の policy 文書の形でない |
| `plan.iam_action_wildcard` | `PLAN-IAM-*` | Allow の statement の Action の `*`・`<service>:*`（大文字小文字を区別しない。[ADR-0301](../../docs/adr/0301-least-privilege-policy.md) 決定8）と、同じ効果を持つ `NotAction`。Deny と部分 wildcard は見ない |
| `plan.deployment_identity_trust` | `PLAN-TRUST-*` | `deployment-identity` の role の信頼条件の形（[ADR-0602](../../docs/adr/0602-bootstrap-and-ci-authentication.md) 決定14・15、[deployment-identity/ADR-0101](../../usecases/deployment-identity/docs/adr/0101-trust-by-immutable-environment-subject.md)）。値は持たない |

各 Policy は、対象が0件なら落ちる（`PLAN-*-00`）。

## Policy を足すとき

- `<name>.rego` と `<name>_test.rego` を同じディレクトリに置く。規則は `deny contains` で書き、違反の ID（`[PLAN-<区分>-<番号>]`）を持たせる。テストは `test_violation_` と `test_pass_` で始まる名前の両方を持ち、各 ID をどこかのテストが名指しする。どれかが欠けると `make tf-policy` が落ちる（同 決定8・9）
- 見る resource の型と address は `../data/policy-plan.json` に足す
- Security の道具（[ADR-0501](../../docs/adr/0501-development-tooling-composition.md) 決定6）の既定のルールと重ならないことを、本リポジトリの書き方の文書で確かめる（ADR-0402 決定3）。確かめた結果は Pull Request に書く
