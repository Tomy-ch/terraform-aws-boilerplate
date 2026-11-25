# ADR-0209: リージョンごとの安全側の設定は partition の既定で有効な全リージョンに置き、CI の経路をその集合に合わせる

- Status: Accepted
- Date: 2026-11-25
- Scope: repository-wide
- Related: ADR-0001, ADR-0102, ADR-0204, ADR-0205, ADR-0207, ADR-0208, ADR-0301, ADR-0401, ADR-0702

## Context

AWS Security Hub の AWS Foundational Security Best Practices と CIS AWS Foundations Benchmark には、アカウントとリージョンの組を単位に評価するコントロールがある（EC2.7・IAM.28・Config.1・EC2.172・SSM.6・SSM.7・EMR.2、GuardDuty.1 など）。AWS SRA は、EBS の既定の暗号化を「every account and Region」で有効にすることを求める。使わないリージョンでも、主体は資源を作れる。

AWS Account Management Reference Guide「Enable or disable AWS Regions in your account」によれば、2019年3月20日より前に提供されたリージョンは既定で有効で、無効にできない。aws の partition では17ある。それより後のリージョンは opt-in である。そのため、既定のリージョンの集合は増えも減りもしない。

リージョンごとの設定を置く usecase は複数になる（アカウントごとの統制の設定と、組織の委任管理者の指定）。CI の role の権限と、ランナーの egress の宛先（`.github/egress.toml`）も同じ集合を要する。`make egress-check` は、ブロックの `region` が region の形の literal でなければ落ちた。CI の role の権限は、ADR-0208 決定4 により provider の region と us-east-1 に限られていた。

IAM の `aws:RequestedRegion` は、要求が呼んだ endpoint の region を表し、常に要求の文脈に含まれる（IAM User Guide「AWS global condition context keys」）。リージョンごとの資源の ARN の region は、要求を受けた endpoint の region と一致する。

## Decision

1. リージョンを単位に評価・適用される安全側の設定（リージョンごとの統制の設定と、リージョンごとの委任管理者の指定）は、決定2 の集合のすべてのリージョンに置く。
2. 集合は、partition ごとの、AWS が既定で有効とするリージョン（opt-in でないもの）とする。aws は17とする。集合は一般 module `modules/default-regions` の出力 `by_partition`（partition を key にした literal の map）として1つだけ持ち、Terraform のほかの場所に literal で書かない。持たない partition の key は置かない。集合は利用者の入力にしない（ADR-0204 決定2、ADR-0208 決定3）。ADR-0205 決定1・2 が採らないのは外部の module であり、リポジトリの中の相対パスの一般 module（同 決定3）はこれに当たらない。
3. 置く usecase は、provider を1つのまま、`by_partition[data.aws_partition.<name>.partition]` を `for_each` に取り、resource の `region = each.key` で置く。key の無い partition では、置く usecase が plan を止める。`data.aws_regions` の結果を `for_each` に使わない。
4. 決定2 の集合に置く resource の CI の role の権限は、集合のリージョンに限る。ARN にリージョンを持つ Resource は、リージョンの部分だけを `*` にした ARN で Allow し、同じ role の inline policy に、それらの Action を、条件 `StringNotEquals` の `aws:RequestedRegion` が集合の外のとき拒む Deny（Resource `"*"`）を置く（ADR-0301 決定11 の但し書き）。resource-level 権限を持たないリージョンごとの Action も、同じ Deny で集合に限る。サービス固有のリージョンの条件キー（`ec2:Region`）を受ける Action は、Allow にもその条件を集合で置く。アカウントに1つの設定と global のサービスの Action は Deny に含めない。集合に key の無い partition では、これらの文を置かない。
5. `make egress-check` は、`region = each.key` を、ブロックの `for_each` を root から静的に解いた集合の各リージョンとして読む。解ける形は閉じた一覧とし（`scripts/README.md` の `egress/` の行）、解けない形、空の集合、リージョンの形でない要素で落ちる。スカラーの `region` は ADR-0208 決定1 のとおり literal に限る。`.github/egress.toml` は集合の写しを `region_set` に持ち、リージョンごとの宛先を `{<set>}` を含む形で書く。`make egress-check` は、usecase の `reference/` の root でも、リージョンを解けることを確かめる（ADR-0702 決定20）。
6. 集合の写しは、Terraform を読まない道具のための `.github/egress.toml` の `region_set` と、`conftest/plan/decl.json` の `default_regions` に限る。ずれは `make egress-check` と Policy Test が落とす。`region_set` は、集合に置く root を実行する job を宣言する変更が足す。テストの期待値は写しに数えない（ずれは、そのテストが落とす）。
7. 集合のどれかのリージョンでサービスが提供されないときは、集合の一部だけに置く形を黙って採らない。ADR-0207 決定11 の逸脱として、置く usecase の ADR に記録する。

## 検討した代替案

### 案A: 集合を置く usecase ごとの literal として持つ

利用者ごとに写しができ、ずれを検査する Policy が利用者ごとに要る。

### 案B: `data.aws_regions` を `for_each` に使う

集合が plan で決まらず、権限と egress を固定できない。opt-in のリージョンが黙って加わる。

### 案C: 権限の ARN をリージョンごとに列挙する

許す要求は決定4 と同じだが、aws の17では、リージョンごとの統制の読み取りと管理の policy が、customer managed policy の上限の 6,144 文字を超える。policy を足すたびに CI の role を管理する role の inline policy が伸び、上限の 90% を超える。

### 案D: リージョンを `*` にした ARN で Allow し、条件を付けない

opt-in のリージョンを含む。SCP の効かない管理アカウントでは、opt-in が有効にされれば届く。

### 案E: egress の宛先をリージョンの wildcard で書く

宣言の検査が途中の `*` を拒み、opt-in のリージョンの endpoint にも当たる。宛先のリージョンを読めず、置かれるリージョンとの突き合わせから外れる。

### 評価

| 評価軸 | 案A | 案B | 案C | 案D | 採用案 |
| --- | --- | --- | --- | --- | --- |
| Security | 高 | 中 | 高 | 低 | 高 |
| Testability | 中（写しごとの検査） | 低 | 高 | 中 | 高 |
| Contract Clarity | 高 | 中 | 高 | 中 | 高 |
| Maintainability | 低（写しが増える） | 低 | 低（policy が増える） | 高 | 中（Deny と Allow の対応） |
| Scope Control | 中 | 低 | 中 | 低 | 高 |

評価軸の定義は ADR-0101 を参照する。

## 意図的に捨てるもの

- opt-in のリージョンを使うこと
- 集合を持たない partition で、リージョンごとの安全側の設定を置くこと
- CI の role の Allow を、リージョンを含む完全な ARN の列挙だけで読めること（範囲は Deny と合わせて読む）

## 保証範囲

- 保証すること: module を呼ぶ Terraform の消費者が同じ集合を使うこと（同じ module の出力）。写しが Terraform の集合とずれないこと（`decl.json` は Policy Test、`region_set` は集合に置く root を実行する job があるときの `make egress-check`）。CI の role の、ARN のリージョンを `*` にした Allow の Action が、同じ role の inline policy の集合の外を拒む Deny に含まれ、その Deny の集合が宣言と一致すること（Policy Test）。resource-level 権限を持たないリージョンごとの Action が Deny に含まれること（Unit Test）。`region = each.key` の集合を解けない構成が、CI の root と reference の root で落ちること
- 保証しないこと: 消費者が module を呼ばずに同じ集合を literal で書くことを拒むこと。レビューだけが見る
- 保証しないこと: 実際の AWS で Deny が集合の外の要求を拒むこと（sandbox の Integration Test の経路が無い間は確かめない）。集合のすべてのリージョンでサービスが提供されること（決定7 は、置く usecase が着手のときに確かめる）

## 検証方法

- Unit Test: `modules/default-regions/tests/`（件数と代表の要素）、権限を持つ usecase の文書の完全一致と長さ、Deny の Action とリージョンごとの Allow の Action の一致
- Policy Test（`make tf-policy`）: 置く usecase の reference の plan のリージョンが `decl.json` の `default_regions` に過不足なく在ること、CI の role の Deny と Allow の対応と Deny の集合、対象の policy が入力に在ること（`PLAN-REGION-*`）
- `make egress-check`、`scripts/egress` の Go のテスト

## 影響

- 集合に置く usecase は `modules/default-regions` を呼ぶ。新しく集合に置く usecase は、CI の role の Allow を決定4 の形で足し、Deny の Action に足し、置く root の egress の宛先を `region_set` のテンプレートで宣言する
- 集合に置く resource の instance は、集合の数だけ増える

## 見直し条件

- AWS が既定で有効なリージョンの定義を変えたとき
- opt-in のリージョンを使う利用者が現れたとき
- aws の外の partition で使う利用者が現れたとき
- 集合のリージョンでサービスが提供されない usecase が現れたとき（決定7）
