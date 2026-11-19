# ADR-0101: subnet を private と isolated の2つの tier に分け、CIDR を固定の slot で割り付ける

- Status: Accepted
- Date: 2026-11-19
- Scope: network-foundation
- Related: repository-wide/ADR-0101, repository-wide/ADR-0102, repository-wide/ADR-0202, repository-wide/ADR-0204, repository-wide/ADR-0207, repository-wide/ADR-0301, network-foundation/ADR-0102

## Context

subnet の CIDR を変えると subnet は置換になり、その中の ENI を持つ資源も作り直しになる（repository-wide/ADR-0202 (internal-topology-as-implementation-detail) 決定8）。tier や AZ を後から足したときに既存の CIDR が動かない割り付けを、最初に決める必要がある。外部 egress を有効にした環境でも、データの層は VPC の外への経路を持たないことが求められる（repository-wide/ADR-0301 (least-privilege-policy) 決定16）。

## Decision

1. tier は `private`（workload）と `isolated`（データの層）の2つとする。どちらも VPC の外への経路を持たない。外部 egress を有効にした環境でも、`isolated` は経路を持たない。
2. VPC の CIDR を 16 に分け（`cidrsubnet(cidr, 4, n)`）、4つずつを slot とする。slot 0 を `private`、slot 1 を `isolated` に充て、slot 2（公開の入口）と slot 3（外部 egress）を後続の tier のために空ける。各 slot の中の位置は `availability_zone_ids` の位置で決まる。
3. AZ は AZ の名前ではなく AZ の ID で受ける。AZ の名前と物理的なゾーンの対応はアカウントごとに異なり、ID はアカウントをまたいで同じゾーンを指す。2 個から 4 個とする。
4. `availability_zone_ids` の途中の要素を消すか並べ替えると、それより後ろの subnet が置換になる。AZ を足すときは末尾に足す。
5. VPC の CIDR は `/16` から `/24` とする。`/24` のとき subnet は `/28`（AWS の最小）になる。
6. Route Table は tier と AZ の組ごとに1つ置く。外部 egress の経路は AZ ごとの装置を指すので、後から分けると関連付けの付け替えになる。
7. tier の集合と割り付けは独自判断（repository-wide/ADR-0207 (default-value-policy) 決定3 の優先順位6）であり、理由の記録は本 ADR とする（同 決定4）。

## 検討した代替案

### 案A: AZ の数を受けて、使える AZ を先頭から選ぶ（`aws-ia/vpc` の `az_count`）

AWS が AZ を足すと選ばれる AZ がずれ、subnet が置換になる。

### 案B: tier と subnet の CIDR を入力で受ける（`aws-ia/vpc` の `subnets`）

subnet の構成が Provider の表現の素通しになる（repository-wide/ADR-0101 (architecture-principles) 決定9、repository-wide/ADR-0204 (no-generic-escape-hatch) 決定2）。

### 案C: tier を1つにする

外部 egress を有効にしたとき、データの層を経路のある subnet から付け替える（置換）ことになる。

### 案D: isolated の tier に custom の Network ACL を置く

経路の無い tier では遮断を足さない。Network ACL は stateless で、gateway 型の VPC Endpoint への通信を消費者が足すたびに規則の変更が要る。

## 意図的に捨てるもの

- VPC の半分（slot 2・3）は、公開の入口と外部 egress を持たない環境では使われない。
- AZ が1つの構成と5つ以上の構成、tier と CIDR の自由な指定、custom の Network ACL（README の unsupported）。

## 保証範囲

- CIDR の割り付け、AZ を末尾に足しても既存の CIDR が動かないこと、Route Table が tier と AZ の組ごとに1つで経路が空であることは、`internal/subnet-tiers/tests/` が mock の plan で見る。
- AZ の途中の削除と並べ替えを止める検査は無い。

## 検証方法

- `internal/subnet-tiers/tests/layout.tftest.hcl`・`routing.tftest.hcl`
- `tests/validation.tftest.hcl` が `cidr` と `availability_zone_ids` の許容値を固定する。

## 影響

- 後続の tier（公開の入口、外部 egress）は slot 2・3 から切り出すので、既存の subnet を動かさずに足せる。

## 見直し条件

- 外部 egress の tier が AZ あたり2つ以上の subnet を要し、slot 3 に収まらないとき（#27）。
- AZ の途中の削除を止める検査（Policy による置換の検出）を足すとき（#330）。
