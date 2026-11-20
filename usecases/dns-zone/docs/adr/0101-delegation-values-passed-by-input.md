# ADR-0101: 委任の NS と DS の値を入力で受け、prd の apply だけが書く

- Status: Accepted
- Date: 2026-11-20
- Scope: dns-zone
- Related: repository-wide/ADR-0102, repository-wide/ADR-0204, repository-wide/ADR-0207, repository-wide/ADR-0301, dns-zone/ADR-0001, dns-zone/ADR-0201, dns-zone/ADR-0302

## Context

dev と stg の zone は別のアカウントにあり、その name server と DS は zone を作った後に決まる。prd の zone の委任の NS と DS は、その値を写したものでなければならない。親の NS が子の zone と食い違うか、子の zone の無い名前を指すと、dangling delegation になる（Route 53 Developer Guide「Protecting against dangling delegation records」）。別のアカウントの state を読むか、1つの root で2つのアカウントの provider を持てば値を自動で渡せるが、1つの apply が2つのアカウントへ書く（repository-wide/ADR-0301 (least-privilege-policy) 決定18）。

## Decision

1. 入力 `delegations` は、key を `dev` か `stg`、値を `name_servers`（Route 53 の name server を4つ）と optional の `ds_record`（`<key tag> 13 2 <digest>`）とする。prd のほかの zone が空でない値を受けると plan で落ちる。
2. 値は、子の zone のアカウントの apply の出力 `name_servers`・`ds_record` を、人が prd の env の値へ写す。手順は docs/setup/dns-zone.md が持つ。
3. 順序は、子の zone の apply、出力の写し、prd の apply とする。子の zone を作る前に親へ NS を書かない。DS は子の zone の署名が有効になった後に足す。
4. 委任の NS の TTL は 3600 秒、DS の TTL は 300 秒とする。DS は不整合のときに名前の解決を止めるので、短く始める（Route 53 Developer Guide「Enabling DNSSEC signing」）。NS は Route 53 の勧めの範囲の下限とする（同「Best practices for Amazon Route 53 DNS」）。値の選択は独自判断（repository-wide/ADR-0207 (default-value-policy) 決定3 の優先順位6）で、理由の記録は本 ADR とする。
5. `allow_overwrite = false` を明示し、既にある record を黙って取り込まない。

## 検討した代替案

### 案A: 別のアカウントの state を読む、または provider の alias で子のアカウントを読む

1つの root が2つのアカウントへ届く権限を持つ（repository-wide/ADR-0301 決定18）。

### 案B: reusable delegation set で name server を固定する

zone を作り直しても name server が変わらず、写し直しが要らない。AWS の勧めの本体は親の NS と zone の name server を一致させることで、置換そのものを止める方が宙づりの窓を作らない（dns-zone/ADR-0201）。delegation set が zone を持たない間の宙づりも生じる。

### 案C: name server と DS を AWS の表現のまま任意の record として受ける

record の入力が Provider の表現の素通しになる（repository-wide/ADR-0204 (no-generic-escape-hatch) 決定2）。

## 意図的に捨てるもの

- 子の zone の name server の変化に、親の委任が自動で追随すること。
- 孫の委任と、apex のほかの zone からの委任。

## 保証範囲

- 入力の形（key、name server の形と数、DS の algorithm と digest type）は `tests/validation.tftest.hcl` が見る。
- 委任の NS と DS の名前・型・TTL・値は `internal/zone/tests/` が、prd のほかの環境が委任を持てないことは `tests/composition.tftest.hcl` が見る。このユースケースが NS と DS のほかの record を持たないことと、reference の委任の record の件数は PLAN-DNS-00・05 が見る。
- 写した値が子の zone の実際の値と一致することは検査しない（別のアカウントの値で plan に出ない）。

## 検証方法

- `tests/validation.tftest.hcl`、`internal/zone/tests/attributes.tftest.hcl`・`validation.tftest.hcl`、`conftest/plan/dns_zone.rego`

## 影響

- 子の zone の置換を許可の宣言で通したときは、同じ変更で prd の `delegations` を写し直す。

## 見直し条件

- 子の zone の値をアカウントをまたいで安全に渡す仕組み（組織の parameter の共有など）を repository-wide で採ったとき。
- 置換を許可して通す運用が頻繁になったとき（案B）。
