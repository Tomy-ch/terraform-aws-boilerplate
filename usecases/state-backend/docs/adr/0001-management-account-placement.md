# ADR-0001: 管理アカウント自身の state Bucket を管理アカウントに置く

- Status: Accepted
- Date: 2026-10-25
- Scope: state-backend
- Related: repository-wide/ADR-0207, repository-wide/ADR-0601, repository-wide/ADR-0602, repository-wide/ADR-0604, repository-wide/ADR-0301

## Context

state Bucket は env ごとに、その env のアカウントに置く（repository-wide/ADR-0604 (state-split-by-apply-role) 決定7）。管理アカウントも state を持つ —— `state-backend`・`deployment-identity`・`organization-baseline` と、メンバーアカウントを立ち上げる間の bootstrap の構成物（同 決定6）である。

AWS Organizations のベストプラクティスは、resource を管理アカウント以外へ置くこと、管理アカウントには管理アカウントでなければならないものだけを置くことを勧める。SCP と RCP は管理アカウントの主体と resource に効かない。

## Decision

1. 管理アカウントの state Bucket を管理アカウントに置く。その Bucket には、管理アカウントの identity root の state だけを置く。
2. これを公式推奨からの逸脱として記録する（repository-wide/ADR-0207 (default-value-policy) 決定11）。

## 検討した代替案

### 案A: 委任管理者アカウントへ集約する

管理アカウントの state を別アカウントに置く。管理アカウントへの cross-account の書き込みが常設になり、repository-wide/ADR-0301 (least-privilege-policy) 決定17 の明示的なユースケースを1つ増やす。フェーズ0 の時点では、管理アカウントの他にアカウントが存在しない。

### 評価

| 評価軸 | 案A | 採用案 |
| --- | --- | --- |
| Security | 中（cross-account の書き込み経路が常設） | 中（SCP / RCP が効かない） |
| Testability | 低（アカウントが2つ要る） | 中 |
| Contract Clarity | 中 | 高（state の境界とアカウントの境界が一致する） |
| Maintainability | 低 | 高 |
| Scope Control | 低（フェーズ0 で成立しない） | 高 |

評価軸の定義は repository-wide/ADR-0101 (architecture-principles) を参照する。

## 意図的に捨てるもの

管理アカウントの state Bucket を、SCP と RCP で外側から守ること。

## 保証範囲

- 保証すること: 管理アカウントの state が、管理アカウントの外の主体からの書き込み経路を持たないこと
- 保証しないこと: 管理アカウントの root による bucket policy の削除の防止（`README.md` の unsupported）

## 検証方法

- Unit Test: `env/admin/identity/tests/imports.tftest.hcl` が、管理アカウントの identity root の state Bucket の名前が `tabp-admin-tfstate-<account_id>-<region>-an` になることを mock の plan で確かめる
- 置き場（決定1）は repository-wide/ADR-0604 (state-split-by-apply-role) 決定5・7 の root と key の形に従い、その検証方法が見る

## 影響

管理アカウントの state Bucket の防御は、bucket policy・key policy・CloudTrail・root の封印に限られる。前の3つは bootstrap の段5 で入る（`README.md` の「段ごとに持つもの」）。

## 見直し条件

- Organizations か Identity Center の管理を委任する先のアカウントを立てたとき
- CI/CD の実行を AWS の上へ移すとき
