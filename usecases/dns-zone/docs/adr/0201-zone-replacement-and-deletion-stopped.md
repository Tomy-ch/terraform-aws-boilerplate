# ADR-0201: zone の置換と削除を止め、force_destroy と accelerated recovery を false に固定する

- Status: Accepted
- Date: 2026-11-20
- Scope: dns-zone
- Related: repository-wide/ADR-0207, repository-wide/ADR-0402, dns-zone/ADR-0101, dns-zone/ADR-0302

## Context

zone を destroy するか作り直すと、親の委任の NS が宙に浮くか、作り直した zone の name server と食い違う（dangling delegation）。Route 53 が守るのは、zone を削除した後に同じ名前の zone を作る1つの場面だけである（Route 53 Developer Guide「Protecting against dangling delegation records」）。Provider の `force_destroy` は、Terraform の管理の外の record を含めて消す。accelerated recovery を有効にした zone は、無効にするまで削除できず、有効にするのに時間がかかる（同「Accelerated recovery」）。

## Decision

1. zone の `force_destroy` を `false` に固定し、入力にしない。plan の JSON で明示の `false` と書かない値（`null`）を区別し、明示を PLAN-DNS-01 が見る（repository-wide/ADR-0207 (default-value-policy) 決定5・6）。
2. zone・KSK・DNSSEC の署名を `plan.stateful_protection` の保護の型に入れる。置換・削除・管理からの除外は、root の許可の宣言（address と理由）が無い限り plan で落ちる。DNSSEC の無効化の順序（DS を先に外す）を破った削除も止まる（dns-zone/ADR-0302）。
3. 置換を許可の宣言で通すときは、同じ変更で親の委任の写し直しを理由に書く（dns-zone/ADR-0101）。
4. accelerated recovery を `false` に固定する。可用性の要件であり、Security Hub のコントロールではない（repository-wide/ADR-0207 決定8）。

## 検討した代替案

### 案A: `force_destroy` を入力にする

sandbox の destroy のために入力で切り替えると、prd でも true にできる。複製の zone は NS と SOA だけを持つので、false のまま destroy できる。

### 案B: 置換の検出を dns-zone の専用の Policy に置く

置換・削除・管理からの除外を止める規則と許可の宣言の形は `plan.stateful_protection` が持つ。同じ規則を二重に持たない。

### 案C: accelerated recovery を入力で有効にできるようにする

要件が無く、有効化の所要の時間と Provider の timeout の相互作用を確かめられない。

## 意図的に捨てるもの

- 他のユースケースの record が残る zone を、このユースケースの destroy で消すこと（`HostedZoneNotEmpty` で止まる）。
- us-east-1 の障害の間の zone の変更。

## 保証範囲

- zone・KSK・DNSSEC の署名の置換・削除が止まることは `conftest/plan/stateful_protection_test.rego` が見る。
- `force_destroy` と accelerated recovery の値は `internal/zone/tests/` と PLAN-DNS-01 が見る。
- Policy が掛かるのは reference の plan までで、env の plan への適用は env への配線（#330）で決める。

## 検証方法

- `conftest/plan/stateful_protection.rego`・`dns_zone.rego` と、それぞれのテスト
- `internal/zone/tests/attributes.tftest.hcl`

## 影響

- zone の名前の変更（`apex_domain` か `env` の変更）は許可の宣言を要する。

## 見直し条件

- prd の zone の変更の可用性を要件にしたとき（accelerated recovery を型付きの bool として足す）。
