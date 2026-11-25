# ADR-0001: 全アカウントの workload root から呼び、アカウントかリージョンに1つしか置けない安全側の設定だけを持ち、experimental で始める

- Status: Accepted
- Date: 2026-11-24
- Scope: account-baseline
- Related: repository-wide/ADR-0102, repository-wide/ADR-0204, repository-wide/ADR-0604, account-baseline/ADR-0101, account-baseline/ADR-0102, account-baseline/ADR-0103

## Context

Security Hub の AWS Foundational Security Best Practices（FSBP）と CIS AWS Foundations Benchmark には、resource ではなくアカウントかアカウント×リージョンを単位に評価するコントロールがある（S3.1・EC2.7・IAM.28・Config.1・Account.1・IAM.7・15・16・EC2.172・SSM.6・SSM.7・EMR.2）。対応する設定は、アカウントかリージョンに1つしか置けない。同じ設定を2つの usecase が宣言すると、互いに上書きして差分が出続ける。

AWS SRA は、Config と Access Analyzer をアカウントの onboarding と baselining で全アカウントに置くことを求め、EBS の既定の暗号化を「every account and Region」で有効にすることを求める。一方で、組織の trail・GuardDuty の組織の設定・Config の集約・組織の analyzer・Inspector・Security Hub は、委任管理者（security のアカウント）から持つ形を示す。

アカウントは管理・security・log-archive・sandbox・dev・stg・prd の7つである（`docs/v1.0-plan/scope.md` 第10.1節）。1つの env の root は identity root と workload root の2つで、identity root は bootstrap と統制の構成物を持つ usecase だけを呼ぶ（repository-wide/ADR-0604 (state-split-by-apply-role) 決定3・4）。

## Decision

1. このユースケースは、7つのアカウントのすべての workload root から呼ぶ。identity root からは呼ばない。`env` は7つの env の token だけを受ける。
2. 持つのは、アカウントかリージョンに1つしか置けない安全側の設定だけとする。
   - アカウントに1つ: S3 Public Access Block（4項目）、セキュリティの代替の連絡先、IAM の password policy
   - リージョンごと: EBS の既定の暗号化、外部アクセスの analyzer（type `ACCOUNT`）、AWS Config の記録、VPC Block Public Access（`block-bidirectional`）、SSM document の公開共有の `Disable`、SSM Automation のログの出力先の `CloudWatch`、EMR の block public access（22 だけを許す）
3. 組織の側の検知（組織の trail、GuardDuty、Config の集約、組織の analyzer、Inspector、Security Hub、検知の通知）は持たない。持ち主は `security-tooling` である。
4. env による違いは `env` の token から導き、機能のフラグを入力に置かない（repository-wide/ADR-0204 (no-generic-escape-hatch)）。
   - `sandbox`: AWS Config を置かない（account-baseline/ADR-0102 (config-recorder-except-sandbox)）
   - `admin`: IAM の password policy を置かない。管理アカウントの password policy は `organization-baseline` の break-glass が持つ
5. 出力を持たない。消費者が無い。通知に値する metric を出す resource を持たないので、通知の Topic を受けない（`docs/v1.0-plan/scope.md` 第7節）。
6. このユースケースは experimental で始める（repository-wide/ADR-0102 (use-case-centric-scope) 決定19-22）。免除されるのは公開契約の後方互換性だけである。
7. stable へ昇格する条件は、7つのアカウントの env の root から呼ばれ、sandbox の検証を通ったことである。sandbox の検証の形は README の「検証方法」が持つ。
8. 決定7 の条件が v1.0.0 の版を切るまでに満たされないときは、このユースケースを削除する。

## 検討した代替案

### 案A: 組織の側の検知も持ち、`env` が `security`・`admin` のときだけ合成する

公開契約の意味が env によって2つのアーキテクチャに分かれる（repository-wide/ADR-0102 (use-case-centric-scope) 決定3）。組織の側の入力（trail の鍵、Topic、data events の対象）は security と admin でしか使われない。CloudWatch Logs への配送の role を作るので、permissions boundary も要る。

### 案B: 各アカウントに trail と GuardDuty の detector を置く

組織の trail 1本と委任管理者の auto-enable の形（SRA）と食い違い、`audit-evidence` の `writers.cloudtrail_organization_trail` とも合わない。

### 案C: 設定ごとに一般 module を作る

消費者が1つで、一般 module の条件（repository-wide/ADR-0102 (use-case-centric-scope) 決定15）に当たらない。

### 評価

| 評価軸 | 案A | 案B | 案C | 採用案 |
| --- | --- | --- | --- | --- |
| Security | 中 | 低（記録される側が止められる） | 中 | 中 |
| Testability | 低（env ごとに別の合成） | 中 | 中 | 高 |
| Contract Clarity | 低 | 中 | 中 | 高 |
| Maintainability | 低 | 低 | 低（module が増える） | 高 |
| Scope Control | 低 | 低 | 中 | 高 |

評価軸の定義は repository-wide/ADR-0101 (architecture-principles) を参照する。

## 意図的に捨てるもの

- 1つの usecase で、アカウントの設定と組織の検知を一度に置くこと
- 利用者が env と無関係に、Config や password policy の有無を選ぶこと

## 保証範囲

- 保証すること: `env` が7つの token の外なら plan で落ちること。`sandbox` で Config を置かず、`admin` で password policy を置かず、ほかの env では両方を置くこと。account-baseline が持つアカウントかリージョンに1つの型を、ほかのユースケース・env・一般 module が宣言すれば層の検査で落ちること。reference の plan の間で password policy の値が `organization-baseline` の break-glass と食い違えば Policy Test で落ちること
- 保証しないこと: このユースケースが実際に7つのアカウントの workload root から呼ばれること（呼び出す root の配置が決める）

## 検証方法

- Contract Test: `tests/validation.tftest.hcl` の `unknown_env` が、ほかの `env` を拒むことを確かめる。`tests/contract.tftest.hcl` の `workload_account`・`sandbox_has_no_config`・`management_account_has_no_password_policy` が、env ごとの合成の件数を確かめる
- Static Analysis（`make layer-check`）: `LY401` が、アカウントかリージョンに1つの型が持ち主の外で宣言されることを落とす（password policy は `organization-baseline` も認める）。
- Policy Test（`make tf-policy`）: `PLAN-ACB-06` が、reference の plan の間で password policy の値（`conftest/plan/decl.json` の `account_baseline.password_policy_keys`）が食い違うことを落とす。比べる相手（層の宣言 `conftest/hcl/layer/decl.json` の `singletons` が password policy の持ち主とする usecase の password policy）が入力に無いときと、宣言の属性で値が1つも読めないときも落とす
- 昇格の条件（決定7）は実環境の観測で確かめる。どのテストも見ない

## 影響

各アカウントの workload root と、その apply の role の権限が要る。管理アカウントの password policy の持ち主は `organization-baseline` のままである。

メンバーの apply の role は password policy を書ける。deployment-identity/ADR-0201 (enumerated-permissions) 決定5 の `DenyGovernanceChange` は、password policy の更新を管理アカウントでだけ拒む。

## 見直し条件

- 決定7 の条件を満たしたとき（stable へ昇格する）
- 決定8 の期限に達したとき
- アカウントかリージョンに1つの安全側の設定を、組織の declarative policy で置く形に移すとき
