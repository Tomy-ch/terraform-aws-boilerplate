# ADR-0001: 管理アカウントに、IAM を変更できる CI の role を置く

- Status: Accepted
- Date: 2026-10-25
- Scope: deployment-identity
- Related: repository-wide/ADR-0207, repository-wide/ADR-0602, repository-wide/ADR-0604, repository-wide/ADR-0301, state-backend/ADR-0001

## Context

bootstrap は管理アカウントから始まり、OIDC provider と plan / apply / apply-identity の role をそのアカウントに置く（repository-wide/ADR-0602 (bootstrap-and-ci-authentication) 決定1・4・13）。管理アカウントの identity root は apply-identity の role で apply する（repository-wide/ADR-0604 (state-split-by-apply-role) 決定3）。したがって apply-identity の role は、管理アカウントの中で IAM の role と policy を変更できる。

AWS Organizations のベストプラクティスは、管理アカウントを、管理アカウントでなければならない作業にだけ使い、resource を他のアカウントへ置くことを勧める。SCP は管理アカウントの主体に効かない。state Bucket の配置は state-backend/ADR-0001 (management-account-placement) が記録したが、CI の role は記録していない。本 ADR はこの推奨から外れる配置を記録する（repository-wide/ADR-0207 (default-value-policy) 決定11）。

## Decision

1. 管理アカウントに、OIDC provider と plan / apply / apply-identity の3つの role を置く。
2. 3つの role の権限は、管理アカウントの bootstrap の構成物と state に要る分に限る（deployment-identity/ADR-0201 (enumerated-permissions)）。管理アカウントの workload の usecase のための権限は、その usecase を足す変更が apply の role へ足す。

## 検討した代替案

### 案A: 委任管理者アカウントに CI の role を置く

管理アカウントの変更を、別のアカウントの CI の role から cross-account で行う。管理アカウントへの書き込み経路が常設になり、repository-wide/ADR-0301 (least-privilege-policy) 決定17 の明示的なユースケースが1つ増える。フェーズ0 の時点では管理アカウントの他にアカウントが存在せず、成立しない。

### 案B: 管理アカウントの変更をすべて人のローカルからの apply にする

CI の role を管理アカウントに置かない。repository-wide/ADR-0602 決定9 の例外が、初回と回復ではなく常時になり、repository-wide/ADR-0601 (change-delivery-path) の経路が管理アカウントで成立しない。

### 評価

| 評価軸 | 案A | 案B | 採用案 |
| --- | --- | --- | --- |
| Security | 中（cross-account の書き込み経路が常設） | 低（人の強い資格情報が常設） | 中（SCP が効かない） |
| Testability | 低（アカウントが2つ要る） | 低（plan が CI で走らない） | 中 |
| Contract Clarity | 中 | 低 | 高（role の境界とアカウントの境界が一致する） |
| Maintainability | 低 | 低 | 高 |
| Scope Control | 低（フェーズ0 で成立しない） | 中 | 高 |

評価軸の定義は repository-wide/ADR-0101 (architecture-principles) を参照する。

## 意図的に捨てるもの

管理アカウントの CI の role を、SCP で外側から制限すること。

## 保証範囲

- 保証すること: 管理アカウントの CI の role が、当該リポジトリの宣言された environment の job からしか assume できない形の信頼条件を持つこと（deployment-identity/ADR-0101 (trust-by-immutable-environment-subject)。その検証の限界は同 ADR が持つ）
- 保証しないこと: apply-identity の role が、自分と他の CI の role の権限を広げる変更を apply すること。それを止めるのは Pull Request のレビューと Policy Test（未配線）である

## 検証方法

- Unit Test: `internal/github-oidc/tests/permissions.tftest.hcl` が、3つの role の policy の文書を完全一致で固定する
- 置き場（決定1）は repository-wide/ADR-0604 決定3・5 の identity root の形に従う

## 影響

管理アカウントの apply-identity の role は、SCP の外で IAM を変更できる。その権限の範囲は Pull Request のレビューで見る。

## 見直し条件

- Organizations か Identity Center の管理を委任する先のアカウントを立てたとき
- state-backend/ADR-0001 を見直すとき
