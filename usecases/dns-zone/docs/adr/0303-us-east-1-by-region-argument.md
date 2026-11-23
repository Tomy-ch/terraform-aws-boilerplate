# ADR-0303: us-east-1 の資源を、provider の alias ではなく resource の region 引数で置く

- Status: Accepted
- Date: 2026-11-20
- Scope: dns-zone
- Related: repository-wide/ADR-0102, repository-wide/ADR-0205, repository-wide/ADR-0208, repository-wide/ADR-0402, repository-wide/ADR-0501, dns-zone/ADR-0301, dns-zone/ADR-0302

## Context

クエリログの Log Group と鍵、resource policy、DNSSEC の鍵、DNSSEC の alarm は us-east-1 に置く。us-east-1 にしか置けない資源の置き方は repository-wide/ADR-0208 (us-east-1-resources-by-region-argument) が決める。Route 53 の resource は region を持たない（グローバル）。

## Decision

1. us-east-1 の資源は、repository-wide/ADR-0208 決定1 に従い、各 resource の `region = "us-east-1"`（literal）で置く。このユースケースは `provider` block と `configuration_aliases` を持たない。
2. クエリログの鍵（Key Policy・alias を含む）と Log Group は、repository-wide/ADR-0208 決定2 に従い `internal/query-log` が自分で持ち、`modules/kms` と `modules/log-group` を呼ばない。
3. `internal/query-log` は `modules/kms`・`modules/log-group` と同じ値（対称鍵・自動 rotation 365 日・削除の猶予 30 日、管理の委任に暗号操作を含めない Key Policy、class `STANDARD`、`skip_destroy = false`）を持つ。重複は repository-wide/ADR-0205 (module-dependency-policy) 決定11 が許す重複で、保持日数の下限・鍵による暗号化・region は PLAN-DNS-02 が、Key Policy は `plan.kms_key_policy` が、一般 module と同じ検査で見る。

## 検討した代替案

provider の alias と、一般 module の `region` の入力は、repository-wide/ADR-0208 の案A・案B が退けている。

## 意図的に捨てるもの

- クエリログの鍵と Log Group が、`modules/kms`・`modules/log-group` の変更に自動で追随すること（追随の漏れは上の Policy が落とす範囲に限って検出する）。

## 保証範囲

- 各資源の region と、クエリログの鍵・Key Policy・Log Group の値は `internal/*/tests/` が見る。Log Group・その鍵・resource policy・DNSSEC の鍵・alarm が us-east-1 にあることは PLAN-DNS-02・03・04 が見る。
- `region` 引数を実際の AWS が受けることは確かめていない（#331）。

## 検証方法

- `internal/*/tests/`、`conftest/plan/dns_zone.rego`、`conftest/plan/kms_key_policy.rego`、`make egress-check`

## 影響

- CI の apply の role の権限と、ランナーの egress の宛先を、us-east-1 についても列挙する必要がある（#330）。

## 見直し条件

- repository-wide/ADR-0208 の見直し条件
