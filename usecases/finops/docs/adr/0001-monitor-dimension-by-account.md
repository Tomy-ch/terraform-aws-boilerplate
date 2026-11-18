# ADR-0001: 予算と異常の検知を全アカウントに置き、monitor の次元を管理アカウントとメンバーアカウントで分ける

- Status: Accepted
- Date: 2026-11-19
- Scope: finops
- Related: repository-wide/ADR-0102, repository-wide/ADR-0202, repository-wide/ADR-0204, repository-wide/ADR-0206, repository-wide/ADR-0207, finops/ADR-0101

## Context

アカウントは管理・security・log-archive・sandbox・dev・stg・prd の7つである。通知の Topic は各アカウントの `ops-notification` が持ち、アカウントを越えて送らない。AWS Budgets は別のアカウントの Topic へ通知できない。

AWS Well-Architected の COST02-BP05 は、アカウントごとにアカウント全体の支出を見る予算を置くことを、COST01-BP05 は、ワークロードのすべてのアカウントに AWS Budgets を置くことを勧める。Cost Anomaly Detection の User Guide は、組織の主たる次元を AWS managed monitor で覆い、AWS services monitor を並べて持つことを勧める。メンバーアカウントごとに見る monitor（`LINKED_ACCOUNT`）は管理アカウントでしか作れない。AWS managed monitor は、管理アカウントに2つ（うち AWS services monitor は1つ）、メンバーアカウントに1つまでである。

管理アカウントには、Cost Explorer を有効にした時点の AWS services monitor と email の subscription が在り、`docs/setup/scripts/check-account.sh` がその存在を確かめる。AWS Organizations のベストプラクティスは、管理アカウントに置くものを、管理アカウントでしかできないことに限ることを勧める。

## Decision

1. 7つのアカウントのすべてに `finops` を置く。予算と異常の通知は、そのアカウントの Topic へ送る。
2. 予算はそのアカウントの費用の全体を見る。管理アカウントの予算は組織の費用の全体を見る。予算を費用の filter で絞らない。
3. monitor は AWS managed monitor を1つ置く。次元は、管理アカウント（`env = "admin"`）ではメンバーアカウントごと（`LINKED_ACCOUNT`）、ほかのアカウントでは AWS のサービスごと（`SERVICE`）とする。次元は `env` から導き、入力にしない。
4. 管理アカウントに bootstrap の前提として在る AWS services monitor と、その email の subscription を、`finops` は扱わない。取り込まず、作り直さない。
5. 予算をタグで切ったワークロードの単位に持つこと（COST01-BP05 の後半）を採らない。公式推奨から外れるので、下の「公式推奨からの逸脱」に記録する（repository-wide/ADR-0207 (default-value-policy) 決定11）。

## 検討した代替案

### 案A: 管理アカウントに集める

管理アカウントの予算を、メンバーアカウントごとに `LinkedAccount` の filter で置く。メンバーアカウントの Topic へは通知できず（Budgets は越境の SNS を受けない）、アカウント ID の入力が要る。COST02-BP05 の「アカウントごとの予算」は、そのアカウントの外に置かれる。

### 案B: メンバーアカウントだけに置く

組織全体の予算と、メンバーアカウントごとの異常の検知（管理アカウントでしか作れない）を失う。

### 案C: 管理アカウントの AWS services monitor を `finops` が持つ

自動で作られた monitor を人が消してから `finops` が作るか、import で取り込む。消してから apply までの間は検知が止まり、bootstrap の手順と `check-account.sh` の書き換えが要る。import は repository-wide/ADR-0202 (internal-topology-as-implementation-detail) 決定7 の範囲の外である。

### 評価

| 評価軸 | 案A | 案B | 案C | 採用案 |
| --- | --- | --- | --- | --- |
| Security | 中 | 中 | 中 | 中 |
| Testability | 低（アカウント ID の組が要る） | 高 | 低（手順が要る） | 高 |
| Contract Clarity | 低（アカウント ID の入力） | 高 | 中 | 高 |
| Maintainability | 低 | 高 | 低 | 高 |
| Scope Control | 低（越境の通知が要る） | 中（組織全体を見ない） | 低 | 高 |

評価軸の定義は repository-wide/ADR-0101 (architecture-principles) を参照する。

## 意図的に捨てるもの

- 管理アカウント自身のサービスごとの異常を、Topic で受けること。管理アカウントの AWS services monitor は email の subscription だけを持つ
- 予算を、タグで切ったワークロードの単位で持つこと（決定5）

## 保証範囲

- 保証すること: `env = "admin"` の monitor の次元が `LINKED_ACCOUNT` で、ほかの `env` では `SERVICE` であること。予算が filter を持たないこと
- 保証しないこと: 管理アカウントの予算と `LINKED_ACCOUNT` の monitor が、管理アカウント自身の費用を含むこと。メンバーアカウントに手で作った AWS services monitor が在るとき、apply が quota で落ちないこと

## 検証方法

- Unit Test: `tests/contract.tftest.hcl` が `env` ごとの monitor の次元と名前を、`internal/cost-anomaly/tests/anomaly.tftest.hcl` が `scope` ごとの次元と、AWS managed monitor であることを mock の plan で確かめる
- Policy Test（`make tf-policy`）: `PLAN-FINOPS-04` が、予算の `cost_filter`・`filter_expression` が空でないか plan で決まらない入力を落とす

## 影響

管理アカウントの workload root と CI の role に、Budgets と Cost Explorer の権限が要る。管理アカウントの managed monitor の枠は2つとも埋まる。

## 公式推奨からの逸脱

- 外れる推奨: AWS Well-Architected COST01-BP05 の「Set a budget for the overall account spend, and a budget for the workload by using tags」の後半（ワークロードの単位の予算）
- 外れる理由: ワークロードの単位で予算を切るには、予算の filter（`cost_filter`・`filter_expression`）に、タグの名前と値を Provider と API の表現のまま渡すことになる（repository-wide/ADR-0204 (no-generic-escape-hatch)）。タグを cost allocation tag として有効にする操作も、このユースケースの外にある
- ユースケース上の制約: 1つのアカウントに1つの env だけを置く（`docs/v1.0-plan/scope.md` 第10.1節）。アカウントの費用の全体の予算が、その env の予算になる
- 影響: cost では、1つのアカウントの中の部分ごとの超過を予算では検知しない。サービスごとの異常は `SERVICE` の monitor が見る。security と availability への影響は無い
- 代替案: タグの名前と値を型付きの入力で受けて filter を組む案。cost allocation tag の有効化を前提に置くことになり、採らない
- 見直し条件: 1つのアカウントに複数のワークロードを置く構成を supported にしたとき。タグの規約が型付きの入力として決まったとき

## 見直し条件

- AWS が、メンバーアカウントで `LINKED_ACCOUNT` 以外の追加の AWS managed monitor を許したとき
- AWS services monitor の customer managed の形を AWS が許したとき
- 管理アカウントの AWS services monitor を、bootstrap の構成物から外すとき
