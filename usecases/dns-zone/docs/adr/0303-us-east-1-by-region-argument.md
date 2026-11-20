# ADR-0303: us-east-1 の資源を、provider の alias ではなく resource の region 引数で置く

- Status: Accepted
- Date: 2026-11-20
- Scope: dns-zone
- Related: repository-wide/ADR-0102, repository-wide/ADR-0204, repository-wide/ADR-0402, repository-wide/ADR-0501, dns-zone/ADR-0301, dns-zone/ADR-0302

## Context

クエリログの Log Group と鍵、resource policy、DNSSEC の鍵、DNSSEC の alarm は us-east-1 に置く。provider は env の root が1つの region で渡す。module が `configuration_aliases` で us-east-1 の provider を要求すると、その module のディレクトリでの `terraform validate` が、渡されない provider を理由に落ちる。`make tf-validate` はすべてのユースケースのディレクトリで validate を走らせ、このエラーは抑止できない。AWS Provider v6 は多くの resource に `region` 引数を持ち、plan の JSON に region が値として現れる。Route 53 の resource は region を持たない（グローバル）。

## Decision

1. us-east-1 の資源は、各 resource の `region = "us-east-1"`（固定値）で置く。このユースケースは `provider` block と `configuration_aliases` を持たない。
2. `modules/kms` と `modules/log-group` は optional の入力 `region` を持つ。既定の `null` は provider の region で、ほかに受けるのは `us-east-1` だけとする。この入力は AWS の region 名を任意に受ける口ではなく、閉じた2値で resource の追加も引数の上書きもしないので、Generic Escape Hatch に当たらない（repository-wide/ADR-0204 (no-generic-escape-hatch) 決定2）。値を足すときは、要る消費者の着手時に同じ判断をする。

## 検討した代替案

### 案A: `configuration_aliases` で us-east-1 の provider を受ける

必須のゲートの `terraform validate` が落ちる。plan の JSON から resource の region を読めず、Policy が us-east-1 であることを見られない。

### 案B: `internal/query-log` が Log Group と鍵を自分で持つ

`modules/log-group` の保持日数・鍵・削除の保護の不変条件を写し、更新に追随しなくなる。

## 意図的に捨てるもの

- 入力 `region` に、us-east-1 のほかのリージョンを渡すこと。

## 保証範囲

- 各資源の region は `internal/*/tests/` と、`modules/kms`・`modules/log-group` の `tests/` が見る。Log Group・resource policy・DNSSEC の鍵・alarm が us-east-1 にあることは PLAN-DNS-02・03・04 が見る。
- `region` 引数を実際の AWS が受けることは確かめていない（#331）。

## 検証方法

- `modules/kms/tests/`、`modules/log-group/tests/`、`internal/*/tests/`、`conftest/plan/dns_zone.rego`

## 影響

- CI の apply の role の権限を、region ごとに列挙する必要がある（#330）。
- CloudFront の証明書や WAF など、us-east-1 を要る他のユースケースも同じ形を取れる。2つ目のユースケースが同じ形を取るときに、repository-wide の ADR への昇格を判断する（repository-wide/ADR-0001 (adr-process-and-placement) 決定20-22）。

## 見直し条件

- `terraform validate` が `configuration_aliases` を持つ module を単独で検証できるようになったとき。
