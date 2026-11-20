# ADR-0102: record を、指す先を持つユースケースが書く

- Status: Accepted
- Date: 2026-11-20
- Scope: dns-zone
- Related: repository-wide/ADR-0102, repository-wide/ADR-0204, repository-wide/ADR-0301, dns-zone/ADR-0101

## Context

record の指す先（CloudFront、ALB、S3 など）と record の持ち主が別のユースケースだと、指す先を destroy しても record が残る。指す先が他人に取り直せる資源なら、残った record が乗っ取りの経路になる。zone の持ち主が record をすべて持つと、record の入力が AWS の表現になる。Route 53 は record を書く権限を、名前・型・操作の条件で絞れるが、tag の条件は無い（Route 53 Developer Guide「Using IAM policy conditions for fine-grained access control」）。apply の role はアカウントに1つで、ユースケースごとの role は無い。

## Decision

1. このユースケースが書く record は、prd の委任の NS と DS だけとする。
2. ほかの record は、指す先を持つユースケースの module が `zone_id` と `zone_name` を入力で受けて書き、指す先と一緒に destroy する（docs/v1.0-plan/scope.md 第3.1節の `dns = { zone_id, name }`）。
3. record の書き手は、`zone_name` の下の名前だけを書く。apex、NS、DS、SOA を書かない。
4. record を書く role の `route53:ChangeResourceRecordSets` は、`ForAllValues:StringEquals` か `ForAllValues:StringLike` の `route53:ChangeResourceRecordSetsNormalizedRecordNames` で、書く名前に限る。Deny ではなく Allow で絞る。その Allow と、絞らない Allow を落とす Policy は `deployment-identity` の変更が持つ（#330）。
5. zone の record を排他に持つ `aws_route53_records_exclusive` を使わない。

## 検討した代替案

### 案A: このユースケースが全 record を持つ

record の入力が Provider の表現の素通しになる（repository-wide/ADR-0204 (no-generic-escape-hatch) 決定2）。指す先の destroy で record が残る。

### 案B: 絞り込みの Policy をこのユースケースの変更に置く

この変更の時点で、Route 53 の Allow を持つ identity の policy が無い。対象の無い規則は何も見ずに通る。

## 意図的に捨てるもの

- 他のユースケースが書いた record を、このユースケースが一覧で持つこと。

## 保証範囲

- このユースケースの record が NS と DS で、zone の apex でなく zone の名前の下にあることは PLAN-DNS-05 が見る。
- 他のユースケースが書く record の名前の絞り込みは、この変更では検査しない（#330）。

## 検証方法

- `conftest/plan/dns_zone.rego`（PLAN-DNS-05・06）

## 影響

- record を書くユースケースが増えるたびに、apply の role の名前の列挙が増える。

## 見直し条件

- ユースケースごとの apply の role を持つとき。
- Route 53 が tag の条件を受けるようになったとき。
