# ADR-0102: Route Table の経路をこのユースケースだけが inline で排他に持つ

- Status: Accepted
- Date: 2026-11-19
- Scope: network-foundation
- Related: repository-wide/ADR-0102, repository-wide/ADR-0201, repository-wide/ADR-0204, repository-wide/ADR-0301, repository-wide/ADR-0402, network-foundation/ADR-0101

## Context

`aws_route` と `aws_route_table_association` は、親の Route Table と同じ module に置く attachment である（`conftest/hcl/layer/decl.json`、LY301）。外部 egress の装置（NAT Gateway・Network Firewall）は `controlled-external-egress`（#27）が持つが、経路を書けるのは Route Table の持ち主だけである。Internet Gateway は VPC に1つしか付かず、外部 egress と公開の入口（`public-api-alb`）の両方が使う。Provider の `aws_route_table` は、inline の `route` を省くと既存の経路を無視し、空の list を書くと経路を排他に管理する。gateway 型の VPC Endpoint の経路は読み取りから外す（AWS Provider v6.73.0 の `internal/service/ec2/vpc_route_table.go`）。

## Decision

1. subnet・Route Table・経路・Route Table の関連付け・Internet Gateway は、このユースケースが持つ。NAT Gateway・EIP・Network Firewall は `controlled-external-egress` が持つ。
2. Route Table の経路は inline の `route` で持ち、外部 egress を持たない既定では空の list にする。経路の伝播（`propagating_vgws`）も空の list にする。`aws_route` を使わない。帯域外で足された経路は次の plan に差分として出る。
3. 外部 egress の経路は、`controlled-external-egress` の装置の識別子を入力で受けて、このユースケースが自分の Route Table に書く。有効化を bool の入力にしない。入力の名前と形は `controlled-external-egress` の設計で決める。この ADR は形を決めない。
4. gateway 型の VPC Endpoint の関連付けは、endpoint の持ち主（`private-aws-access`）が `private_route_table_ids` の出力を受けて置く。inline の `route = []` と衝突しない（Context）。
5. Route Table（subnet の Route Table と main Route Table）の経路と経路の伝播が空であること、このユースケースの module の下に宣言した型（`conftest/plan/decl.json` の `allowed_types`）の外の resource が無いことを、Policy（`plan.network_foundation` の PLAN-NET-04）で固定する（repository-wide/ADR-0402 (policy-test-scope-and-verification) 決定4）。経路を作る型を禁止の一覧で並べず、許す型を並べるので、Internet Gateway・NAT Gateway・VPN Gateway・Transit Gateway の attachment・VPC Endpoint・peering を、型の名前を知らなくても落とす。

## 検討した代替案

### 案A: `controlled-external-egress` が egress の subnet・Route Table・Internet Gateway を持つ

Internet Gateway が VPC に1つという制約で、公開の入口と両立しない。egress の subnet の CIDR がこのユースケースの割り付けの外で決まり、重なりが apply まで分からない。

### 案B: NAT Gateway と EIP もこのユースケースが持ち、`controlled-external-egress` を Network Firewall に絞る

NAT Gateway は外部 egress のためだけの装置で、外部 egress を持たない環境の持ち主に置く理由が無い。

### 案C: このユースケースで外部 egress の入力と egress の tier を先に作る

経路の形（Network Firewall を Internet Gateway と NAT Gateway の間に置くか、NAT Gateway と workload の間に置くか）が `controlled-external-egress` の設計に依る。判断を保留したまま入力を作らない（repository-wide/ADR-0204 (no-generic-escape-hatch) 決定4）。

### 案D: `aws_route` を使い、inline の `route` を書かない

帯域外で足された経路が plan に出ない。

## 意図的に捨てるもの

- 他のユースケースが Route Table に直接経路を書く構成
- DNS 経由の持ち出しの遮断（Route 53 Resolver DNS Firewall と query logging）。経路の層の保証だけを持つ。DNS は `controlled-external-egress` の設計で扱う（#27）

## 保証範囲

- 外部への経路の不在と Route Table の経路の空は、reference の plan を入力にした Policy（PLAN-NET-04）と `internal/subnet-tiers/tests/routing.tftest.hcl` が見る。
- 帯域外で足された経路が実際に plan の差分に出ることは、Provider のソースから導いたもので、実際の AWS では確かめていない。

## 検証方法

- `conftest/plan/network_foundation_test.rego` が PLAN-NET-04 の違反と非違反を固定する。
- 帯域外の経路の差分は sandbox の apply の経路（#331）で確かめる。

## 影響

- `controlled-external-egress` を有効にする変更は、このユースケースの入力と経路と Internet Gateway の追加を伴い、`allowed_types` と PLAN-NET-04 を「外部 egress を有効にしたときだけ private の Route Table が装置への経路を持つ」に改める。

## 見直し条件

- `controlled-external-egress` の設計で、経路の持ち主をこのユースケースにできない理由が見つかったとき。
- Provider が inline の `route` の読み取りから外す経路の種類が変わったとき。
