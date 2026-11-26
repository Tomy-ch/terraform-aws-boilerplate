# ADR-0101: 組織の trail を、管理イベントの読み書きを全リージョンで記録し、audit-evidence と CloudWatch Logs へ送る形で置く

- Status: Accepted
- Date: 2026-11-27
- Scope: security-tooling
- Related: repository-wide/ADR-0206, repository-wide/ADR-0207, audit-evidence/ADR-0001, audit-evidence/ADR-0102, audit-evidence/ADR-0201, security-tooling/ADR-0001, security-tooling/ADR-0102

## Context

組織の trail は、管理アカウントとすべてのメンバーアカウントの事象を、同じ S3 Bucket・CloudWatch Logs・Amazon EventBridge へ届ける（AWS CloudTrail User Guide「An organization trail is a configuration that enables delivery of CloudTrail events in the management account and all member accounts ... to the same Amazon S3 bucket, CloudWatch Logs, and Amazon EventBridge」）。multi-region の trail は、すべてのリージョンの事象を1つの配信先に集める。

`audit-evidence` は、log-archive のアカウントに、組織の trail の配信を受ける Object Lock の Bucket と鍵を持つ。Bucket Policy と Key Policy は、trail の名前・所有する管理アカウントの ID・組織の ID・リージョンから、trail が在る前に組める（audit-evidence/ADR-0102・0201）。

Security Hub の CloudTrail.1（管理イベントの読み書きを記録する multi-region の trail）、CloudTrail.2（保管時の暗号化）、CloudTrail.4（log file validation）、CloudTrail.5（CloudWatch Logs との統合）が、この trail に当たる。

## Decision

1. 組織の trail を1つ、multi-region で置く。home のリージョンは provider のリージョンとする。
2. 記録するのは管理イベントの読みと書きだけとする。data events は、要るものを決めた変更が selector を足す。
3. log file validation を有効にする。
4. 配信先は、log-archive のアカウントの `audit-evidence` の Bucket とする。`kms_key_id` に `audit-evidence` の鍵を必ず設定する。trail の名前は、`audit-evidence` の `writers.cloudtrail_organization_trail.trail_name` に渡す値と同じ固定の名前とする。
5. CloudWatch Logs へ送る。Log Group は security のアカウントに置き、保持を365日以上とし、このユースケースの customer managed key で暗号化する。CloudTrail が引き受ける role の権限は、その Log Group への `logs:CreateLogStream` と `logs:PutLogEvents` だけとする。

## 検討した代替案

### 案A: 管理アカウントだけの trail を先に置く

security と log-archive の配線を待たずに置ける。管理アカウントの trail は後で組織の trail へ変えられる（AWS CloudTrail User Guide）。ただし Log Group と role は呼ぶアカウントに在るものに限られるので、security のアカウントへ移すときに作り直しが要る。保管の Bucket を管理アカウントに置くなら、Object Lock を持たず、記録される側の主体が消せる。

### 案B: data events も最初から記録する

記録の量と料金は、trail を置くまで分からない。足すことは後から selector を足すだけで、作り直しを伴わない。

### 案C: CloudWatch Logs へ送らない

CloudTrail.5 から外れ、security-tooling/ADR-0102 の通知の経路が無くなる。取り込みの料金は掛からない。

### 評価

| 評価軸 | 案A | 案B | 案C | 採用案 |
| --- | --- | --- | --- | --- |
| Security | 低（記録される側が消せる） | 高 | 中（通知の経路が無い） | 高 |
| Testability | 中 | 中 | 中 | 中（実配信は配線の後） |
| Contract Clarity | 低（後で置き場が変わる） | 高 | 高 | 高 |
| Maintainability | 低（作り直しが要る） | 中（料金が読めない） | 高 | 高 |
| Scope Control | 中 | 低 | 高 | 高 |

評価軸の定義は repository-wide/ADR-0101 (architecture-principles) を参照する。

## 意図的に捨てるもの

data events の記録。S3 の object の読み書きと Lambda の起動は、selector を足すまで記録しない。

## 保証範囲

- 保証すること: 無い。このユースケースは Terraform の実装を持たない
- 保証しないこと: CloudWatch Logs の取り込みの料金。量は trail を置いてから、Log Group の `IncomingBytes` で確かめる

## 検証方法

- trail を置く変更が、決定1-5 を固定する Unit Test と Policy Test を足す
- 配信は、security-tooling/ADR-0001 決定6 の昇格の条件として、実環境で確かめる

## 影響

`audit-evidence` は、この trail の名前・管理アカウントの ID・組織の ID・リージョンを `writers` で受ける。trail を置く前に、log-archive のアカウントで `audit-evidence` を apply しておく。

## 見直し条件

- data events を記録する対象が決まったとき（決定2）
- Log Group の取り込みの料金が、検知の効果に見合わないと分かったとき（決定5）
