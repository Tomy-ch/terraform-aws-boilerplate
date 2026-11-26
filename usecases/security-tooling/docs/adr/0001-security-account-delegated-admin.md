# ADR-0001: 組織の検知を security のアカウントの委任管理者で持ち、experimental で始める

- Status: Accepted
- Date: 2026-11-27
- Scope: security-tooling
- Related: repository-wide/ADR-0102, repository-wide/ADR-0206, repository-wide/ADR-0207, repository-wide/ADR-0301, repository-wide/ADR-0604, security-tooling/ADR-0101, security-tooling/ADR-0102

## Context

AWS SRA は、組織の CloudTrail を CloudTrail の委任管理者のアカウントから管理することを求める（「SHOULD be managed from a CloudTrail delegated administrator account」）。`docs/v1.0-plan/scope.md` 第7節は、このユースケースを「委任管理者（securityのアカウント）で持つ組織の検知」とし、第10.1節は、検知を調べる日常の作業を security のアカウントに置き、管理アカウントに入れない。

組織の trail の所有者は、委任管理者が作っても管理アカウントである（AWS CloudTrail User Guide「Organization trails and event data stores only exist in the management account」）。trail を作るときの CloudWatch Logs の Log Group と、CloudTrail が引き受ける role は、作る呼び出しをしたアカウントに在るものに限られる（同 Guide「Both the CloudWatch Logs log group and log role must exist in the calling account」）。

管理アカウントには SCP が効かない。security のアカウントは Security の OU に在り、SCP で守れる。

## Decision

1. `security-tooling` は、security のアカウントの workload root（`env/security/`）だけが呼ぶ。管理アカウントとほかのメンバーアカウントの root は呼ばない。
2. 組織の trail は、CloudTrail の委任管理者の security のアカウントから作る。Log Group と、CloudTrail が引き受ける role も security のアカウントに置く（security-tooling/ADR-0101）。
3. 管理アカウントの事象の通知（break-glass・root・`sts:AssumeRoot`）も、security のアカウントで持つ（security-tooling/ADR-0102）。
4. 委任管理者の指定は `organization-baseline` が持ち、CloudTrail の信頼アクセスは人の手順で有効にする（organization-baseline/ADR-0001 決定5）。このユースケースは、そのどちらも持たない。
5. このユースケースは experimental で始める（repository-wide/ADR-0102 (use-case-centric-scope) 決定19-22）。免除されるのは公開契約の後方互換性だけである。
6. stable へ昇格する条件は、次のすべてを満たすことである。
   - security のアカウントへ CI が apply し、その後の CI の plan が変更なしを示す
   - 組織の trail が logging の状態で、`audit-evidence` の Bucket と Log Group の両方に事象が届く
   - security-tooling/ADR-0102 の alarm が、`set-alarm-state` で Slack まで届く
   - 毎月の break-glass の試験（`docs/setup/organization-baseline.md` 3.6）で、サインインと role の引き受けの alarm が鳴る
7. security のアカウントへの配線が、v1.0.0 の版を切るまでに入らないときは、このユースケースを削除する。

## 検討した代替案

### 案A: 管理アカウントの workload root（`env/admin/`）が組織の trail を作る

SRA の委任管理者の形から外れる。Log Group と alarm が管理アカウントに入り、SCP で守れない。組織のすべてのアカウントの管理イベントが管理アカウントの Log Group に入り、scope.md 第10.1節に反する。security のアカウントの配線を待たずに置ける。

### 案B: trail と通知を、管理アカウントの別のユースケースにする

このユースケースの残り（GuardDuty ほか）は security のアカウントに置くので、同じユースケースを2つのアカウントから別の形で呼ぶことを避けるには、分けるしかない。repository-wide/ADR-0102 決定3（1つのユースケースは1つのアーキテクチャ）は満たすが、案A と同じく SRA から外れ、scope.md 第7節・#473 の配置を書き換える。

### 評価

| 評価軸 | 案A | 案B | 採用案 |
| --- | --- | --- | --- |
| Security | 低（Log Group と alarm を SCP で守れない） | 低（同左） | 高 |
| Testability | 中 | 中 | 中（実配信は security と log-archive の配線の後） |
| Contract Clarity | 中 | 低（組織の検知が2つのユースケースに分かれる） | 高 |
| Maintainability | 中 | 低 | 高 |
| Scope Control | 低（管理アカウントに日常の調べ物が入る） | 中 | 高 |

評価軸の定義は repository-wide/ADR-0101 (architecture-principles) を参照する。

## 意図的に捨てるもの

security のアカウントの配線を待たずに、組織の trail と通知を置くこと。配線までの間、90日を超える監査の記録も、repository-wide/ADR-0301 決定19 の通知も無い。

## 保証範囲

- 保証すること: 無い。このユースケースは Terraform の実装を持たない
- 保証しないこと: 組織の trail を委任管理者から Terraform で作れること（AWS Provider の `aws_cloudtrail` が、管理アカウントの所有する trail を委任管理者のアカウントから読み書きできるか）。trail を置く変更の着手時に確かめ、できなければこの ADR を見直す

## 検証方法

- 昇格の条件（決定6）は、実環境の観測で確かめる。どのテストも見ない
- 決定1 の呼び出し元を見る検査は、実装を足す変更が置く

## 影響

trail を置くには、次が先に要る。

- security と log-archive のアカウントの `state-backend`・`deployment-identity`・workload root と、その CI の経路
- log-archive の `audit-evidence` と、security と log-archive の `ops-notification`
- 委任管理者の指定と、CloudTrail の信頼アクセス

## 見直し条件

- 決定6 の条件を満たしたとき（stable へ昇格する）
- 決定7 の期限に達したとき
- 委任管理者のアカウントから組織の trail を Terraform で扱えないと分かったとき（保証範囲）
